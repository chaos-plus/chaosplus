package authn

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	auditmod "github.com/chaos-plus/chaosplus/internal/modules/audit"
	"github.com/uptrace/bun"
)

const (
	emailVerificationNotification = "email_verification"
	emailVerificationTokenPrefix  = "cpe1_"
)

type emailVerificationRow struct {
	bun.BaseModel `bun:"table:iam_email_verification_tokens"`
	TokenHMAC     string `bun:"token_hmac,pk"`
	PrincipalID   guid.ID
	Email         string
	CreatedAt     int64
	ExpiresAt     int64
	ConsumedAt    int64
	// Code 是 6 位数字邮箱验证码(注册/登录输入);一次性 + 短时效。
	Code string `bun:"code,notnull,default:''"`
}

// emailVerificationCode 生成 6 位数字邮箱验证码(一次性、短时效)。
func emailVerificationCode() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", binary.BigEndian.Uint32(b[:])%1000000), nil
}

func (s *WebService) configureEmailVerification() error {
	if !s.cfg.EmailVerification.Enabled {
		return nil
	}
	if !s.web.Enabled || s.cfg.EmailVerification.TokenTTL < 5*time.Minute ||
		s.cfg.EmailVerification.TokenTTL > 7*24*time.Hour {
		return errors.New("authn email verification configuration exceeds security limits")
	}
	return validateAuthnURL("email_verification.verify_url", s.cfg.EmailVerification.VerifyURL)
}

func (s *WebService) EmailVerificationEnabled() bool {
	return s != nil && s.cfg.Enabled && s.web.Enabled && s.cfg.EmailVerification.Enabled && s.db != nil
}

func (s *WebService) BeginEmailVerification(ctx context.Context, authorization, cookieHeader string) error {
	if !s.EmailVerificationEnabled() {
		return authnext.ErrEmailVerificationDisabled
	}
	claims, err := s.Authenticate(ctx, authorization, cookieHeader)
	if err != nil {
		return err
	}

	now := s.now().UTC()
	expires := now.Add(s.cfg.EmailVerification.TokenTTL)
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var principal principalRow
		query := tx.NewSelect().Model(&principal).Where("id = ? AND status = 'active'", claims.PrincipalID)
		if s.db.Dialect().Name().String() != "sqlite" {
			query = query.For("UPDATE")
		}
		if err := query.Scan(ctx); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return authnext.ErrInvalidSession
			}
			return err
		}
		if principal.EmailVerified {
			return nil
		}
		if principal.Email == "" {
			return authnext.ErrEmailRequired
		}
		secret, err := randomToken(32)
		if err != nil {
			return fmt.Errorf("generate email verification credential: %w", err)
		}
		token := emailVerificationTokenPrefix + secret
		code, err := emailVerificationCode()
		if err != nil {
			return fmt.Errorf("generate email verification code: %w", err)
		}

		row := emailVerificationRow{
			TokenHMAC: s.emailVerificationHMAC(token), PrincipalID: principal.ID, Email: principal.Email,
			CreatedAt: now.UnixMilli(), ExpiresAt: expires.UnixMilli(), Code: code,
		}
		verificationURL, err := credentialURL(s.cfg.EmailVerification.VerifyURL, token)
		if err != nil {
			return err
		}
		if _, err := tx.NewUpdate().Model((*emailVerificationRow)(nil)).Set("consumed_at = ?", now.UnixMilli()).
			Where("principal_id = ? AND consumed_at = 0", principal.ID).Exec(ctx); err != nil {
			return err
		}
		if _, err := tx.NewInsert().Model(&row).Exec(ctx); err != nil {
			return err
		}
		if err := s.enqueueNotification(ctx, tx, notificationPayload{
			Type: emailVerificationNotification, Recipient: principal.Email, VerificationURL: verificationURL, Code: code,
			OccurredAt: now, ExpiresAt: expires,
		}); err != nil {
			return err
		}
		_, err = s.auditTrail.AppendTo(ctx, tx, auditmod.EventInput{
			TenantID: authnAuditTenant, PrincipalID: principal.ID,
			EventType: "email_verification_requested", TargetType: "principal", TargetID: principal.ID,
			Outcome: "success", Detail: map[string]any{"email_snapshot_bound": true},
		})
		return err
	})
	if err != nil {
		if errors.Is(err, authnext.ErrInvalidSession) || errors.Is(err, authnext.ErrEmailRequired) {
			return err
		}
		return fmt.Errorf("begin email verification: %w", err)
	}
	return nil
}

// maxCodeAttempts bounds 6-digit email-verification-code guesses (M2).
const maxCodeAttempts = 5

func (s *WebService) codeAttempt(code string) int {
	if v, ok := s.codeAttempts.Load(code); ok {
		return v.(int)
	}
	return 0
}

func (s *WebService) recordCodeAttempt(code string) {
	v := s.codeAttempt(code) + 1
	s.codeAttempts.Store(code, v)
}

// CompleteEmailVerification 支持链接 token 或 6 位验证码两种方式激活。
func (s *WebService) CompleteEmailVerification(ctx context.Context, token, code string) error {
	if !s.EmailVerificationEnabled() {
		return authnext.ErrEmailVerificationDisabled
	}
	now := s.now().UTC().UnixMilli()
	var verification emailVerificationRow
	var computedHMAC string
	switch {
	case token != "":
		if !strings.HasPrefix(token, emailVerificationTokenPrefix) || len(token) > 128 {
			return authnext.ErrInvalidEmailVerification
		}
		computedHMAC = s.emailVerificationHMAC(token)
		if err := s.db.NewSelect().Model(&verification).
			Where("token_hmac = ? AND consumed_at = 0 AND expires_at > ?", computedHMAC, now).Scan(ctx); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return authnext.ErrInvalidEmailVerification
			}
			return fmt.Errorf("load email verification credential: %w", err)
		}
		if subtle.ConstantTimeCompare([]byte(verification.TokenHMAC), []byte(computedHMAC)) != 1 {
			return authnext.ErrInvalidEmailVerification
		}
	case code != "":
		if len(code) != 6 {
			return authnext.ErrInvalidEmailVerification
		}
		// M2 (round-3 review): brute-force cap — 5 failed attempts lock the code.
		if s.codeAttempt(code) >= maxCodeAttempts {
			return authnext.ErrInvalidEmailVerification
		}
		if err := s.db.NewSelect().Model(&verification).
			Where("code = ? AND consumed_at = 0 AND expires_at > ?", code, now).Scan(ctx); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				s.recordCodeAttempt(code)
				return authnext.ErrInvalidEmailVerification
			}
			return fmt.Errorf("load email verification credential: %w", err)
		}
		s.codeAttempts.Delete(code) // 成功即清零
	default:
		return authnext.ErrInvalidEmailVerification
	}
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		result, err := tx.NewUpdate().Model((*emailVerificationRow)(nil)).Set("consumed_at = ?", now).
			Where("token_hmac = ? AND consumed_at = 0 AND expires_at > ?", verification.TokenHMAC, now).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return authnext.ErrInvalidEmailVerification
		}
		result, err = tx.NewUpdate().Model((*principalRow)(nil)).Set("email_verified = ?", true).Set("activation_required = ?", false).Set("updated_at = ?", now).
			Where("id = ? AND status = 'active' AND email = ? AND email_verified = ?", verification.PrincipalID, verification.Email, false).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return authnext.ErrInvalidEmailVerification
		}
		_, err = s.auditTrail.AppendTo(ctx, tx, auditmod.EventInput{
			TenantID: authnAuditTenant, PrincipalID: verification.PrincipalID,
			EventType: "email_verification_completed", TargetType: "principal", TargetID: verification.PrincipalID,
			Outcome: "success", Detail: map[string]any{"email_snapshot_matched": true},
		})
		return err
	})
	if err != nil {
		if errors.Is(err, authnext.ErrInvalidEmailVerification) {
			return err
		}
		return fmt.Errorf("complete email verification: %w", err)
	}
	// 验证通过 → 触发注册后续(自动建租户)。失败不回滚验证结果,记日志即可。
	if s.VerifiedHook != nil {
		if err := s.VerifiedHook(ctx, verification.PrincipalID, verification.Email); err != nil {
			slog.Error("verified hook", "principal", verification.PrincipalID, "err", err)
		}
	}
	return nil
}

// randomVerificationCode 生成 6 位数字邮箱验证码。
func randomVerificationCode() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", int(binary.BigEndian.Uint32(b))%1000000), nil
}

func (s *WebService) emailVerificationHMAC(token string) string {
	mac := hmac.New(sha256.New, s.mfaPurposeKey("email-verification:v1"))
	_, _ = mac.Write([]byte("chaosplus:email-verification:v1\x00" + token))
	return hex.EncodeToString(mac.Sum(nil))
}
