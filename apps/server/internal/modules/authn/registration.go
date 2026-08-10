package authn

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/passwordx"
	auditmod "github.com/chaos-plus/chaosplus/internal/modules/audit"
	"github.com/uptrace/bun"
)

type RegistrationPrincipalCreator func(context.Context, bun.IDB, string, string, string, time.Time) (string, error)

func (s *WebService) configureRegistration() error {
	if !s.cfg.Registration.Enabled {
		return nil
	}
	if !s.Enabled() || !s.EmailVerificationEnabled() || !s.NotificationEnabled() || s.registrationCreator == nil {
		return errors.New("authn registration requires the web flow, email verification, notification delivery, and an identity creator")
	}
	return nil
}

func (s *WebService) RegistrationEnabled() bool {
	return s != nil && s.Enabled() && s.cfg.Registration.Enabled && s.EmailVerificationEnabled() && s.NotificationEnabled() && s.registrationCreator != nil
}

func (s *WebService) Capabilities() authnext.Capabilities {
	if s == nil {
		return authnext.Capabilities{}
	}
	return authnext.Capabilities{
		Registration: s.RegistrationEnabled(), PasswordRecovery: s.RecoveryEnabled(),
		Passkey: s.cfg.Enabled && s.web.Enabled && s.cfg.Passkey.Enabled && s.passkeys != nil,
	}
}

func (s *WebService) Register(ctx context.Context, email, password, displayName string) error {
	if !s.RegistrationEnabled() {
		return authnext.ErrRegistrationDisabled
	}
	email, displayName = strings.ToLower(strings.TrimSpace(email)), strings.TrimSpace(displayName)
	parsed, err := mail.ParseAddress(email)
	if len(email) > 320 || len(password) < 8 || len(password) > 1024 || len(displayName) > 128 || err != nil || !strings.EqualFold(parsed.Address, email) {
		return authnext.ErrInvalidRegistration
	}
	if codeSendThrottled(email) {
		return ErrVerificationCodeThrottled
	}
	passwordHash, err := passwordx.Hash(password)
	if err != nil {
		return fmt.Errorf("hash registration password: %w", err)
	}
	secret, err := randomToken(32)
	if err != nil {
		return fmt.Errorf("generate registration verification credential: %w", err)
	}
	token := emailVerificationTokenPrefix + secret
	verificationURL, err := credentialURL(s.cfg.EmailVerification.VerifyURL, token)
	if err != nil {
		return err
	}
	code, err := randomVerificationCode()
	if err != nil {
		return fmt.Errorf("generate verification code: %w", err)
	}
	now := s.now().UTC()
	expires := now.Add(s.cfg.EmailVerification.TokenTTL)
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		principalID, err := s.registrationCreator(ctx, tx, email, passwordHash, displayName, now)
		if err != nil {
			return err
		}
		verification := emailVerificationRow{
			TokenHMAC: s.emailVerificationHMAC(token), PrincipalID: principalID, Email: email,
			CreatedAt: now.UnixMilli(), ExpiresAt: expires.UnixMilli(), Code: code,
		}
		if _, err := tx.NewInsert().Model(&verification).Exec(ctx); err != nil {
			return err
		}
		if err := s.enqueueNotification(ctx, tx, notificationPayload{
			Type: emailVerificationNotification, Recipient: email, VerificationURL: verificationURL, Code: code,
			OccurredAt: now, ExpiresAt: expires,
		}); err != nil {
			return err
		}
		_, err = s.auditTrail.AppendTo(ctx, tx, auditmod.EventInput{
			TenantID: authnAuditTenant, PrincipalID: principalID,
			EventType: "principal_registration_requested", TargetType: "principal", TargetID: principalID,
			Outcome: "success", Detail: map[string]any{"activation_required": true, "tenant_membership_created": false},
		})
		return err
	})
	if errors.Is(err, authnext.ErrRegistrationConflict) {
		return s.resendRegistrationCode(ctx, email)
	}
	if errors.Is(err, authnext.ErrInvalidRegistration) {
		return err
	}
	if err != nil {
		return fmt.Errorf("register principal: %w", err)
	}
	return nil
}

// resendRegistrationCode 对已存在但未激活的注册邮箱重新生成并下发一次验证码;
// 已激活账号静默忽略(可直接登录)。
func (s *WebService) resendRegistrationCode(ctx context.Context, email string) error {
	var principal principalRow
	if err := s.db.NewSelect().Model(&principal).Where("login_name = ?", email).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return authnext.ErrRegistrationConflict
		}
		return fmt.Errorf("load pending principal for resend: %w", err)
	}
	if !principal.ActivationRequired {
		return authnext.ErrRegistrationConflict
	}
	secret, err := randomToken(32)
	if err != nil {
		return fmt.Errorf("generate resend credential: %w", err)
	}
	token := emailVerificationTokenPrefix + secret
	code, err := emailVerificationCode()
	if err != nil {
		return fmt.Errorf("generate resend code: %w", err)
	}
	now := s.now().UTC()
	expires := now.Add(s.cfg.EmailVerification.TokenTTL)
	verificationURL, err := credentialURL(s.cfg.EmailVerification.VerifyURL, token)
	if err != nil {
		return err
	}
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewUpdate().Model((*emailVerificationRow)(nil)).
			Set("consumed_at = ?", now.UnixMilli()).
			Where("principal_id = ? AND consumed_at = 0", principal.ID).Exec(ctx); err != nil {
			return err
		}
		row := emailVerificationRow{
			TokenHMAC: s.emailVerificationHMAC(token), PrincipalID: principal.ID, Email: email,
			CreatedAt: now.UnixMilli(), ExpiresAt: expires.UnixMilli(), Code: code,
		}
		if _, err := tx.NewInsert().Model(&row).Exec(ctx); err != nil {
			return err
		}
		return s.enqueueNotification(ctx, tx, notificationPayload{
			Type: emailVerificationNotification, Recipient: email, VerificationURL: verificationURL, Code: code,
			OccurredAt: now, ExpiresAt: expires,
		})
	})
}
