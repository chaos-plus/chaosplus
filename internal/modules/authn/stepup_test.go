package authn

import (
	"testing"
	"time"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStepUpRequiresMFA(t *testing.T) {
	service, _ := newLocalService(t)
	cookie := authenticatedCookie(t, service)
	_, err := service.BeginStepUp(t.Context(), "", cookie)
	assert.ErrorIs(t, err, authnext.ErrMFANotEnabled)
}

func TestStepUpElevatesSession(t *testing.T) {
	service, principalID, cookie, enrollment, _, now := enabledMFAFixture(t)
	now = now.Add(30 * time.Second)
	service.now = func() time.Time { return now }

	options, err := service.BeginStepUp(t.Context(), "", cookie)
	require.NoError(t, err)
	assert.Equal(t, []string{"totp", "recovery_code"}, options.Methods)
	var row stepUpChallengeRow
	require.NoError(t, service.db.NewSelect().Model(&row).Where("id_hash = ?", tokenHash(options.ChallengeID)).Scan(t.Context()))
	assert.Equal(t, principalID, row.PrincipalID)
	assert.Equal(t, currentSessionHash(cookie, service.web.CookieName), row.SessionHash)

	result, err := service.VerifyStepUp(t.Context(), "", cookie, options.ChallengeID, mustTOTP(t, enrollment.Secret, now))
	require.NoError(t, err)
	assert.NotEmpty(t, result.SessionID)
	claims, err := service.Authenticate(t.Context(), "", service.SessionCookie(result.SessionID))
	require.NoError(t, err)
	assert.Equal(t, 2, claims.ACR)
	assert.Equal(t, []string{"pwd", "mfa"}, claims.AMR)
	assert.Equal(t, now, claims.AuthTime)
	_, err = service.Authenticate(t.Context(), "", cookie)
	assert.ErrorIs(t, err, authnext.ErrInvalidSession)
	require.NoError(t, service.db.NewSelect().Model(&row).Where("id_hash = ?", tokenHash(options.ChallengeID)).Scan(t.Context()))
	assert.NotZero(t, row.ConsumedAt)
	assertAuthnAuditCount(t, service, "step_up_completed", 1)
}

func TestStepUpBindsChallengeToSession(t *testing.T) {
	service, _, cookie, enrollment, _, now := enabledMFAFixture(t)
	now = now.Add(30 * time.Second)
	service.now = func() time.Time { return now }
	loginChallenge, err := service.BeginLogin(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	loginResult, err := service.VerifyLoginMFA(t.Context(), loginChallenge.ChallengeID, mustTOTP(t, enrollment.Secret, now))
	require.NoError(t, err)
	otherCookie := service.SessionCookie(loginResult.SessionID)

	options, err := service.BeginStepUp(t.Context(), "", cookie)
	require.NoError(t, err)
	_, err = service.VerifyStepUp(t.Context(), "", otherCookie, options.ChallengeID, mustTOTP(t, enrollment.Secret, now))
	assert.ErrorIs(t, err, authnext.ErrMFAChallenge)
	_, err = service.Authenticate(t.Context(), "", cookie)
	require.NoError(t, err)
	var row stepUpChallengeRow
	require.NoError(t, service.db.NewSelect().Model(&row).Where("id_hash = ?", tokenHash(options.ChallengeID)).Scan(t.Context()))
	assert.Zero(t, row.ConsumedAt)
}

func TestStepUpWrongCodeAndAttemptLimit(t *testing.T) {
	service, _, cookie, enrollment, _, now := enabledMFAFixture(t)
	now = now.Add(30 * time.Second)
	service.now = func() time.Time { return now }
	options, err := service.BeginStepUp(t.Context(), "", cookie)
	require.NoError(t, err)
	invalid := differentCode(mustTOTP(t, enrollment.Secret, now))
	maxAttempts := service.cfg.MFA.MaxAttempts
	for range maxAttempts - 1 {
		_, err = service.VerifyStepUp(t.Context(), "", cookie, options.ChallengeID, invalid)
		assert.ErrorIs(t, err, authnext.ErrInvalidMFA)
	}
	var row stepUpChallengeRow
	require.NoError(t, service.db.NewSelect().Model(&row).Where("id_hash = ?", tokenHash(options.ChallengeID)).Scan(t.Context()))
	assert.Equal(t, maxAttempts-1, row.Attempts)
	assert.Zero(t, row.ConsumedAt)
	_, err = service.VerifyStepUp(t.Context(), "", cookie, options.ChallengeID, invalid)
	assert.ErrorIs(t, err, authnext.ErrInvalidMFA)
	require.NoError(t, service.db.NewSelect().Model(&row).Where("id_hash = ?", tokenHash(options.ChallengeID)).Scan(t.Context()))
	assert.Equal(t, maxAttempts, row.Attempts)
	assert.NotZero(t, row.ConsumedAt)
	_, err = service.VerifyStepUp(t.Context(), "", cookie, options.ChallengeID, mustTOTP(t, enrollment.Secret, now))
	assert.ErrorIs(t, err, authnext.ErrMFAChallenge)
}
