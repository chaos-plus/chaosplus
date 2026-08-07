package authn

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	auditmod "github.com/chaos-plus/chaosplus/internal/modules/audit"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/chaos-plus/chaosplus/internal/modules/identity"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// TestWebServiceSessionAndReturnURLHelpers covers the helpers federation uses
// after an external login completes. All three shipped with zero coverage.
func TestWebServiceSessionAndReturnURLHelpers(t *testing.T) {
	service, principalID := newLocalService(t)

	assert.True(t, service.CookieSecure())
	assert.Equal(t, "https://app.example/login", service.PostLogoutURL())

	// An empty return URL falls back to the configured post-login URL.
	resolved, err := service.ResolveReturnURL("")
	require.NoError(t, err)
	assert.Equal(t, "https://app.example/", resolved)
	resolved, err = service.ResolveReturnURL("  https://app.example/  ")
	require.NoError(t, err)
	assert.Equal(t, "https://app.example/", resolved)
	// Anything outside the allowlist is refused, which is what stops open
	// redirects after a federated login.
	_, err = service.ResolveReturnURL("https://evil.example/steal")
	assert.ErrorIs(t, err, authnext.ErrReturnURL)

	// A session created directly authenticates like any browser session.
	token, err := service.CreateSession(t.Context(), principalID, time.Now().UTC(), authnext.Assurance{AuthTime: time.Now().UTC(), Level: 1, Methods: []string{"pwd"}})
	require.NoError(t, err)
	require.NotEmpty(t, token)
	claims, err := service.Authenticate(t.Context(), "", service.SessionCookie(token))
	require.NoError(t, err)
	assert.Equal(t, principalID, claims.Subject)

	// A disabled service refuses both helpers instead of silently succeeding.
	disabled, err := NewWebService(authnext.Config{}, nil)
	require.NoError(t, err)
	_, err = disabled.CreateSession(t.Context(), principalID, time.Now().UTC(), authnext.Assurance{})
	assert.ErrorIs(t, err, authnext.ErrDisabled)
	_, err = disabled.ResolveReturnURL("https://app.example/")
	assert.ErrorIs(t, err, authnext.ErrDisabled)
}

// TestWebServicePropagatesDatabaseFailures drops the real credential and
// session tables so the driver produces genuine errors. These failure branches
// guard the login path and were otherwise unexercised.
func TestWebServicePropagatesDatabaseFailures(t *testing.T) {
	t.Run("credentials", func(t *testing.T) {
		service, _ := newLocalService(t)
		_, err := service.db.ExecContext(t.Context(), "DROP TABLE iam_credentials")
		require.NoError(t, err)

		_, beginErr := service.BeginLogin(t.Context(), "Admin", "correct horse battery staple", "")
		assert.Error(t, beginErr)
		_, _, loginErr := service.Login(t.Context(), "Admin", "correct horse battery staple", "")
		assert.Error(t, loginErr)
	})

	t.Run("sessions", func(t *testing.T) {
		service, _ := newLocalService(t)
		token, _, err := service.Login(t.Context(), "Admin", "correct horse battery staple", "")
		require.NoError(t, err)
		cookie := service.SessionCookie(token)
		_, dropErr := service.db.ExecContext(t.Context(), "DROP TABLE iam_sessions")
		require.NoError(t, dropErr)

		_, listErr := service.ListSessions(t.Context(), "", cookie)
		assert.Error(t, listErr)
		assert.Error(t, service.RevokeSession(t.Context(), "", cookie, "some-session"))
		assert.Error(t, service.LogoutAll(t.Context(), "", cookie))
		assert.Error(t, service.ChangePassword(t.Context(), "", cookie, "correct horse battery staple", "a different long passphrase"))
		// Logout still returns a clear-cookie header when the store is gone, so
		// a browser is never left holding a session it cannot use.
		assert.NotEmpty(t, service.Logout(t.Context(), cookie))
	})
}

func newLocalService(t *testing.T) (*WebService, string) {
	t.Helper()
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(context.Background(), db))
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	cfg := authnext.Config{
		Enabled: true, Issuer: "https://iam.example", Audience: []string{"api"},
		SigningKey: base64.RawStdEncoding.EncodeToString(seed), AccessTokenTTL: time.Minute,
		MFA:     authnext.MFAConfig{EncryptionKey: base64.RawStdEncoding.EncodeToString(seed)},
		Passkey: authnext.PasskeyConfig{Enabled: true, RPID: "app.example", DisplayName: "Chaosplus", Origins: []string{"https://app.example"}},
		Web:     authnext.WebConfig{Enabled: true, CookieName: "cp_session", SessionTTL: time.Hour, IdleTTL: 10 * time.Minute, PostLoginURL: "https://app.example/", PostLogoutURL: "https://app.example/login", AllowedReturnURLs: []string{"https://app.example/"}, AllowedOrigins: []string{"https://app.example"}, CookieSecure: true},
	}
	service, err := NewWebService(cfg, db)
	require.NoError(t, err)
	principalID, err := EnsureBootstrapPrincipal(context.Background(), db, BootstrapPrincipal{LoginName: "Admin", Password: "correct horse battery staple", DisplayName: "Administrator", Email: "admin@example.com"})
	require.NoError(t, err)
	return service, principalID
}

func TestLocalLoginSessionLogout(t *testing.T) {
	service, principalID := newLocalService(t)
	ctx := context.Background()
	token, returnURL, err := service.Login(ctx, " ADMIN ", "correct horse battery staple", "")
	require.NoError(t, err)
	assert.Equal(t, "https://app.example/", returnURL)
	cookie := service.SessionCookie(token)
	assert.Contains(t, cookie, "HttpOnly")
	assert.Contains(t, cookie, "Secure")
	claims, err := service.Authenticate(ctx, "", cookie)
	require.NoError(t, err)
	assert.Equal(t, principalID, claims.Subject)
	assert.Equal(t, "admin", claims.PreferredUsername)
	assert.Equal(t, "admin@example.com", claims.Email)
	assert.True(t, claims.EmailVerified)
	assert.Equal(t, 1, claims.ACR)
	assert.Equal(t, []string{"pwd"}, claims.AMR)
	assert.False(t, claims.AuthTime.IsZero())
	assert.Equal(t, "https://app.example/login", service.Logout(ctx, cookie))
	_, err = service.Authenticate(ctx, "", cookie)
	assert.ErrorIs(t, err, ErrInvalidSession)
}

func TestLocalAccessToken(t *testing.T) {
	service, principalID := newLocalService(t)
	token, expires, err := service.IssueAccessToken(context.Background(), principalID, "api", "openid profile")
	require.NoError(t, err)
	assert.Equal(t, int64(60), expires)
	claims, err := service.Authenticate(context.Background(), "Bearer "+token, "")
	require.NoError(t, err)
	assert.Equal(t, principalID, claims.Subject)
	assert.True(t, claims.EmailVerified)
	assert.Equal(t, 1, claims.ACR)
	assert.Equal(t, []string{"pwd"}, claims.AMR)
	assert.False(t, claims.AuthTime.IsZero())
	assert.NotEmpty(t, service.JWKS()["keys"])
	_, err = service.Authenticate(context.Background(), "Bearer "+token+"x", "")
	assert.Error(t, err)
}

func TestSessionAndPasswordSecurityCenter(t *testing.T) {
	service, principalID := newLocalService(t)
	first, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	second, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	firstCookie := service.SessionCookie(first)
	secondCookie := service.SessionCookie(second)

	sessions, err := service.ListSessions(t.Context(), "", firstCookie)
	require.NoError(t, err)
	require.Len(t, sessions, 2)
	var other string
	for _, session := range sessions {
		if session.Current {
			assert.Equal(t, tokenHash(first), session.ID)
		} else {
			other = session.ID
		}
	}
	require.NotEmpty(t, other)
	require.NoError(t, service.RevokeSession(t.Context(), "", firstCookie, other))
	_, err = service.Authenticate(t.Context(), "", secondCookie)
	assert.ErrorIs(t, err, ErrInvalidSession)
	assert.ErrorIs(t, service.RevokeSession(t.Context(), "", firstCookie, "invalid"), ErrSessionNotFound)

	assert.ErrorIs(t, service.ChangePassword(t.Context(), "", firstCookie, "wrong", "new correct horse battery staple"), authnext.ErrInvalidCredentials)
	assert.ErrorIs(t, service.ChangePassword(t.Context(), "", firstCookie, "correct horse battery staple", "short"), ErrInvalidPassword)
	require.NoError(t, service.ChangePassword(t.Context(), "", firstCookie, "correct horse battery staple", "new correct horse battery staple"))
	_, err = service.Authenticate(t.Context(), "", firstCookie)
	require.NoError(t, err)
	_, _, err = service.Login(t.Context(), "admin", "correct horse battery staple", "")
	assert.ErrorIs(t, err, authnext.ErrInvalidCredentials)
	third, _, err := service.Login(t.Context(), "admin", "new correct horse battery staple", "")
	require.NoError(t, err)
	assert.ErrorIs(t, service.ChangePassword(t.Context(), "", firstCookie, "new correct horse battery staple", "correct horse battery staple"), ErrPasswordReused)

	require.NoError(t, service.LogoutAll(t.Context(), "", firstCookie))
	_, err = service.Authenticate(t.Context(), "", firstCookie)
	assert.ErrorIs(t, err, ErrInvalidSession)
	_, err = service.Authenticate(t.Context(), "", service.SessionCookie(third))
	assert.ErrorIs(t, err, ErrInvalidSession)

	auditService := auditmod.NewService(service.db)
	for _, eventType := range []string{"session_revoke", "password_change", "logout_all"} {
		events, total, auditErr := auditService.List(t.Context(), auditmod.Filter{TenantID: authnAuditTenant, EventType: eventType, Offset: 0, Limit: 50})
		require.NoError(t, auditErr)
		require.Equal(t, int64(1), total)
		require.Equal(t, principalID, events[0].PrincipalID)
	}
	integrity, err := auditService.Verify(t.Context(), authnAuditTenant)
	require.NoError(t, err)
	assert.True(t, integrity.Valid)
}

func TestPasswordHistoryRetainsPolicyWindow(t *testing.T) {
	service, principalID := newLocalService(t)
	token, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	cookie := service.SessionCookie(token)
	passwords := []string{
		"security password version 01", "security password version 02", "security password version 03",
		"security password version 04", "security password version 05", "security password version 06",
		"security password version 07",
	}
	current := "correct horse battery staple"
	for _, next := range passwords {
		require.NoError(t, service.ChangePassword(t.Context(), "", cookie, current, next))
		current = next
	}
	count, err := service.db.NewSelect().Model((*passwordHistoryRow)(nil)).Where("principal_id = ?", principalID).Count(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 5, count)
	assert.ErrorIs(t, service.ChangePassword(t.Context(), "", cookie, current, passwords[1]), ErrPasswordReused)
	require.NoError(t, service.ChangePassword(t.Context(), "", cookie, current, passwords[0]))
}

func TestDisabledPrincipalInvalidatesBearerToken(t *testing.T) {
	service, principalID := newLocalService(t)
	token, _, err := service.IssueAccessToken(t.Context(), principalID, "api", "openid")
	require.NoError(t, err)
	_, err = service.db.NewUpdate().Table("iam_principals").Set("status = 'disabled'").Where("id = ?", principalID).Exec(t.Context())
	require.NoError(t, err)
	_, err = service.Authenticate(t.Context(), "Bearer "+token, "")
	assert.ErrorIs(t, err, authnext.ErrInvalidToken)

	deletedService, deletedPrincipalID := newLocalService(t)
	deletedToken, _, err := deletedService.IssueAccessToken(t.Context(), deletedPrincipalID, "api", "openid")
	require.NoError(t, err)
	_, err = deletedService.db.NewDelete().Table("iam_principals").Where("id = ?", deletedPrincipalID).Exec(t.Context())
	require.NoError(t, err)
	_, err = deletedService.Authenticate(t.Context(), "Bearer "+deletedToken, "")
	assert.ErrorIs(t, err, authnext.ErrInvalidToken)

	serviceToken, _, err := service.IssueSubjectToken("service-account", "api", "jobs.read", "Worker", "")
	require.NoError(t, err)
	_, err = service.Authenticate(t.Context(), "Bearer "+serviceToken, "")
	assert.NoError(t, err)
}

func TestLocalLoginSecurity(t *testing.T) {
	service, _ := newLocalService(t)
	for range 5 {
		_, _, err := service.Login(context.Background(), "admin", "wrong password", "")
		assert.ErrorIs(t, err, authnext.ErrInvalidCredentials)
	}
	_, _, err := service.Login(context.Background(), "admin", "correct horse battery staple", "")
	assert.ErrorIs(t, err, authnext.ErrInvalidCredentials)
	_, _, err = service.Login(context.Background(), "missing", "wrong password", "")
	assert.ErrorIs(t, err, authnext.ErrInvalidCredentials)
	assert.ErrorIs(t, service.ValidateLoginOrigin("https://evil.example"), ErrCSRF)
	assert.NoError(t, service.ValidateLoginOrigin("https://app.example"))
}

func TestLocalServiceValidation(t *testing.T) {
	_, err := NewWebService(authnext.Config{Enabled: true}, nil)
	assert.Error(t, err)
	service, err := NewWebService(authnext.Config{}, nil)
	require.NoError(t, err)
	assert.False(t, service.Enabled())
}

func TestWebSessionMetadataAndCSRF(t *testing.T) {
	service, _ := newLocalService(t)
	assert.Equal(t, "cp_session", service.SessionCookieName())
	assert.Equal(t, "https://iam.example", service.Issuer())
	assert.Equal(t, "https://app.example/login", service.PostLogoutURL())
	assert.Contains(t, service.ClearCookie(), "Max-Age=0")

	assert.NoError(t, service.ValidateCSRF(http.MethodGet, "", "", ""))
	assert.NoError(t, service.ValidateCSRF(http.MethodPost, "", "", "Bearer token"))
	assert.NoError(t, service.ValidateCSRF(http.MethodPost, "", "", ""))
	assert.ErrorIs(t, service.ValidateCSRF(http.MethodPost, "https://evil.example", "cp_session=value", ""), ErrCSRF)
	assert.NoError(t, service.ValidateCSRF(http.MethodPost, "https://app.example", "cp_session=value", ""))
	assert.Equal(t, "https://app.example/login", service.Logout(t.Context(), "invalid cookie"))
}

func TestSubjectAndIDTokenVariants(t *testing.T) {
	service, principalID := newLocalService(t)
	token, expires, err := service.IssueSubjectToken("service-account", "", "jobs.read", "Worker", "worker@example.com")
	require.NoError(t, err)
	assert.Equal(t, int64(60), expires)
	claims, err := service.Authenticate(t.Context(), "Bearer "+token, "")
	require.NoError(t, err)
	assert.Equal(t, "service-account", claims.Subject)
	assert.Equal(t, authnext.SubjectTypeService, claims.SubjectType)
	assert.Equal(t, []string{"api"}, claims.Audience)

	tenantToken, _, err := service.IssueTenantSubjectToken(t.Context(), "service-account", "tenant", "api", "jobs.write", "Worker", "")
	require.NoError(t, err)
	claims, err = service.Authenticate(t.Context(), "Bearer "+tenantToken, "")
	require.NoError(t, err)
	assert.Equal(t, "tenant", claims.OrganizationID)

	idToken, err := service.IssueIDToken(t.Context(), principalID, "browser", "nonce")
	require.NoError(t, err)
	idTokenParts := strings.Split(idToken, ".")
	require.Len(t, idTokenParts, 3)
	idTokenPayload, err := base64.RawURLEncoding.DecodeString(idTokenParts[1])
	require.NoError(t, err)
	var idClaims map[string]any
	require.NoError(t, json.Unmarshal(idTokenPayload, &idClaims))
	assert.Equal(t, true, idClaims["email_verified"])
	tenantIDToken, err := service.IssueTenantIDToken(t.Context(), principalID, "tenant", "browser", "")
	require.NoError(t, err)
	assert.Len(t, strings.Split(tenantIDToken, "."), 3)
}

func TestOAuthClientBearerSubjectLifecycle(t *testing.T) {
	service, _ := newLocalService(t)
	now := time.Now().UTC().UnixMilli()
	_, err := service.db.ExecContext(t.Context(), `INSERT INTO iam_oauth_clients
 (id, tenant_id, name, status, created_at, updated_at) VALUES (?, ?, ?, 'active', ?, ?)`, "worker", "tenant", "Worker", now, now)
	require.NoError(t, err)

	token, _, err := service.IssueTenantOAuthClientToken(t.Context(), "worker", "tenant", "api", "jobs.read", "Worker")
	require.NoError(t, err)
	claims, err := service.Authenticate(t.Context(), "Bearer "+token, "")
	require.NoError(t, err)
	assert.Equal(t, authnext.SubjectTypeOAuthClient, claims.SubjectType)
	assert.Equal(t, "client:worker", claims.Subject)
	assert.Equal(t, "tenant", claims.OrganizationID)

	_, _, err = service.IssueTenantOAuthClientToken(t.Context(), "worker", "other-tenant", "api", "jobs.read", "Worker")
	assert.ErrorIs(t, err, authnext.ErrInvalidToken)
	_, err = service.db.NewUpdate().Table("iam_oauth_clients").Set("status = 'disabled'").Where("id = 'worker'").Exec(t.Context())
	require.NoError(t, err)
	_, err = service.Authenticate(t.Context(), "Bearer "+token, "")
	assert.ErrorIs(t, err, authnext.ErrInvalidToken)
	_, err = service.db.NewDelete().Table("iam_oauth_clients").Where("id = 'worker'").Exec(t.Context())
	require.NoError(t, err)
	_, err = service.Authenticate(t.Context(), "Bearer "+token, "")
	assert.ErrorIs(t, err, authnext.ErrInvalidToken)

	for _, subject := range []string{"worker", "client:"} {
		malformed, _, issueErr := service.issueSubjectToken(t.Context(), authnext.SubjectTypeOAuthClient, subject, "tenant", "api", "", 0, "", "", false, authnext.Assurance{})
		require.NoError(t, issueErr)
		_, authenticateErr := service.Authenticate(t.Context(), "Bearer "+malformed, "")
		assert.ErrorIs(t, authenticateErr, authnext.ErrInvalidToken)
	}
	assert.ErrorIs(t, service.verifySubjectState(t.Context(), &authnext.Claims{SubjectType: "unknown"}), authnext.ErrInvalidToken)
	_, _, err = service.IssueSubjectToken("", "api", "", "", "")
	assert.ErrorIs(t, err, authnext.ErrInvalidToken)
}

func TestServiceAccountBearerSubjectLifecycle(t *testing.T) {
	service, _ := newLocalService(t)
	require.NoError(t, organization.Migrate(t.Context(), service.db))
	require.NoError(t, organization.EnsureTenant(t.Context(), service.db, "tenant"))
	identities := identity.NewService(service.db, authnAuditAppender(auditmod.NewService(service.db)), iam.NewAdministratorGuard())
	account, err := identities.CreateServiceAccount(t.Context(), "tenant", "automation", "Automation", "", nil)
	require.NoError(t, err)

	token, _, err := service.IssueTenantServiceAccountToken(t.Context(), account.ID, "tenant", "api", "jobs.read", account.LoginName, 1)
	require.NoError(t, err)
	claims, err := service.Authenticate(t.Context(), "Bearer "+token, "")
	require.NoError(t, err)
	assert.Equal(t, authnext.SubjectTypeServiceAccount, claims.SubjectType)
	assert.Equal(t, account.ID, claims.Subject)

	wrongTenant, _, err := service.IssueTenantServiceAccountToken(t.Context(), account.ID, "other", "api", "jobs.read", account.LoginName, 1)
	require.NoError(t, err)
	_, err = service.Authenticate(t.Context(), "Bearer "+wrongTenant, "")
	assert.ErrorIs(t, err, authnext.ErrInvalidToken)
	_, err = identities.ReplaceServiceAccount(t.Context(), "tenant", account.ID, account.DisplayName, "", "disabled", nil, account.Version)
	require.NoError(t, err)
	_, err = service.Authenticate(t.Context(), "Bearer "+token, "")
	assert.ErrorIs(t, err, authnext.ErrInvalidToken)
}

func TestOAuthClientBearerSubjectStorageFailure(t *testing.T) {
	service, _ := newLocalService(t)
	now := time.Now().UTC().UnixMilli()
	_, err := service.db.ExecContext(t.Context(), `INSERT INTO iam_oauth_clients
 (id, tenant_id, name, status, created_at, updated_at) VALUES (?, ?, ?, 'active', ?, ?)`, "worker", "tenant", "Worker", now, now)
	require.NoError(t, err)
	token, _, err := service.IssueTenantOAuthClientToken(t.Context(), "worker", "tenant", "api", "jobs.read", "Worker")
	require.NoError(t, err)
	_, err = service.db.ExecContext(t.Context(), "DROP TABLE iam_oauth_clients")
	require.NoError(t, err)

	_, err = service.Authenticate(t.Context(), "Bearer "+token, "")
	assert.ErrorIs(t, err, authnext.ErrUnavailable)
	_, _, err = service.IssueTenantOAuthClientToken(t.Context(), "worker", "tenant", "api", "jobs.read", "Worker")
	assert.ErrorIs(t, err, authnext.ErrUnavailable)
}

func TestTokenAndSessionExpiration(t *testing.T) {
	service, principalID := newLocalService(t)
	issuedAt := time.Now().UTC()
	service.now = func() time.Time { return issuedAt }
	token, _, err := service.IssueAccessToken(t.Context(), principalID, "api", "openid")
	require.NoError(t, err)
	service.now = func() time.Time { return issuedAt.Add(2 * time.Minute) }
	_, err = service.Authenticate(t.Context(), "Bearer "+token, "")
	assert.ErrorIs(t, err, authnext.ErrInvalidToken)

	service.now = func() time.Time { return issuedAt }
	session, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "https://app.example/")
	require.NoError(t, err)
	service.now = func() time.Time { return issuedAt.Add(2 * time.Hour) }
	_, err = service.Authenticate(t.Context(), "", service.SessionCookie(session))
	assert.ErrorIs(t, err, ErrInvalidSession)
}

func TestPrincipalSecurityStatesAndReconciliation(t *testing.T) {
	service, principalID := newLocalService(t)
	_, err := service.db.NewUpdate().Table("iam_credentials").Set("mfa_required = ?", true).Where("principal_id = ?", principalID).Exec(t.Context())
	require.NoError(t, err)
	_, _, err = service.Login(t.Context(), "admin", "correct horse battery staple", "")
	assert.ErrorIs(t, err, authnext.ErrAdditionalVerification)
	_, err = service.db.NewUpdate().Table("iam_credentials").Set("mfa_required = ?", false).Where("principal_id = ?", principalID).Exec(t.Context())
	require.NoError(t, err)
	oldSession, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	oldCookie := service.SessionCookie(oldSession)
	oldAccess, _, err := service.IssueAccessToken(t.Context(), principalID, "api", "openid")
	require.NoError(t, err)
	_, err = service.db.ExecContext(t.Context(), `INSERT INTO iam_password_recovery_tokens (token_hmac, principal_id, created_at, expires_at, consumed_at) VALUES (?, ?, 1, 9999999999999, 0)`, strings.Repeat("a", 64), principalID)
	require.NoError(t, err)
	_, err = service.db.ExecContext(t.Context(), `INSERT INTO iam_email_verification_tokens (token_hmac, principal_id, email, created_at, expires_at, consumed_at) VALUES (?, ?, 'admin@example.com', 1, 9999999999999, 0)`, strings.Repeat("b", 64), principalID)
	require.NoError(t, err)
	_, err = service.db.NewUpdate().Table("iam_principals").Set("status = ?", "disabled").Where("id = ?", principalID).Exec(t.Context())
	require.NoError(t, err)
	_, _, err = service.Login(t.Context(), "admin", "correct horse battery staple", "")
	assert.ErrorIs(t, err, authnext.ErrInvalidCredentials)

	reconciled, err := EnsureBootstrapPrincipal(t.Context(), service.db, BootstrapPrincipal{
		LoginName: "admin", Password: "new correct horse battery staple", DisplayName: "Updated", Email: "UPDATED@example.com",
	})
	require.NoError(t, err)
	assert.Equal(t, principalID, reconciled)
	var reconciledPrincipal principalRow
	require.NoError(t, service.db.NewSelect().Model(&reconciledPrincipal).Where("id = ?", principalID).Scan(t.Context()))
	assert.Equal(t, "updated@example.com", reconciledPrincipal.Email)
	assert.True(t, reconciledPrincipal.EmailVerified)
	_, err = service.Authenticate(t.Context(), "", oldCookie)
	assert.ErrorIs(t, err, ErrInvalidSession)
	_, err = service.Authenticate(t.Context(), "Bearer "+oldAccess, "")
	assert.ErrorIs(t, err, authnext.ErrInvalidToken)
	var recoveryConsumed, verificationConsumed, credentialVersion int64
	require.NoError(t, service.db.NewSelect().Table("iam_password_recovery_tokens").Column("consumed_at").Where("token_hmac = ?", strings.Repeat("a", 64)).Scan(t.Context(), &recoveryConsumed))
	require.NoError(t, service.db.NewSelect().Table("iam_email_verification_tokens").Column("consumed_at").Where("token_hmac = ?", strings.Repeat("b", 64)).Scan(t.Context(), &verificationConsumed))
	require.NoError(t, service.db.NewSelect().Table("iam_credentials").Column("credential_version").Where("principal_id = ?", principalID).Scan(t.Context(), &credentialVersion))
	assert.Positive(t, recoveryConsumed)
	assert.Positive(t, verificationConsumed)
	_, _, err = service.Login(t.Context(), "admin", "new correct horse battery staple", "")
	require.NoError(t, err)
	_, err = EnsureBootstrapPrincipal(t.Context(), service.db, BootstrapPrincipal{
		LoginName: "admin", Password: "new correct horse battery staple", DisplayName: "Updated", Email: "updated@example.com",
	})
	require.NoError(t, err)
	var unchangedCredentialVersion int64
	require.NoError(t, service.db.NewSelect().Table("iam_credentials").Column("credential_version").Where("principal_id = ?", principalID).Scan(t.Context(), &unchangedCredentialVersion))
	assert.Equal(t, credentialVersion, unchangedCredentialVersion)
	assert.Error(t, func() error {
		_, err := EnsureBootstrapPrincipal(t.Context(), nil, BootstrapPrincipal{})
		return err
	}())
	_, err = EnsureBootstrapPrincipal(t.Context(), service.db, BootstrapPrincipal{LoginName: "", Password: "short"})
	assert.Error(t, err)
}

func TestSigningKeyFormatsAndDefaults(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	for index := range seed {
		seed[index] = byte(index + 1)
	}
	for name, encoded := range map[string]string{
		"raw standard": base64.RawStdEncoding.EncodeToString(seed),
		"standard":     base64.StdEncoding.EncodeToString(seed),
		"raw url":      base64.RawURLEncoding.EncodeToString(seed),
		"url":          base64.URLEncoding.EncodeToString(seed),
		"raw bytes":    strings.Repeat("!", ed25519.SeedSize),
	} {
		t.Run(name, func(t *testing.T) {
			key, err := parseSigningKey(encoded)
			require.NoError(t, err)
			assert.Len(t, key, ed25519.PrivateKeySize)
		})
	}
	private := ed25519.NewKeyFromSeed(seed)
	key, err := parseSigningKey(base64.RawStdEncoding.EncodeToString(private))
	require.NoError(t, err)
	assert.Equal(t, private, key)
	_, err = parseSigningKey("")
	assert.Error(t, err)
	_, err = parseSigningKey("too-short")
	assert.Error(t, err)
}

func TestWebServiceDefaultsAndValidation(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	seed := make([]byte, ed25519.SeedSize)
	encoded := base64.RawStdEncoding.EncodeToString(seed)
	_, err = NewWebService(authnext.Config{Enabled: true, SigningKey: encoded}, db)
	assert.ErrorContains(t, err, "issuer is required")
	_, err = NewWebService(authnext.Config{Enabled: true, Issuer: "https://iam.example"}, db)
	assert.ErrorContains(t, err, "signing key is required")

	service, err := NewWebService(authnext.Config{
		Enabled: true, Issuer: " https://iam.example/ ", SigningKey: encoded,
		MFA: authnext.MFAConfig{EncryptionKey: encoded},
		Web: authnext.WebConfig{Enabled: true, SessionTTL: time.Minute, IdleTTL: time.Hour},
	}, db, nil)
	require.NoError(t, err)
	assert.Equal(t, "https://iam.example", service.Issuer())
	assert.Equal(t, []string{"chaosplus-api"}, service.cfg.Audience)
	assert.Equal(t, 15*time.Minute, service.cfg.AccessTokenTTL)
	assert.Equal(t, 30*time.Second, service.cfg.ClockSkew)
	assert.Equal(t, "cp_session", service.SessionCookieName())
	assert.Equal(t, time.Minute, service.web.IdleTTL)
}

func TestWebServiceRejectsUnsafeSecurityConfiguration(t *testing.T) {
	service, _ := newLocalService(t)

	tests := []struct {
		name   string
		mutate func(*authnext.Config)
		match  string
	}{
		{
			name: "MFA limits",
			mutate: func(cfg *authnext.Config) {
				cfg.MFA.RecoveryCodes = 21
			},
			match: "MFA configuration exceeds security limits",
		},
		{
			name: "MFA secret sources",
			mutate: func(cfg *authnext.Config) {
				cfg.MFA.EncryptionKeyFile = "not-used-when-inline-is-set"
			},
			match: "mutually exclusive",
		},
		{
			name: "MFA key",
			mutate: func(cfg *authnext.Config) {
				cfg.MFA.EncryptionKey = "invalid"
			},
			match: "32 bytes",
		},
		{
			name: "Passkey adapter",
			mutate: func(cfg *authnext.Config) {
				cfg.Passkey.RPID = "%%&&"
			},
			match: "configure passkeys",
		},
		{
			name: "signing key source",
			mutate: func(cfg *authnext.Config) {
				cfg.Web.Enabled = false
				cfg.Passkey.Enabled = false
				cfg.SigningKeyFile = "not-used-when-inline-is-set"
			},
			match: "mutually exclusive",
		},
		{
			name: "signing key",
			mutate: func(cfg *authnext.Config) {
				cfg.Web.Enabled = false
				cfg.Passkey.Enabled = false
				cfg.SigningKey = "invalid"
			},
			match: "signing key",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := service.cfg
			test.mutate(&cfg)
			_, err := NewWebService(cfg, service.db)
			assert.ErrorContains(t, err, test.match)
		})
	}
}

func TestAccessTokenMalformedClaimsAndHeaders(t *testing.T) {
	service, _ := newLocalService(t)
	for _, header := range []string{"Basic value", "Bearer malformed", "Bearer !!!.body.signature"} {
		_, err := service.Authenticate(t.Context(), header, "")
		assert.Error(t, err, header)
	}

	wrongIssuer, err := service.sign(map[string]any{
		"iss": "https://other.example", "sub": "subject", "aud": []string{"api"},
		"subject_type": authnext.SubjectTypeService,
		"iat":          time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix(),
	})
	require.NoError(t, err)
	_, err = service.Authenticate(t.Context(), "Bearer "+wrongIssuer, "")
	assert.ErrorIs(t, err, authnext.ErrInvalidToken)
	wrongAudience, err := service.sign(map[string]any{
		"iss": service.Issuer(), "sub": "subject", "aud": []string{"other"},
		"subject_type": authnext.SubjectTypeService,
		"iat":          time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix(),
	})
	require.NoError(t, err)
	_, err = service.Authenticate(t.Context(), "Bearer "+wrongAudience, "")
	assert.ErrorIs(t, err, authnext.ErrInvalidAud)
	emptySubject, err := service.sign(map[string]any{
		"iss": service.Issuer(), "sub": "", "aud": []string{"api"},
		"subject_type": authnext.SubjectTypeService,
		"iat":          time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix(),
	})
	require.NoError(t, err)
	_, err = service.Authenticate(t.Context(), "Bearer "+emptySubject, "")
	assert.ErrorIs(t, err, authnext.ErrInvalidToken)
	for _, subjectType := range []string{"", "unknown"} {
		token, signErr := service.sign(map[string]any{
			"iss": service.Issuer(), "sub": "subject", "aud": []string{"api"},
			"subject_type": subjectType,
			"iat":          time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix(),
		})
		require.NoError(t, signErr)
		_, authenticateErr := service.Authenticate(t.Context(), "Bearer "+token, "")
		assert.ErrorIs(t, authenticateErr, authnext.ErrInvalidToken)
	}
}

func TestAuthenticationDatabaseUnavailable(t *testing.T) {
	service, principalID := newLocalService(t)
	session, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	principalToken, _, err := service.IssueAccessToken(t.Context(), principalID, "api", "")
	require.NoError(t, err)
	require.NoError(t, service.db.Close())
	_, err = service.Authenticate(t.Context(), "", service.SessionCookie(session))
	assert.ErrorIs(t, err, authnext.ErrUnavailable)
	_, err = service.Authenticate(t.Context(), "Bearer "+principalToken, "")
	assert.ErrorIs(t, err, authnext.ErrUnavailable)
	_, _, err = service.IssueTenantAccessToken(t.Context(), "missing", "tenant", "api", "")
	assert.ErrorIs(t, err, authnext.ErrUnavailable)
}

func TestDisabledServiceAndLoginValidation(t *testing.T) {
	service, err := NewWebService(authnext.Config{}, nil, WithClaimEnricher(nil))
	require.NoError(t, err)
	_, err = service.Authenticate(t.Context(), "", "")
	assert.ErrorIs(t, err, authnext.ErrMissingBearer)
	_, _, err = service.Login(t.Context(), "user", "password", "")
	assert.ErrorIs(t, err, authnext.ErrDisabled)

	service, _ = newLocalService(t)
	_, _, err = service.Login(t.Context(), "admin", "correct horse battery staple", "https://evil.example/")
	assert.ErrorContains(t, err, "return URL is not allowed")
	_, _, err = service.IssueAccessToken(t.Context(), "missing", "api", "")
	assert.ErrorIs(t, err, ErrInvalidSession)
	_, err = service.IssueIDToken(t.Context(), "missing", "client", "")
	assert.ErrorIs(t, err, ErrInvalidSession)
}

func TestSessionAbsoluteExpiryCapsIdleRefresh(t *testing.T) {
	service, _ := newLocalService(t)
	started := time.Now().UTC().Truncate(time.Second)
	service.web.SessionTTL = time.Minute
	service.web.IdleTTL = time.Minute
	service.now = func() time.Time { return started }
	token, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	service.now = func() time.Time { return started.Add(time.Second) }
	_, err = service.Authenticate(t.Context(), "", service.SessionCookie(token))
	require.NoError(t, err)

	var expiresAt, absoluteExpiresAt int64
	err = service.db.NewSelect().Table("iam_sessions").Column("expires_at", "absolute_expires_at").Where("id_hash = ?", tokenHash(token)).Scan(t.Context(), &expiresAt, &absoluteExpiresAt)
	require.NoError(t, err)
	assert.Equal(t, absoluteExpiresAt, expiresAt)
}

func TestLoginDatabaseConstraintFailures(t *testing.T) {
	t.Run("credential update", func(t *testing.T) {
		service, _ := newLocalService(t)
		_, err := service.db.ExecContext(t.Context(), `CREATE TRIGGER deny_credential_update BEFORE UPDATE ON iam_credentials BEGIN SELECT RAISE(ABORT, 'denied'); END`)
		require.NoError(t, err)
		_, _, err = service.Login(t.Context(), "admin", "correct horse battery staple", "")
		assert.ErrorContains(t, err, "reset login failures")
	})

	t.Run("session insert", func(t *testing.T) {
		service, _ := newLocalService(t)
		_, err := service.db.ExecContext(t.Context(), `CREATE TRIGGER deny_session_insert BEFORE INSERT ON iam_sessions BEGIN SELECT RAISE(ABORT, 'denied'); END`)
		require.NoError(t, err)
		_, _, err = service.Login(t.Context(), "admin", "correct horse battery staple", "")
		assert.ErrorContains(t, err, "create session")
	})

	t.Run("missing credential", func(t *testing.T) {
		service, principalID := newLocalService(t)
		_, err := service.db.NewDelete().Table("iam_credentials").Where("principal_id = ?", principalID).Exec(t.Context())
		require.NoError(t, err)
		_, _, err = service.Login(t.Context(), "admin", "correct horse battery staple", "")
		assert.ErrorContains(t, err, "read credential")
	})
}

func TestSecurityCenterDatabaseFailures(t *testing.T) {
	t.Run("revoke session", func(t *testing.T) {
		service, _ := newLocalService(t)
		token, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
		require.NoError(t, err)
		_, err = service.db.ExecContext(t.Context(), `CREATE TRIGGER deny_session_revoke BEFORE UPDATE OF revoked_at ON iam_sessions BEGIN SELECT RAISE(ABORT, 'revoke denied'); END`)
		require.NoError(t, err)
		err = service.RevokeSession(t.Context(), "", service.SessionCookie(token), tokenHash(token))
		assert.ErrorContains(t, err, "revoke denied")
	})

	t.Run("logout all", func(t *testing.T) {
		service, _ := newLocalService(t)
		token, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
		require.NoError(t, err)
		_, err = service.db.ExecContext(t.Context(), `CREATE TRIGGER deny_logout_all BEFORE UPDATE OF revoked_at ON iam_sessions BEGIN SELECT RAISE(ABORT, 'logout denied'); END`)
		require.NoError(t, err)
		err = service.LogoutAll(t.Context(), "", service.SessionCookie(token))
		assert.ErrorContains(t, err, "logout denied")
	})

	t.Run("password history", func(t *testing.T) {
		service, _ := newLocalService(t)
		token, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
		require.NoError(t, err)
		_, err = service.db.ExecContext(t.Context(), `CREATE TRIGGER deny_password_history BEFORE INSERT ON iam_password_history BEGIN SELECT RAISE(ABORT, 'history denied'); END`)
		require.NoError(t, err)
		err = service.ChangePassword(t.Context(), "", service.SessionCookie(token), "correct horse battery staple", "new correct horse battery staple")
		assert.ErrorContains(t, err, "history denied")
	})

	t.Run("audit failure rolls back session revoke", func(t *testing.T) {
		service, _ := newLocalService(t)
		first, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
		require.NoError(t, err)
		second, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
		require.NoError(t, err)
		_, err = service.db.ExecContext(t.Context(), `CREATE TRIGGER deny_session_audit BEFORE INSERT ON iam_audit_events WHEN NEW.event_type = 'session_revoke' BEGIN SELECT RAISE(ABORT, 'audit denied'); END`)
		require.NoError(t, err)
		err = service.RevokeSession(t.Context(), "", service.SessionCookie(first), tokenHash(second))
		assert.ErrorContains(t, err, "audit denied")
		_, err = service.Authenticate(t.Context(), "", service.SessionCookie(second))
		require.NoError(t, err)
	})

	t.Run("audit failure rolls back logout all", func(t *testing.T) {
		service, _ := newLocalService(t)
		token, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
		require.NoError(t, err)
		_, err = service.db.ExecContext(t.Context(), `CREATE TRIGGER deny_logout_audit BEFORE INSERT ON iam_audit_events WHEN NEW.event_type = 'logout_all' BEGIN SELECT RAISE(ABORT, 'audit denied'); END`)
		require.NoError(t, err)
		err = service.LogoutAll(t.Context(), "", service.SessionCookie(token))
		assert.ErrorContains(t, err, "audit denied")
		_, err = service.Authenticate(t.Context(), "", service.SessionCookie(token))
		require.NoError(t, err)
	})

	t.Run("audit failure rolls back password change", func(t *testing.T) {
		service, _ := newLocalService(t)
		token, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
		require.NoError(t, err)
		_, err = service.db.ExecContext(t.Context(), `CREATE TRIGGER deny_password_audit BEFORE INSERT ON iam_audit_events WHEN NEW.event_type = 'password_change' BEGIN SELECT RAISE(ABORT, 'audit denied'); END`)
		require.NoError(t, err)
		err = service.ChangePassword(t.Context(), "", service.SessionCookie(token), "correct horse battery staple", "new correct horse battery staple")
		assert.ErrorContains(t, err, "audit denied")
		_, _, err = service.Login(t.Context(), "admin", "correct horse battery staple", "")
		require.NoError(t, err)
		_, _, err = service.Login(t.Context(), "admin", "new correct horse battery staple", "")
		assert.ErrorIs(t, err, authnext.ErrInvalidCredentials)
	})
}

func TestSecurityCenterAuthenticationAndReadFailures(t *testing.T) {
	service, _ := newLocalService(t)
	_, err := service.ListSessions(t.Context(), "", "")
	assert.Error(t, err)
	assert.Error(t, service.RevokeSession(t.Context(), "", "", strings.Repeat("a", 64)))
	assert.Error(t, service.LogoutAll(t.Context(), "", ""))
	assert.Error(t, service.ChangePassword(t.Context(), "", "", "current password", "new secure password"))

	token, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	cookie := service.SessionCookie(token)
	assert.ErrorIs(t, service.RevokeSession(t.Context(), "", cookie, strings.Repeat("a", 64)), ErrSessionNotFound)

	t.Run("session list storage", func(t *testing.T) {
		storage, principalID := newLocalService(t)
		bearer, _, issueErr := storage.IssueAccessToken(t.Context(), principalID, "api", "")
		require.NoError(t, issueErr)
		_, dropErr := storage.db.ExecContext(t.Context(), "DROP TABLE iam_sessions")
		require.NoError(t, dropErr)
		_, listErr := storage.ListSessions(t.Context(), "Bearer "+bearer, "")
		assert.ErrorContains(t, listErr, "list browser sessions")
	})

	t.Run("missing credential", func(t *testing.T) {
		storage, principalID := newLocalService(t)
		bearer, _, issueErr := storage.IssueAccessToken(t.Context(), principalID, "api", "")
		require.NoError(t, issueErr)
		_, deleteErr := storage.db.NewDelete().Model((*credentialRow)(nil)).Where("principal_id = ?", principalID).Exec(t.Context())
		require.NoError(t, deleteErr)
		changeErr := storage.ChangePassword(t.Context(), "Bearer "+bearer, "", "current password", "new secure password")
		assert.ErrorIs(t, changeErr, authnext.ErrInvalidToken)
	})

	t.Run("corrupt credential", func(t *testing.T) {
		storage, principalID := newLocalService(t)
		bearer, _, issueErr := storage.IssueAccessToken(t.Context(), principalID, "api", "")
		require.NoError(t, issueErr)
		_, updateErr := storage.db.NewUpdate().Model((*credentialRow)(nil)).Set("password_hash = 'invalid'").Where("principal_id = ?", principalID).Exec(t.Context())
		require.NoError(t, updateErr)
		changeErr := storage.ChangePassword(t.Context(), "Bearer "+bearer, "", "current password", "new secure password")
		assert.ErrorContains(t, changeErr, "verify current password")
	})

	t.Run("password history storage", func(t *testing.T) {
		storage, principalID := newLocalService(t)
		bearer, _, issueErr := storage.IssueAccessToken(t.Context(), principalID, "api", "")
		require.NoError(t, issueErr)
		_, dropErr := storage.db.ExecContext(t.Context(), "DROP TABLE iam_password_history")
		require.NoError(t, dropErr)
		changeErr := storage.ChangePassword(t.Context(), "Bearer "+bearer, "", "correct horse battery staple", "new secure password")
		assert.ErrorContains(t, changeErr, "load password history")
	})
}

func TestSigningRejectsUnsupportedPayloadAndMalformedBody(t *testing.T) {
	service, _ := newLocalService(t)
	_, err := service.sign(map[string]any{"unsupported": make(chan struct{})})
	assert.Error(t, err)

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"EdDSA","kid":"` + service.kid + `"}`))
	signed := header + ".!"
	signature := ed25519.Sign(service.privateKey, []byte(signed))
	_, err = service.Authenticate(t.Context(), "Bearer "+signed+"."+base64.RawURLEncoding.EncodeToString(signature), "")
	assert.ErrorIs(t, err, authnext.ErrInvalidToken)
}

func authnAuditAppender(service *auditmod.Service) auditx.Appender {
	return func(ctx context.Context, db bun.IDB, event auditx.Event) error {
		_, err := service.AppendTo(ctx, db, auditmod.EventInput{
			TenantID: event.TenantID, PrincipalID: event.PrincipalID,
			EventType: event.EventType, TargetType: event.TargetType, TargetID: event.TargetID,
			Outcome: "success", Detail: event.Detail,
		})
		return err
	}
}
