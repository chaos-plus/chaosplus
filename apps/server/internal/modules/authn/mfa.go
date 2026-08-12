package authn

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/passwordx"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/hotp"
	"github.com/pquerna/otp/totp"
	"github.com/uptrace/bun"
)

const (
	mfaCipherVersion = "v1"
	totpPeriod       = int64(30)
)

var (
	ErrInvalidMFA                   = authnext.ErrInvalidMFA
	ErrMFAChallenge                 = authnext.ErrMFAChallenge
	ErrMFAEnrollment                = authnext.ErrMFAEnrollment
	ErrMFAAlreadyOn                 = authnext.ErrMFAAlreadyOn
	ErrMFANotEnabled                = authnext.ErrMFANotEnabled
	errUnsupportedAuthnCiphertext   = errors.New("unsupported authentication ciphertext")
	errInvalidAuthnCiphertext       = errors.New("invalid authentication ciphertext")
	errDecryptAuthenticationPayload = errors.New("decrypt authentication data")
)

type mfaEnrollmentRow struct {
	bun.BaseModel    `bun:"table:iam_mfa_enrollments"`
	PrincipalID      guid.ID `bun:"principal_id,pk"`
	SecretCiphertext string
	CreatedAt        int64
	ExpiresAt        int64
}

type recoveryCodeRow struct {
	bun.BaseModel `bun:"table:iam_recovery_codes"`
	PrincipalID   guid.ID `bun:"principal_id,pk"`
	CodeHash      string  `bun:"code_hash,pk"`
	CreatedAt     int64
	UsedAt        int64
}

type mfaChallengeRow struct {
	bun.BaseModel `bun:"table:iam_mfa_challenges"`
	IDHash        string `bun:"id_hash,pk"`
	PrincipalID   guid.ID
	ReturnURL     string
	CreatedAt     int64
	ExpiresAt     int64
	ConsumedAt    int64
	Attempts      int
}

func parseMFAKey(encoded string) ([]byte, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, errors.New("authn MFA encryption key is required when browser authentication is enabled")
	}
	for _, decoder := range []*base64.Encoding{base64.RawStdEncoding, base64.StdEncoding, base64.RawURLEncoding, base64.URLEncoding} {
		key, err := decoder.DecodeString(encoded)
		if err == nil && len(key) == 32 {
			return key, nil
		}
	}
	return nil, errors.New("authn MFA encryption key must be base64-encoded 32 bytes")
}

func (s *WebService) encryptMFASecret(principalID guid.ID, secret string) (string, error) {
	return s.encryptAuthnData("v1", principalID.String(), []byte(secret))
}

func (s *WebService) encryptAuthnData(purpose, associatedData string, plain []byte) (string, error) {
	block, err := aes.NewCipher(s.mfaPurposeKey("encryption:" + purpose))
	if err != nil {
		return "", fmt.Errorf("create authentication cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create authentication AEAD: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate authentication nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, plain, []byte(associatedData))
	return mfaCipherVersion + "." + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (s *WebService) decryptMFASecret(principalID guid.ID, encoded string) (string, error) {
	plain, err := s.decryptAuthnData("v1", principalID.String(), encoded)
	return string(plain), err
}

func (s *WebService) decryptAuthnData(purpose, associatedData, encoded string) ([]byte, error) {
	version, payload, ok := strings.Cut(encoded, ".")
	if !ok || version != mfaCipherVersion {
		return nil, errUnsupportedAuthnCiphertext
	}
	sealed, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, errInvalidAuthnCiphertext
	}
	block, err := aes.NewCipher(s.mfaPurposeKey("encryption:" + purpose))
	if err != nil {
		return nil, fmt.Errorf("create authentication cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(sealed) < gcm.NonceSize() {
		return nil, errInvalidAuthnCiphertext
	}
	nonce, ciphertext := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, []byte(associatedData))
	if err != nil {
		return nil, errDecryptAuthenticationPayload
	}
	return plain, nil
}

func (s *WebService) MFAStatus(ctx context.Context, authorization, cookieHeader string) (authnext.MFAStatus, error) {
	claims, err := s.Authenticate(ctx, authorization, cookieHeader)
	if err != nil {
		return authnext.MFAStatus{}, err
	}
	var credential credentialRow
	if err := s.db.NewSelect().Model(&credential).Where("principal_id = ?", claims.PrincipalID).Scan(ctx); err != nil {
		return authnext.MFAStatus{}, fmt.Errorf("load MFA status: %w", err)
	}
	remaining, err := s.db.NewSelect().Model((*recoveryCodeRow)(nil)).Where("principal_id = ? AND used_at = 0", claims.PrincipalID).Count(ctx)
	if err != nil {
		return authnext.MFAStatus{}, fmt.Errorf("count recovery codes: %w", err)
	}
	return authnext.MFAStatus{TOTPEnabled: credential.MFARequired, RecoveryCodesRemaining: remaining}, nil
}

func (s *WebService) BeginTOTPEnrollment(ctx context.Context, authorization, cookieHeader, currentPassword string) (authnext.MFAEnrollment, error) {
	claims, err := s.Authenticate(ctx, authorization, cookieHeader)
	if err != nil {
		return authnext.MFAEnrollment{}, err
	}
	if err := s.ensureRecoveryCooldownExpired(ctx, claims.PrincipalID); err != nil {
		return authnext.MFAEnrollment{}, err
	}
	principal, credential, err := s.verifyCurrentPassword(ctx, claims.PrincipalID, currentPassword)
	if err != nil {
		return authnext.MFAEnrollment{}, err
	}
	if credential.MFARequired {
		return authnext.MFAEnrollment{}, ErrMFAAlreadyOn
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: s.cfg.MFA.Issuer, AccountName: principal.LoginName, Period: uint(totpPeriod), SecretSize: 20, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		return authnext.MFAEnrollment{}, fmt.Errorf("generate TOTP secret: %w", err)
	}
	ciphertext, err := s.encryptMFASecret(claims.PrincipalID, key.Secret())
	if err != nil {
		return authnext.MFAEnrollment{}, err
	}
	now := s.now().UTC()
	row := mfaEnrollmentRow{PrincipalID: claims.PrincipalID, SecretCiphertext: ciphertext, CreatedAt: now.UnixMilli(), ExpiresAt: now.Add(s.cfg.MFA.EnrollmentTTL).UnixMilli()}
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewDelete().Model((*mfaEnrollmentRow)(nil)).Where("principal_id = ?", claims.PrincipalID).Exec(ctx); err != nil {
			return err
		}
		if _, err := tx.NewInsert().Model(&row).Exec(ctx); err != nil {
			return err
		}
		return s.appendSecurityAudit(ctx, tx, claims.PrincipalID, "mfa_enrollment_started", "success")
	})
	if err != nil {
		return authnext.MFAEnrollment{}, fmt.Errorf("store TOTP enrollment: %w", err)
	}
	return authnext.MFAEnrollment{Secret: key.Secret(), ProvisioningURI: key.URL(), ExpiresAt: time.UnixMilli(row.ExpiresAt).UTC()}, nil
}

func (s *WebService) ConfirmTOTPEnrollment(ctx context.Context, authorization, cookieHeader, code string) (authnext.MFAConfirmation, error) {
	claims, err := s.Authenticate(ctx, authorization, cookieHeader)
	if err != nil {
		return authnext.MFAConfirmation{}, err
	}
	now := s.now().UTC()
	var recoveryCodes []string
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var enrollment mfaEnrollmentRow
		if err := tx.NewSelect().Model(&enrollment).Where("principal_id = ? AND expires_at > ?", claims.PrincipalID, now.UnixMilli()).Scan(ctx); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrMFAEnrollment
			}
			return err
		}
		secret, err := s.decryptMFASecret(claims.PrincipalID, enrollment.SecretCiphertext)
		if err != nil {
			return err
		}
		step, valid := validateTOTP(code, secret, now)
		if !valid {
			return ErrInvalidMFA
		}
		recoveryCodes, err = s.replaceRecoveryCodes(ctx, tx, claims.PrincipalID, now)
		if err != nil {
			return err
		}
		result, err := tx.NewUpdate().Model((*credentialRow)(nil)).Set("totp_secret = ?", enrollment.SecretCiphertext).
			Set("mfa_required = ?", true).Set("totp_last_used_step = ?", step).Set("updated_at = ?", now.UnixMilli()).
			Where("principal_id = ? AND mfa_required = ?", claims.PrincipalID, false).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return ErrMFAAlreadyOn
		}
		if _, err := tx.NewDelete().Model((*mfaEnrollmentRow)(nil)).Where("principal_id = ?", claims.PrincipalID).Exec(ctx); err != nil {
			return err
		}
		if err := s.revokeOtherAuthentication(ctx, tx, claims.PrincipalID, currentSessionHash(cookieHeader, s.web.CookieName), now.UnixMilli()); err != nil {
			return err
		}
		return s.appendSecurityAudit(ctx, tx, claims.PrincipalID, "mfa_enabled", "success")
	})
	if err != nil {
		return authnext.MFAConfirmation{}, fmt.Errorf("confirm TOTP enrollment: %w", err)
	}
	return authnext.MFAConfirmation{RecoveryCodes: recoveryCodes}, nil
}

func (s *WebService) VerifyLoginMFA(ctx context.Context, challengeID, code string) (authnext.LoginResult, error) {
	if strings.TrimSpace(challengeID) == "" || strings.TrimSpace(code) == "" {
		return authnext.LoginResult{}, ErrMFAChallenge
	}
	now := s.now().UTC()
	var result authnext.LoginResult
	var principalID guid.ID
	invalidCode := false
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var challenge mfaChallengeRow
		err := tx.NewSelect().Model(&challenge).Where("id_hash = ?", tokenHash(challengeID)).Scan(ctx)
		if err != nil || challenge.ConsumedAt != 0 || challenge.ExpiresAt <= now.UnixMilli() || challenge.Attempts >= s.cfg.MFA.MaxAttempts {
			return ErrMFAChallenge
		}
		principalID = challenge.PrincipalID
		var credential credentialRow
		if err := tx.NewSelect().Model(&credential).Where("principal_id = ? AND mfa_required = ?", challenge.PrincipalID, true).Scan(ctx); err != nil {
			return ErrMFAChallenge
		}
		valid, err := s.consumeFactor(ctx, tx, &credential, code, now)
		if err != nil {
			return err
		}
		if !valid {
			update := tx.NewUpdate().Model((*mfaChallengeRow)(nil)).
				Set("attempts = attempts + 1")
			if challenge.Attempts+1 >= s.cfg.MFA.MaxAttempts {
				update = update.Set("consumed_at = ?", now.UnixMilli())
			}
			result, err := update.Where("id_hash = ? AND consumed_at = 0 AND attempts = ?", challenge.IDHash, challenge.Attempts).Exec(ctx)
			if err != nil {
				return err
			}
			if affected, _ := result.RowsAffected(); affected != 1 {
				return ErrMFAChallenge
			}
			invalidCode = true
			return nil
		}
		updated, err := tx.NewUpdate().Model((*mfaChallengeRow)(nil)).Set("consumed_at = ?", now.UnixMilli()).
			Where("id_hash = ? AND consumed_at = 0", challenge.IDHash).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := updated.RowsAffected(); affected != 1 {
			return ErrMFAChallenge
		}
		sessionID, err := s.insertSession(ctx, tx, challenge.PrincipalID, now, authnext.Assurance{AuthTime: now, Level: 2, Methods: []string{"pwd", "mfa"}})
		if err != nil {
			return err
		}
		result = authnext.LoginResult{Status: "authenticated", ReturnURL: challenge.ReturnURL, SessionID: sessionID}
		return s.appendSecurityAudit(ctx, tx, challenge.PrincipalID, "mfa_login", "success")
	})
	if err != nil {
		return authnext.LoginResult{}, err
	}
	if invalidCode {
		s.audit(ctx, principalID, "mfa_login", "denied")
		return authnext.LoginResult{}, ErrInvalidMFA
	}
	return result, nil
}

func (s *WebService) RegenerateRecoveryCodes(ctx context.Context, authorization, cookieHeader, currentPassword, code string) (authnext.MFAConfirmation, error) {
	claims, err := s.Authenticate(ctx, authorization, cookieHeader)
	if err != nil {
		return authnext.MFAConfirmation{}, err
	}
	_, credential, err := s.verifyCurrentPassword(ctx, claims.PrincipalID, currentPassword)
	if err != nil {
		return authnext.MFAConfirmation{}, err
	}
	if !credential.MFARequired {
		return authnext.MFAConfirmation{}, ErrMFANotEnabled
	}
	now := s.now().UTC()
	var codes []string
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var current credentialRow
		if err := tx.NewSelect().Model(&current).Where("principal_id = ? AND mfa_required = ?", claims.PrincipalID, true).Scan(ctx); err != nil {
			return ErrMFANotEnabled
		}
		if valid, err := passwordx.Verify(current.PasswordHash, currentPassword); err != nil || !valid {
			return authnext.ErrInvalidCredentials
		}
		valid, err := s.consumeFactor(ctx, tx, &current, code, now)
		if err != nil {
			return err
		}
		if !valid {
			return ErrInvalidMFA
		}
		codes, err = s.replaceRecoveryCodes(ctx, tx, claims.PrincipalID, now)
		if err != nil {
			return err
		}
		if err := s.revokeOtherAuthentication(ctx, tx, claims.PrincipalID, currentSessionHash(cookieHeader, s.web.CookieName), now.UnixMilli()); err != nil {
			return err
		}
		return s.appendSecurityAudit(ctx, tx, claims.PrincipalID, "mfa_recovery_regenerated", "success")
	})
	if err != nil {
		return authnext.MFAConfirmation{}, fmt.Errorf("regenerate recovery codes: %w", err)
	}
	return authnext.MFAConfirmation{RecoveryCodes: codes}, nil
}

func (s *WebService) DisableTOTP(ctx context.Context, authorization, cookieHeader, currentPassword, code string) error {
	claims, err := s.Authenticate(ctx, authorization, cookieHeader)
	if err != nil {
		return err
	}
	_, credential, err := s.verifyCurrentPassword(ctx, claims.PrincipalID, currentPassword)
	if err != nil {
		return err
	}
	if !credential.MFARequired {
		return ErrMFANotEnabled
	}
	now := s.now().UTC()
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var current credentialRow
		if err := tx.NewSelect().Model(&current).Where("principal_id = ? AND mfa_required = ?", claims.PrincipalID, true).Scan(ctx); err != nil {
			return ErrMFANotEnabled
		}
		if valid, err := passwordx.Verify(current.PasswordHash, currentPassword); err != nil || !valid {
			return authnext.ErrInvalidCredentials
		}
		valid, err := s.consumeFactor(ctx, tx, &current, code, now)
		if err != nil {
			return err
		}
		if !valid {
			return ErrInvalidMFA
		}
		if _, err := tx.NewUpdate().Model((*credentialRow)(nil)).Set("totp_secret = ''").Set("mfa_required = ?", false).
			Set("totp_last_used_step = 0").Set("updated_at = ?", now.UnixMilli()).Where("principal_id = ?", claims.PrincipalID).Exec(ctx); err != nil {
			return err
		}
		for _, model := range []any{(*mfaEnrollmentRow)(nil), (*recoveryCodeRow)(nil), (*mfaChallengeRow)(nil)} {
			if _, err := tx.NewDelete().Model(model).Where("principal_id = ?", claims.PrincipalID).Exec(ctx); err != nil {
				return err
			}
		}
		if err := s.revokeOtherAuthentication(ctx, tx, claims.PrincipalID, currentSessionHash(cookieHeader, s.web.CookieName), now.UnixMilli()); err != nil {
			return err
		}
		return s.appendSecurityAudit(ctx, tx, claims.PrincipalID, "mfa_disabled", "success")
	})
	if err != nil {
		return fmt.Errorf("disable TOTP: %w", err)
	}
	return nil
}

func (s *WebService) createLoginChallenge(ctx context.Context, principalID guid.ID, returnURL string, now time.Time) (authnext.LoginResult, error) {
	challengeID, err := randomToken(32)
	if err != nil {
		return authnext.LoginResult{}, err
	}
	row := mfaChallengeRow{IDHash: tokenHash(challengeID), PrincipalID: principalID, ReturnURL: returnURL, CreatedAt: now.UnixMilli(), ExpiresAt: now.Add(s.cfg.MFA.ChallengeTTL).UnixMilli()}
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewDelete().Model((*mfaChallengeRow)(nil)).Where("principal_id = ? AND (consumed_at <> 0 OR expires_at <= ?)", principalID, now.UnixMilli()).Exec(ctx); err != nil {
			return err
		}
		_, err := tx.NewInsert().Model(&row).Exec(ctx)
		return err
	})
	if err != nil {
		return authnext.LoginResult{}, fmt.Errorf("create MFA challenge: %w", err)
	}
	expiresAt := time.UnixMilli(row.ExpiresAt).UTC()
	return authnext.LoginResult{Status: "mfa_required", ReturnURL: returnURL, ChallengeID: challengeID, ExpiresAt: &expiresAt, Methods: []string{"totp", "recovery_code"}}, nil
}

func (s *WebService) verifyCurrentPassword(ctx context.Context, principalID guid.ID, password string) (principalRow, credentialRow, error) {
	var principal principalRow
	if err := s.db.NewSelect().Model(&principal).Where("id = ? AND status = 'active'", principalID).Scan(ctx); err != nil {
		return principalRow{}, credentialRow{}, authnext.ErrInvalidCredentials
	}
	var credential credentialRow
	if err := s.db.NewSelect().Model(&credential).Where("principal_id = ?", principalID).Scan(ctx); err != nil {
		return principalRow{}, credentialRow{}, fmt.Errorf("load credential: %w", err)
	}
	valid, err := passwordx.Verify(credential.PasswordHash, password)
	if err != nil {
		return principalRow{}, credentialRow{}, fmt.Errorf("verify current password: %w", err)
	}
	if !valid {
		return principalRow{}, credentialRow{}, authnext.ErrInvalidCredentials
	}
	return principal, credential, nil
}

func (s *WebService) consumeFactor(ctx context.Context, db bun.IDB, credential *credentialRow, code string, now time.Time) (bool, error) {
	normalized := normalizeRecoveryCode(code)
	if len(strings.TrimSpace(code)) == 6 && strings.IndexFunc(strings.TrimSpace(code), func(r rune) bool { return r < '0' || r > '9' }) == -1 {
		secret, err := s.decryptMFASecret(credential.PrincipalID, credential.TOTPSecret)
		if err != nil {
			return false, err
		}
		step, valid := validateTOTP(code, secret, now)
		if !valid || step <= credential.TOTPLastUsedStep {
			return false, nil
		}
		result, err := db.NewUpdate().Model((*credentialRow)(nil)).Set("totp_last_used_step = ?", step).
			Where("principal_id = ? AND totp_last_used_step < ?", credential.PrincipalID, step).Exec(ctx)
		if err != nil {
			return false, err
		}
		affected, _ := result.RowsAffected()
		return affected == 1, nil
	}
	if normalized == "" {
		return false, nil
	}
	result, err := db.NewUpdate().Model((*recoveryCodeRow)(nil)).Set("used_at = ?", now.UnixMilli()).
		Where("principal_id = ? AND code_hash = ? AND used_at = 0", credential.PrincipalID, s.recoveryCodeHash(normalized)).Exec(ctx)
	if err != nil {
		return false, err
	}
	affected, _ := result.RowsAffected()
	return affected == 1, nil
}

func validateTOTP(code, secret string, now time.Time) (int64, bool) {
	code = strings.TrimSpace(code)
	counter := now.Unix() / totpPeriod
	for _, candidate := range []int64{counter, counter - 1, counter + 1} {
		if candidate < 0 {
			continue
		}
		valid, err := hotp.ValidateCustom(code, uint64(candidate), secret, hotp.ValidateOpts{Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
		if err == nil && valid {
			return candidate, true
		}
	}
	return 0, false
}

func (s *WebService) replaceRecoveryCodes(ctx context.Context, tx bun.Tx, principalID guid.ID, now time.Time) ([]string, error) {
	if _, err := tx.NewDelete().Model((*recoveryCodeRow)(nil)).Where("principal_id = ?", principalID).Exec(ctx); err != nil {
		return nil, err
	}
	codes := make([]string, 0, s.cfg.MFA.RecoveryCodes)
	rows := make([]recoveryCodeRow, 0, s.cfg.MFA.RecoveryCodes)
	for range s.cfg.MFA.RecoveryCodes {
		code, err := generateRecoveryCode()
		if err != nil {
			return nil, err
		}
		codes = append(codes, code)
		rows = append(rows, recoveryCodeRow{PrincipalID: principalID, CodeHash: s.recoveryCodeHash(normalizeRecoveryCode(code)), CreatedAt: now.UnixMilli()})
	}
	if _, err := tx.NewInsert().Model(&rows).Exec(ctx); err != nil {
		return nil, err
	}
	return codes, nil
}

func generateRecoveryCode() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate recovery code: %w", err)
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
	groups := make([]string, 0, (len(encoded)+3)/4)
	for start := 0; start < len(encoded); start += 4 {
		groups = append(groups, encoded[start:min(start+4, len(encoded))])
	}
	return strings.Join(groups, "-"), nil
}

func normalizeRecoveryCode(code string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(code)))
}

func (s *WebService) recoveryCodeHash(code string) string {
	mac := hmac.New(sha256.New, s.mfaPurposeKey("recovery:v1"))
	_, _ = mac.Write([]byte("platform:mfa:recovery:v1\x00" + code))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *WebService) mfaPurposeKey(purpose string) []byte {
	mac := hmac.New(sha256.New, s.mfaKey)
	_, _ = mac.Write([]byte("platform:mfa:key:" + purpose))
	return mac.Sum(nil)
}

func (s *WebService) insertSession(ctx context.Context, db bun.IDB, principalID guid.ID, now time.Time, assurance authnext.Assurance) (string, error) {
	token, err := randomToken(32)
	if err != nil {
		return "", err
	}
	absEnd := now.Add(s.web.SessionTTL)
	idleEnd := now.Add(s.web.IdleTTL)
	row := sessionRow{
		IDHash: tokenHash(token), PrincipalID: principalID, CreatedAt: now.UnixMilli(), AuthTime: assurance.AuthTime.UTC().UnixMilli(),
		ACR: assurance.Level, AMR: strings.Join(assurance.Methods, " "), LastSeenAt: now.UnixMilli(), ExpiresAt: idleEnd.UnixMilli(), AbsoluteExpiresAt: absEnd.UnixMilli(),
	}
	if _, err := db.NewInsert().Model(&row).Exec(ctx); err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return token, nil
}

func (s *WebService) revokeOtherAuthentication(ctx context.Context, tx bun.Tx, principalID guid.ID, currentSession string, now int64) error {
	query := tx.NewUpdate().Model((*sessionRow)(nil)).Set("revoked_at = ?", now).Where("principal_id = ? AND revoked_at = 0", principalID)
	if currentSession != "" {
		query = query.Where("id_hash <> ?", currentSession)
	}
	if _, err := query.Exec(ctx); err != nil {
		return err
	}
	_, err := tx.NewUpdate().Table("iam_refresh_tokens").Set("revoked_at = ?", now).Where("principal_id = ? AND revoked_at = 0", principalID).Exec(ctx)
	return err
}

func currentSessionHash(cookieHeader, name string) string {
	token, err := cookieValue(cookieHeader, name)
	if err != nil {
		return ""
	}
	return tokenHash(token)
}
