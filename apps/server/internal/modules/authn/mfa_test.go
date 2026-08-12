package authn

import (
	"encoding/base32"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMFAAuditFailuresRollBackSecurityMutations(t *testing.T) {
	t.Run("enrollment", func(t *testing.T) {
		service, principalID := newLocalService(t)
		now := time.Unix(1_785_600_000, 0).UTC()
		service.now = func() time.Time { return now }
		cookie := authenticatedCookie(t, service)
		rejectAuthnAudit(t, service, "mfa_enrollment_started")

		_, err := service.BeginTOTPEnrollment(t.Context(), "", cookie, "correct horse battery staple")
		require.ErrorContains(t, err, "append audit event")
		count, countErr := service.db.NewSelect().Model((*mfaEnrollmentRow)(nil)).Where("principal_id = ?", principalID).Count(t.Context())
		require.NoError(t, countErr)
		assert.Zero(t, count)
		assertAuthnAuditCount(t, service, "mfa_enrollment_started", 0)
	})

	t.Run("enable", func(t *testing.T) {
		service, principalID, cookie, enrollment, now := pendingMFAFixture(t)
		rejectAuthnAudit(t, service, "mfa_enabled")

		_, err := service.ConfirmTOTPEnrollment(t.Context(), "", cookie, mustTOTP(t, enrollment.Secret, now))
		require.ErrorContains(t, err, "append audit event")
		var credential credentialRow
		require.NoError(t, service.db.NewSelect().Model(&credential).Where("principal_id = ?", principalID).Scan(t.Context()))
		assert.False(t, credential.MFARequired)
		count, countErr := service.db.NewSelect().Model((*recoveryCodeRow)(nil)).Where("principal_id = ?", principalID).Count(t.Context())
		require.NoError(t, countErr)
		assert.Zero(t, count)
		count, countErr = service.db.NewSelect().Model((*mfaEnrollmentRow)(nil)).Where("principal_id = ?", principalID).Count(t.Context())
		require.NoError(t, countErr)
		assert.Equal(t, 1, count)
		assertAuthnAuditCount(t, service, "mfa_enabled", 0)
	})

	t.Run("login", func(t *testing.T) {
		service, principalID, _, enrollment, _, now := enabledMFAFixture(t)
		now = now.Add(30 * time.Second)
		service.now = func() time.Time { return now }
		challenge, err := service.BeginLogin(t.Context(), "admin", "correct horse battery staple", "")
		require.NoError(t, err)
		var before credentialRow
		require.NoError(t, service.db.NewSelect().Model(&before).Where("principal_id = ?", principalID).Scan(t.Context()))
		sessionsBefore, err := service.db.NewSelect().Model((*sessionRow)(nil)).Where("principal_id = ?", principalID).Count(t.Context())
		require.NoError(t, err)
		rejectAuthnAudit(t, service, "mfa_login")

		_, err = service.VerifyLoginMFA(t.Context(), challenge.ChallengeID, mustTOTP(t, enrollment.Secret, now))
		require.ErrorContains(t, err, "append audit event")
		var challengeRow mfaChallengeRow
		require.NoError(t, service.db.NewSelect().Model(&challengeRow).Where("id_hash = ?", tokenHash(challenge.ChallengeID)).Scan(t.Context()))
		assert.Zero(t, challengeRow.ConsumedAt)
		var after credentialRow
		require.NoError(t, service.db.NewSelect().Model(&after).Where("principal_id = ?", principalID).Scan(t.Context()))
		assert.Equal(t, before.TOTPLastUsedStep, after.TOTPLastUsedStep)
		sessionsAfter, err := service.db.NewSelect().Model((*sessionRow)(nil)).Where("principal_id = ?", principalID).Count(t.Context())
		require.NoError(t, err)
		assert.Equal(t, sessionsBefore, sessionsAfter)
		assertAuthnAuditCount(t, service, "mfa_login", 0)
	})

	t.Run("recovery code regeneration", func(t *testing.T) {
		service, principalID, cookie, enrollment, _, now := enabledMFAFixture(t)
		now = now.Add(30 * time.Second)
		service.now = func() time.Time { return now }
		var beforeHashes []string
		require.NoError(t, service.db.NewSelect().Model((*recoveryCodeRow)(nil)).Column("code_hash").Where("principal_id = ?", principalID).Order("code_hash ASC").Scan(t.Context(), &beforeHashes))
		extraSession, err := service.insertSession(t.Context(), service.db, parseGUID(principalID), now, authnext.Assurance{AuthTime: now, Level: 1, Methods: []string{"pwd"}})
		require.NoError(t, err)
		rejectAuthnAudit(t, service, "mfa_recovery_regenerated")

		_, err = service.RegenerateRecoveryCodes(t.Context(), "", cookie, "correct horse battery staple", mustTOTP(t, enrollment.Secret, now))
		require.ErrorContains(t, err, "append audit event")
		var afterHashes []string
		require.NoError(t, service.db.NewSelect().Model((*recoveryCodeRow)(nil)).Column("code_hash").Where("principal_id = ?", principalID).Order("code_hash ASC").Scan(t.Context(), &afterHashes))
		assert.Equal(t, beforeHashes, afterHashes)
		var revokedAt int64
		require.NoError(t, service.db.NewSelect().Model((*sessionRow)(nil)).Column("revoked_at").Where("id_hash = ?", tokenHash(extraSession)).Scan(t.Context(), &revokedAt))
		assert.Zero(t, revokedAt)
		assertAuthnAuditCount(t, service, "mfa_recovery_regenerated", 0)
	})

	t.Run("disable", func(t *testing.T) {
		service, principalID, cookie, _, recoveryCodes, _ := enabledMFAFixture(t)
		rejectAuthnAudit(t, service, "mfa_disabled")

		err := service.DisableTOTP(t.Context(), "", cookie, "correct horse battery staple", recoveryCodes[0])
		require.ErrorContains(t, err, "append audit event")
		var credential credentialRow
		require.NoError(t, service.db.NewSelect().Model(&credential).Where("principal_id = ?", principalID).Scan(t.Context()))
		assert.True(t, credential.MFARequired)
		var usedAt int64
		require.NoError(t, service.db.NewSelect().Model((*recoveryCodeRow)(nil)).Column("used_at").Where("principal_id = ? AND code_hash = ?", principalID, service.recoveryCodeHash(normalizeRecoveryCode(recoveryCodes[0]))).Scan(t.Context(), &usedAt))
		assert.Zero(t, usedAt)
		assertAuthnAuditCount(t, service, "mfa_disabled", 0)
	})
}

func TestTOTPEnrollmentAndLogin(t *testing.T) {
	service, principalID := newLocalService(t)
	now := time.Unix(1_785_600_000, 0).UTC()
	service.now = func() time.Time { return now }
	sessionID, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	cookie := service.SessionCookie(sessionID)

	status, err := service.MFAStatus(t.Context(), "", cookie)
	require.NoError(t, err)
	assert.False(t, status.TOTPEnabled)
	assert.Zero(t, status.RecoveryCodesRemaining)

	_, err = service.BeginTOTPEnrollment(t.Context(), "", cookie, "wrong password")
	assert.ErrorIs(t, err, authnext.ErrInvalidCredentials)
	enrollment, err := service.BeginTOTPEnrollment(t.Context(), "", cookie, "correct horse battery staple")
	require.NoError(t, err)
	assert.NotEmpty(t, enrollment.Secret)
	assert.Contains(t, enrollment.ProvisioningURI, "otpauth://totp/")
	var stored mfaEnrollmentRow
	require.NoError(t, service.db.NewSelect().Model(&stored).Where("principal_id = ?", principalID).Scan(t.Context()))
	assert.True(t, strings.HasPrefix(stored.SecretCiphertext, "v1."))
	assert.NotContains(t, stored.SecretCiphertext, enrollment.Secret)

	currentCode := mustTOTP(t, enrollment.Secret, now)
	_, err = service.ConfirmTOTPEnrollment(t.Context(), "", cookie, differentCode(currentCode))
	assert.ErrorIs(t, err, ErrInvalidMFA)
	confirmation, err := service.ConfirmTOTPEnrollment(t.Context(), "", cookie, currentCode)
	require.NoError(t, err)
	require.Len(t, confirmation.RecoveryCodes, 10)
	assert.Len(t, uniqueStrings(confirmation.RecoveryCodes), 10)

	status, err = service.MFAStatus(t.Context(), "", cookie)
	require.NoError(t, err)
	assert.True(t, status.TOTPEnabled)
	assert.Equal(t, 10, status.RecoveryCodesRemaining)
	_, err = service.BeginTOTPEnrollment(t.Context(), "", cookie, "correct horse battery staple")
	assert.ErrorIs(t, err, ErrMFAAlreadyOn)

	_, _, err = service.Login(t.Context(), "admin", "correct horse battery staple", "")
	assert.ErrorIs(t, err, authnext.ErrAdditionalVerification)
	challenge, err := service.BeginLogin(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	assert.Equal(t, "mfa_required", challenge.Status)
	assert.Empty(t, challenge.SessionID)
	require.NotNil(t, challenge.ExpiresAt)

	_, err = service.VerifyLoginMFA(t.Context(), challenge.ChallengeID, mustTOTP(t, enrollment.Secret, now))
	assert.ErrorIs(t, err, ErrInvalidMFA, "the enrollment TOTP step cannot be replayed")
	now = now.Add(30 * time.Second)
	result, err := service.VerifyLoginMFA(t.Context(), challenge.ChallengeID, mustTOTP(t, enrollment.Secret, now))
	require.NoError(t, err)
	assert.Equal(t, "authenticated", result.Status)
	claims, err := service.Authenticate(t.Context(), "", service.SessionCookie(result.SessionID))
	require.NoError(t, err)
	assert.Equal(t, 2, claims.ACR)
	assert.Equal(t, []string{"pwd", "mfa"}, claims.AMR)
	assert.Equal(t, now, claims.AuthTime)
	_, err = service.VerifyLoginMFA(t.Context(), challenge.ChallengeID, confirmation.RecoveryCodes[0])
	assert.ErrorIs(t, err, ErrMFAChallenge)
}

func TestRecoveryCodesRegenerationAndDisable(t *testing.T) {
	service, _ := newLocalService(t)
	now := time.Unix(1_785_600_000, 0).UTC()
	service.now = func() time.Time { return now }
	sessionID, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	cookie := service.SessionCookie(sessionID)
	enrollment, err := service.BeginTOTPEnrollment(t.Context(), "", cookie, "correct horse battery staple")
	require.NoError(t, err)
	confirmation, err := service.ConfirmTOTPEnrollment(t.Context(), "", cookie, mustTOTP(t, enrollment.Secret, now))
	require.NoError(t, err)

	challenge, err := service.BeginLogin(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	login, err := service.VerifyLoginMFA(t.Context(), challenge.ChallengeID, strings.ToLower(strings.ReplaceAll(confirmation.RecoveryCodes[0], "-", " ")))
	require.NoError(t, err)
	recoveryCookie := service.SessionCookie(login.SessionID)
	status, err := service.MFAStatus(t.Context(), "", recoveryCookie)
	require.NoError(t, err)
	assert.Equal(t, 9, status.RecoveryCodesRemaining)

	challenge, err = service.BeginLogin(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	_, err = service.VerifyLoginMFA(t.Context(), challenge.ChallengeID, confirmation.RecoveryCodes[0])
	assert.ErrorIs(t, err, ErrInvalidMFA)

	now = now.Add(30 * time.Second)
	_, err = service.RegenerateRecoveryCodes(t.Context(), "", cookie, "wrong password", mustTOTP(t, enrollment.Secret, now))
	assert.ErrorIs(t, err, authnext.ErrInvalidCredentials)
	replacement, err := service.RegenerateRecoveryCodes(t.Context(), "", cookie, "correct horse battery staple", mustTOTP(t, enrollment.Secret, now))
	require.NoError(t, err)
	require.Len(t, replacement.RecoveryCodes, 10)
	assert.NotContains(t, replacement.RecoveryCodes, confirmation.RecoveryCodes[1])
	_, err = service.Authenticate(t.Context(), "", recoveryCookie)
	assert.ErrorIs(t, err, ErrInvalidSession, "regeneration revokes other sessions")

	now = now.Add(30 * time.Second)
	currentCode := mustTOTP(t, enrollment.Secret, now)
	assert.ErrorIs(t, service.DisableTOTP(t.Context(), "", cookie, "correct horse battery staple", differentCode(currentCode)), ErrInvalidMFA)
	require.NoError(t, service.DisableTOTP(t.Context(), "", cookie, "correct horse battery staple", currentCode))
	status, err = service.MFAStatus(t.Context(), "", cookie)
	require.NoError(t, err)
	assert.False(t, status.TOTPEnabled)
	assert.Zero(t, status.RecoveryCodesRemaining)
	_, _, err = service.Login(t.Context(), "admin", "correct horse battery staple", "")
	assert.NoError(t, err)
	assert.ErrorIs(t, service.DisableTOTP(t.Context(), "", cookie, "correct horse battery staple", "000000"), ErrMFANotEnabled)
	_, err = service.RegenerateRecoveryCodes(t.Context(), "", cookie, "correct horse battery staple", "000000")
	assert.ErrorIs(t, err, ErrMFANotEnabled)
}

func TestMFAChallengeAttemptsAndExpiration(t *testing.T) {
	service, principalID := newLocalService(t)
	service.cfg.MFA.MaxAttempts = 2
	now := time.Unix(1_785_600_000, 0).UTC()
	service.now = func() time.Time { return now }
	sessionID, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	cookie := service.SessionCookie(sessionID)
	enrollment, err := service.BeginTOTPEnrollment(t.Context(), "", cookie, "correct horse battery staple")
	require.NoError(t, err)
	_, err = service.ConfirmTOTPEnrollment(t.Context(), "", cookie, mustTOTP(t, enrollment.Secret, now))
	require.NoError(t, err)
	now = now.Add(30 * time.Second)

	challenge, err := service.BeginLogin(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	invalidCode := differentCode(mustTOTP(t, enrollment.Secret, now))
	for range 2 {
		_, err = service.VerifyLoginMFA(t.Context(), challenge.ChallengeID, invalidCode)
		assert.ErrorIs(t, err, ErrInvalidMFA)
	}
	var row mfaChallengeRow
	require.NoError(t, service.db.NewSelect().Model(&row).Where("id_hash = ?", tokenHash(challenge.ChallengeID)).Scan(t.Context()))
	assert.Equal(t, 2, row.Attempts)
	assert.NotZero(t, row.ConsumedAt)
	_, err = service.VerifyLoginMFA(t.Context(), challenge.ChallengeID, mustTOTP(t, enrollment.Secret, now))
	assert.ErrorIs(t, err, ErrMFAChallenge)

	challenge, err = service.BeginLogin(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	now = now.Add(service.cfg.MFA.ChallengeTTL + time.Second)
	_, err = service.VerifyLoginMFA(t.Context(), challenge.ChallengeID, mustTOTP(t, enrollment.Secret, now))
	assert.ErrorIs(t, err, ErrMFAChallenge)

	_, err = service.BeginTOTPEnrollment(t.Context(), "", cookie, "correct horse battery staple")
	assert.ErrorIs(t, err, ErrMFAAlreadyOn)
	_, err = service.db.NewUpdate().Model((*credentialRow)(nil)).Set("mfa_required = ?", false).Set("totp_secret = ''").Where("principal_id = ?", principalID).Exec(t.Context())
	require.NoError(t, err)
	service.cfg.MFA.EnrollmentTTL = time.Second
	enrollment, err = service.BeginTOTPEnrollment(t.Context(), "", cookie, "correct horse battery staple")
	require.NoError(t, err)
	now = now.Add(2 * time.Second)
	_, err = service.ConfirmTOTPEnrollment(t.Context(), "", cookie, mustTOTP(t, enrollment.Secret, now))
	assert.ErrorIs(t, err, ErrMFAEnrollment)
}

func TestMFAKeyAndCipherValidation(t *testing.T) {
	key := make([]byte, 32)
	for index := range key {
		key[index] = byte(index + 1)
	}
	for _, encoded := range []string{
		base64.RawStdEncoding.EncodeToString(key), base64.StdEncoding.EncodeToString(key),
		base64.RawURLEncoding.EncodeToString(key), base64.URLEncoding.EncodeToString(key),
	} {
		parsed, err := parseMFAKey(encoded)
		require.NoError(t, err)
		assert.Equal(t, key, parsed)
	}
	_, err := parseMFAKey("")
	assert.Error(t, err)
	_, err = parseMFAKey("short")
	assert.Error(t, err)

	service, principalID := newLocalService(t)
	ciphertext, err := service.encryptMFASecret(parseGUID(principalID), "JBSWY3DPEHPK3PXP")
	require.NoError(t, err)
	secret, err := service.decryptMFASecret(parseGUID(principalID), ciphertext)
	require.NoError(t, err)
	assert.Equal(t, "JBSWY3DPEHPK3PXP", secret)
	_, err = service.decryptMFASecret(testID("other"), ciphertext)
	assert.Error(t, err)
	_, err = service.decryptMFASecret(parseGUID(principalID), "v2.invalid")
	assert.Error(t, err)
	_, err = service.decryptMFASecret(parseGUID(principalID), "v1.invalid")
	assert.Error(t, err)

	assert.Empty(t, normalizeRecoveryCode(" -- "))
	assert.Equal(t, "ABCD1234", normalizeRecoveryCode(" abcd-1234 "))
	recoveryCode, err := generateRecoveryCode()
	require.NoError(t, err)
	assert.Len(t, normalizeRecoveryCode(recoveryCode), 26)
	assert.NotContains(t, recoveryCode, "=")
	assert.False(t, func() bool { _, ok := validateTOTP("invalid", "bad", time.Now()); return ok }())
}

func TestMFAFailsClosedForInvalidAuthenticationAndStorage(t *testing.T) {
	service, _ := newLocalService(t)
	ctx := t.Context()
	_, err := service.MFAStatus(ctx, "", "")
	assert.Error(t, err)
	_, err = service.BeginTOTPEnrollment(ctx, "", "", "password")
	assert.Error(t, err)
	_, err = service.ConfirmTOTPEnrollment(ctx, "", "", "123456")
	assert.Error(t, err)
	_, err = service.VerifyLoginMFA(ctx, "", "")
	assert.ErrorIs(t, err, ErrMFAChallenge)
	_, err = service.RegenerateRecoveryCodes(ctx, "", "", "password", "123456")
	assert.Error(t, err)
	assert.Error(t, service.DisableTOTP(ctx, "", "", "password", "123456"))
	assert.Empty(t, currentSessionHash("", service.web.CookieName))

	_, err = service.decryptMFASecret(testID("principal"), "v1.AAAA")
	assert.Error(t, err)

	credential := credentialRow{PrincipalID: testID("principal"), TOTPSecret: "v1.invalid"}
	valid, err := service.consumeFactor(ctx, service.db, &credential, "123456", time.Now())
	assert.False(t, valid)
	assert.Error(t, err)
	valid, err = service.consumeFactor(ctx, service.db, &credential, " -- ", time.Now())
	assert.False(t, valid)
	require.NoError(t, err)
}

func TestMFAFailsClosedForCorruptCredentialAndClosedDatabase(t *testing.T) {
	service, principalID := newLocalService(t)
	sessionID, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	cookie := service.SessionCookie(sessionID)
	_, err = service.db.NewUpdate().Model((*credentialRow)(nil)).Set("password_hash = ?", "invalid").Where("principal_id = ?", principalID).Exec(t.Context())
	require.NoError(t, err)
	_, err = service.BeginTOTPEnrollment(t.Context(), "", cookie, "correct horse battery staple")
	assert.Error(t, err)
	assert.Error(t, service.DisableTOTP(t.Context(), "", cookie, "correct horse battery staple", "123456"))

	closed, _ := newLocalService(t)
	ciphertext, err := closed.encryptMFASecret(testID("principal"), "JBSWY3DPEHPK3PXP")
	require.NoError(t, err)
	require.NoError(t, closed.db.Close())
	_, err = closed.consumeFactor(t.Context(), closed.db, &credentialRow{PrincipalID: testID("principal")}, "RECOVERY-CODE", time.Now())
	assert.Error(t, err)
	code := mustTOTP(t, "JBSWY3DPEHPK3PXP", time.Now())
	_, err = closed.consumeFactor(t.Context(), closed.db, &credentialRow{PrincipalID: testID("principal"), TOTPSecret: ciphertext}, code, time.Now())
	assert.Error(t, err)
	_, err = closed.insertSession(t.Context(), closed.db, testID("principal"), time.Now(), authnext.Assurance{AuthTime: time.Now(), Level: 1, Methods: []string{"pwd"}})
	assert.Error(t, err)
	_, valid := validateTOTP("invalid", "bad", time.Unix(0, 0))
	assert.False(t, valid)
}

func TestMFAFailsClosedForMissingAndConcurrentState(t *testing.T) {
	t.Run("missing credential", func(t *testing.T) {
		service, principalID := newLocalService(t)
		sessionID, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
		require.NoError(t, err)
		cookie := service.SessionCookie(sessionID)
		_, err = service.db.NewDelete().Model((*credentialRow)(nil)).Where("principal_id = ?", principalID).Exec(t.Context())
		require.NoError(t, err)
		_, err = service.MFAStatus(t.Context(), "", cookie)
		assert.Error(t, err)
		_, err = service.BeginTOTPEnrollment(t.Context(), "", cookie, "correct horse battery staple")
		assert.Error(t, err)
	})

	t.Run("corrupt enrollment", func(t *testing.T) {
		service, principalID := newLocalService(t)
		now := time.Unix(1_785_600_000, 0).UTC()
		service.now = func() time.Time { return now }
		sessionID, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
		require.NoError(t, err)
		cookie := service.SessionCookie(sessionID)
		_, err = service.BeginTOTPEnrollment(t.Context(), "", cookie, "correct horse battery staple")
		require.NoError(t, err)
		_, err = service.db.NewUpdate().Model((*mfaEnrollmentRow)(nil)).Set("secret_ciphertext = ?", "v1.invalid").Where("principal_id = ?", principalID).Exec(t.Context())
		require.NoError(t, err)
		_, err = service.ConfirmTOTPEnrollment(t.Context(), "", cookie, "123456")
		assert.Error(t, err)
	})

	t.Run("concurrent enable", func(t *testing.T) {
		service, principalID := newLocalService(t)
		now := time.Unix(1_785_600_000, 0).UTC()
		service.now = func() time.Time { return now }
		sessionID, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
		require.NoError(t, err)
		cookie := service.SessionCookie(sessionID)
		enrollment, err := service.BeginTOTPEnrollment(t.Context(), "", cookie, "correct horse battery staple")
		require.NoError(t, err)
		_, err = service.db.NewUpdate().Model((*credentialRow)(nil)).Set("mfa_required = ?", true).Where("principal_id = ?", principalID).Exec(t.Context())
		require.NoError(t, err)
		_, err = service.ConfirmTOTPEnrollment(t.Context(), "", cookie, mustTOTP(t, enrollment.Secret, now))
		assert.ErrorIs(t, err, ErrMFAAlreadyOn)
	})

	t.Run("challenge state mismatch", func(t *testing.T) {
		service, principalID := newLocalService(t)
		now := time.Unix(1_785_600_000, 0).UTC()
		service.now = func() time.Time { return now }
		sessionID, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
		require.NoError(t, err)
		cookie := service.SessionCookie(sessionID)
		enrollment, err := service.BeginTOTPEnrollment(t.Context(), "", cookie, "correct horse battery staple")
		require.NoError(t, err)
		_, err = service.ConfirmTOTPEnrollment(t.Context(), "", cookie, mustTOTP(t, enrollment.Secret, now))
		require.NoError(t, err)
		now = now.Add(30 * time.Second)
		challenge, err := service.BeginLogin(t.Context(), "admin", "correct horse battery staple", "")
		require.NoError(t, err)
		_, err = service.db.NewUpdate().Model((*credentialRow)(nil)).Set("mfa_required = ?", false).Where("principal_id = ?", principalID).Exec(t.Context())
		require.NoError(t, err)
		_, err = service.VerifyLoginMFA(t.Context(), challenge.ChallengeID, mustTOTP(t, enrollment.Secret, now))
		assert.ErrorIs(t, err, ErrMFAChallenge)
	})

	t.Run("challenge storage unavailable", func(t *testing.T) {
		service, principalID := newLocalService(t)
		_, err := service.db.NewUpdate().Model((*credentialRow)(nil)).Set("mfa_required = ?", true).Where("principal_id = ?", principalID).Exec(t.Context())
		require.NoError(t, err)
		_, err = service.db.ExecContext(t.Context(), "DROP TABLE iam_mfa_challenges")
		require.NoError(t, err)
		_, err = service.BeginLogin(t.Context(), "admin", "correct horse battery staple", "")
		assert.Error(t, err)
	})
}

func TestMFAStorageFailuresFailClosed(t *testing.T) {
	t.Run("invalid ciphertext encoding", func(t *testing.T) {
		service, _ := newLocalService(t)
		_, err := service.decryptAuthnData("v1", "principal", "v1.%%%")
		assert.ErrorContains(t, err, "invalid authentication ciphertext")
	})

	t.Run("recovery status storage", func(t *testing.T) {
		service, principalID := newLocalService(t)
		bearer, _, err := service.IssueAccessToken(t.Context(), parseGUID(principalID), "api", "")
		require.NoError(t, err)
		_, err = service.db.ExecContext(t.Context(), "DROP TABLE iam_recovery_codes")
		require.NoError(t, err)
		_, err = service.MFAStatus(t.Context(), "Bearer "+bearer, "")
		assert.ErrorContains(t, err, "count recovery codes")
	})

	t.Run("enrollment cleanup storage", func(t *testing.T) {
		service, _ := newLocalService(t)
		cookie := authenticatedCookie(t, service)
		_, err := service.db.ExecContext(t.Context(), "DROP TABLE iam_mfa_enrollments")
		require.NoError(t, err)
		_, err = service.BeginTOTPEnrollment(t.Context(), "", cookie, "correct horse battery staple")
		assert.ErrorContains(t, err, "store TOTP enrollment")
	})

	t.Run("enrollment lookup storage", func(t *testing.T) {
		service, _ := newLocalService(t)
		cookie := authenticatedCookie(t, service)
		_, err := service.db.ExecContext(t.Context(), "DROP TABLE iam_mfa_enrollments")
		require.NoError(t, err)
		_, err = service.ConfirmTOTPEnrollment(t.Context(), "", cookie, "123456")
		assert.ErrorContains(t, err, "confirm TOTP enrollment")
	})

	t.Run("recovery cleanup storage", func(t *testing.T) {
		service, _ := newLocalService(t)
		now := time.Unix(1_785_600_000, 0).UTC()
		service.now = func() time.Time { return now }
		cookie := authenticatedCookie(t, service)
		enrollment, err := service.BeginTOTPEnrollment(t.Context(), "", cookie, "correct horse battery staple")
		require.NoError(t, err)
		_, err = service.db.ExecContext(t.Context(), "DROP TABLE iam_recovery_codes")
		require.NoError(t, err)
		_, err = service.ConfirmTOTPEnrollment(t.Context(), "", cookie, mustTOTP(t, enrollment.Secret, now))
		assert.ErrorContains(t, err, "confirm TOTP enrollment")
	})

	t.Run("recovery insert storage", func(t *testing.T) {
		service, _ := newLocalService(t)
		now := time.Unix(1_785_600_000, 0).UTC()
		service.now = func() time.Time { return now }
		cookie := authenticatedCookie(t, service)
		enrollment, err := service.BeginTOTPEnrollment(t.Context(), "", cookie, "correct horse battery staple")
		require.NoError(t, err)
		_, err = service.db.ExecContext(t.Context(), `CREATE TRIGGER deny_recovery_insert BEFORE INSERT ON iam_recovery_codes BEGIN SELECT RAISE(ABORT, 'recovery insert denied'); END`)
		require.NoError(t, err)
		_, err = service.ConfirmTOTPEnrollment(t.Context(), "", cookie, mustTOTP(t, enrollment.Secret, now))
		assert.ErrorContains(t, err, "recovery insert denied")
	})

	t.Run("challenge storage", func(t *testing.T) {
		service, principalID := newLocalService(t)
		_, err := service.db.ExecContext(t.Context(), "DROP TABLE iam_mfa_challenges")
		require.NoError(t, err)
		_, err = service.createLoginChallenge(t.Context(), parseGUID(principalID), "https://app.example/", time.Now().UTC())
		assert.ErrorContains(t, err, "create MFA challenge")
	})

	t.Run("disabled principal password check", func(t *testing.T) {
		service, principalID := newLocalService(t)
		_, err := service.db.NewUpdate().Model((*principalRow)(nil)).Set("status = 'disabled'").Where("id = ?", principalID).Exec(t.Context())
		require.NoError(t, err)
		_, _, err = service.verifyCurrentPassword(t.Context(), parseGUID(principalID), "correct horse battery staple")
		assert.ErrorIs(t, err, authnext.ErrInvalidCredentials)
	})
}

func mustTOTP(t *testing.T, secret string, now time.Time) string {
	t.Helper()
	code, err := totp.GenerateCode(secret, now)
	require.NoError(t, err)
	return code
}

func pendingMFAFixture(t *testing.T) (*WebService, string, string, authnext.MFAEnrollment, time.Time) {
	t.Helper()
	service, principalID := newLocalService(t)
	now := time.Unix(1_785_600_000, 0).UTC()
	service.now = func() time.Time { return now }
	cookie := authenticatedCookie(t, service)
	enrollment, err := service.BeginTOTPEnrollment(t.Context(), "", cookie, "correct horse battery staple")
	require.NoError(t, err)
	return service, principalID, cookie, enrollment, now
}

func enabledMFAFixture(t *testing.T) (*WebService, string, string, authnext.MFAEnrollment, []string, time.Time) {
	t.Helper()
	service, principalID, cookie, enrollment, now := pendingMFAFixture(t)
	confirmation, err := service.ConfirmTOTPEnrollment(t.Context(), "", cookie, mustTOTP(t, enrollment.Secret, now))
	require.NoError(t, err)
	return service, principalID, cookie, enrollment, confirmation.RecoveryCodes, now
}

func rejectAuthnAudit(t *testing.T, service *WebService, eventType string) {
	t.Helper()
	statement := fmt.Sprintf(`CREATE TRIGGER deny_authn_audit BEFORE INSERT ON iam_audit_events WHEN NEW.event_type = '%s' BEGIN SELECT RAISE(ABORT, 'audit denied'); END`, eventType)
	_, err := service.db.ExecContext(t.Context(), statement)
	require.NoError(t, err)
}

func assertAuthnAuditCount(t *testing.T, service *WebService, eventType string, expected int) {
	t.Helper()
	count, err := service.db.NewSelect().Table("iam_audit_events").Where("tenant_id = ? AND event_type = ?", authnAuditTenant, eventType).Count(t.Context())
	require.NoError(t, err)
	assert.Equal(t, expected, count)
}

func uniqueStrings(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func differentCode(code string) string {
	if code[0] == '0' {
		return "1" + code[1:]
	}
	return "0" + code[1:]
}
func TestTOTPGoogleGitHubCompatibleAlgorithm(t *testing.T) {
	// RFC 6238 Appendix B test vectors (SHA-1, 30s period) are the exact values
	// Google Authenticator's test suite uses, so passing them proves the same
	// algorithm GitHub and Google ship. Secret is base32 of ASCII
	// "12345678901234567890"; expected values are the official 8-digit vectors
	// truncated to the 6 digits Google/GitHub display.
	rfcSecret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	vectors := []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1_111_111_109, "081804"},
		{1_111_111_111, "050471"},
		{1_234_567_890, "005924"},
		{2_000_000_000, "279037"},
		{20_000_000_000, "353130"},
	}
	for _, v := range vectors {
		at := time.Unix(v.unix, 0).UTC()
		code, err := totp.GenerateCode(rfcSecret, at)
		require.NoError(t, err)
		assert.Equal(t, v.want, code, "production library code at %d", v.unix)
		step, ok := validateTOTP(code, rfcSecret, at)
		assert.True(t, ok, "validator accepts RFC 6238 vector at %d", v.unix)
		assert.Equal(t, v.unix/totpPeriod, step)
	}
	// A code three steps away must not validate inside the one-step window.
	otherStep, err := totp.GenerateCode(rfcSecret, time.Unix(1_111_111_109+90, 0).UTC())
	require.NoError(t, err)
	_, ok := validateTOTP(otherStep, rfcSecret, time.Unix(1_111_111_109, 0).UTC())
	assert.False(t, ok, "code three steps away must be rejected")
}

func TestTOTPProvisioningURIGoogleCompatible(t *testing.T) {
	service, _ := newLocalService(t)
	service.cfg.MFA.Issuer = "Chaosplus Test"
	now := time.Unix(1_785_600_000, 0).UTC()
	service.now = func() time.Time { return now }
	sessionID, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	cookie := service.SessionCookie(sessionID)

	enrollment, err := service.BeginTOTPEnrollment(t.Context(), "", cookie, "correct horse battery staple")
	require.NoError(t, err)

	u, err := url.Parse(enrollment.ProvisioningURI)
	require.NoError(t, err)
	assert.Equal(t, "otpauth", u.Scheme)
	assert.Equal(t, "totp", u.Host)
	assert.Contains(t, u.Path, service.cfg.MFA.Issuer+":admin")
	q := u.Query()
	assert.Equal(t, enrollment.Secret, q.Get("secret"))
	assert.Equal(t, "SHA1", q.Get("algorithm"))
	assert.Equal(t, "6", q.Get("digits"))
	assert.Equal(t, "30", q.Get("period"))
	assert.Equal(t, service.cfg.MFA.Issuer, q.Get("issuer"))
	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(enrollment.Secret)
	require.NoError(t, err)
	assert.Len(t, raw, 20, "TOTP secret must be 160 bits")

	// Google/GitHub authenticator apps consume this otpauth URI and compute
	// codes from the same secret; the enrollment and MFA login must succeed.
	confirmation, err := service.ConfirmTOTPEnrollment(t.Context(), "", cookie, mustTOTP(t, enrollment.Secret, now))
	require.NoError(t, err)
	require.Len(t, confirmation.RecoveryCodes, 10)

	challenge, err := service.BeginLogin(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	result, err := service.VerifyLoginMFA(t.Context(), challenge.ChallengeID, mustTOTP(t, enrollment.Secret, now.Add(30*time.Second)))
	require.NoError(t, err)
	assert.Equal(t, "authenticated", result.Status)
}
