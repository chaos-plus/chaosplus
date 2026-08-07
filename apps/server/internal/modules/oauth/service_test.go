package oauth

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	auditmod "github.com/chaos-plus/chaosplus/internal/modules/audit"
	authnmod "github.com/chaos-plus/chaosplus/internal/modules/authn"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/chaos-plus/chaosplus/internal/modules/identity"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestAuthorizationCodePKCEAndRefreshRotation(t *testing.T) {
	service, auth, principalID := newOAuthTestService(t)
	db := service.db
	now := time.Now().UTC().UnixMilli()
	client := clientRow{ID: "web", TenantID: "tenant", Name: "Web", RedirectURIs: `["https://client.example/callback"]`, GrantTypes: "authorization_code,refresh_token", Scopes: "openid profile email", PublicClient: true, Status: "active", CreatedAt: now, UpdatedAt: now}
	_, err := db.NewInsert().Model(&client).Exec(context.Background())
	require.NoError(t, err)
	session, _, err := auth.Login(context.Background(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	redirect, err := service.Authorize(context.Background(), auth.SessionCookie(session), "web", "https://client.example/callback", "code", "openid profile", "state", challenge, "S256", "nonce", "")
	require.NoError(t, err)
	parsed, err := url.Parse(redirect)
	require.NoError(t, err)
	code := parsed.Query().Get("code")
	response, err := service.Token(context.Background(), url.Values{"grant_type": {"authorization_code"}, "client_id": {"web"}, "code": {code}, "redirect_uri": {"https://client.example/callback"}, "code_verifier": {verifier}}, "")
	require.NoError(t, err)
	assert.NotEmpty(t, response.AccessToken)
	assert.NotEmpty(t, response.RefreshToken)
	assert.NotEmpty(t, response.IDToken)
	claims, err := auth.Authenticate(context.Background(), "Bearer "+response.AccessToken, "")
	require.NoError(t, err)
	assert.Equal(t, principalID, claims.Subject)
	assert.Equal(t, "tenant", claims.OrganizationID)
	assert.Equal(t, "web", claims.ClientID)
	assert.Equal(t, 1, claims.ACR)
	assert.Equal(t, []string{"pwd"}, claims.AMR)
	assert.False(t, claims.AuthTime.IsZero())
	rotated, err := service.Token(context.Background(), url.Values{"grant_type": {"refresh_token"}, "client_id": {"web"}, "refresh_token": {response.RefreshToken}}, "")
	require.NoError(t, err)
	assert.NotEqual(t, response.RefreshToken, rotated.RefreshToken)
	rotatedClaims, err := auth.Authenticate(context.Background(), "Bearer "+rotated.AccessToken, "")
	require.NoError(t, err)
	assert.Equal(t, claims.ClientID, rotatedClaims.ClientID)
	assert.Equal(t, claims.ACR, rotatedClaims.ACR)
	assert.Equal(t, claims.AMR, rotatedClaims.AMR)
	assert.Equal(t, claims.AuthTime, rotatedClaims.AuthTime)
	_, err = db.ExecContext(context.Background(), "UPDATE iam_tenant_members SET status = 'disabled' WHERE tenant_id = ? AND user_subject = ?", "tenant", principalID)
	require.NoError(t, err)
	_, err = service.Token(context.Background(), url.Values{"grant_type": {"refresh_token"}, "client_id": {"web"}, "refresh_token": {rotated.RefreshToken}}, "")
	assert.ErrorIs(t, err, ErrInvalidRequest)
	_, err = db.ExecContext(context.Background(), "UPDATE iam_tenant_members SET status = 'active' WHERE tenant_id = ? AND user_subject = ?", "tenant", principalID)
	require.NoError(t, err)
	_, err = service.Token(context.Background(), url.Values{"grant_type": {"refresh_token"}, "client_id": {"web"}, "refresh_token": {response.RefreshToken}}, "")
	assert.ErrorIs(t, err, ErrInvalidRequest)
}

func TestClientManagementTenantIsolation(t *testing.T) {
	service, _, _ := newOAuthTestService(t)
	client, secret, err := service.CreateClient(context.Background(), "tenant-a", "Worker", nil, []string{"client_credentials"}, []string{"jobs.read"}, false)
	require.NoError(t, err)
	assert.NotEmpty(t, secret)

	other, err := service.ListClients(context.Background(), "tenant-b")
	require.NoError(t, err)
	assert.Empty(t, other)
	_, err = service.UpdateClient(context.Background(), "tenant-b", client.ID, "Changed", nil, []string{"client_credentials"}, []string{"jobs.read"}, false, "active")
	assert.ErrorIs(t, err, ErrInvalidRequest)
	_, err = service.RotateClientSecret(context.Background(), "tenant-b", client.ID)
	assert.ErrorIs(t, err, ErrInvalidRequest)
	assert.ErrorIs(t, service.DeleteClient(context.Background(), "tenant-b", client.ID), ErrInvalidRequest)

	owned, err := service.ListClients(context.Background(), "tenant-a")
	require.NoError(t, err)
	require.Len(t, owned, 1)
	assert.Equal(t, client.ID, owned[0].ID)
	events, total, err := service.audit.List(t.Context(), auditmod.Filter{TenantID: "tenant-a", Limit: 50})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Equal(t, "oauth_client_created", events[0].EventType)
	require.Equal(t, int64(1), events[0].Sequence)
}

func TestIntrospectionRequiresConfidentialClient(t *testing.T) {
	service, auth, _ := newOAuthTestService(t)
	public, _, err := service.CreateClient(context.Background(), "tenant", "Browser", []string{"https://app.example/callback"}, []string{"authorization_code"}, []string{"openid"}, true)
	require.NoError(t, err)
	confidential, secret, err := service.CreateClient(context.Background(), "tenant", "Resource Server", nil, []string{"client_credentials"}, []string{"introspect"}, false)
	require.NoError(t, err)
	token, _, err := auth.IssueSubjectToken("principal", "api", "openid", "principal", "")
	require.NoError(t, err)

	_, err = service.Introspect(context.Background(), url.Values{"client_id": {public.ID}, "token": {token}}, "")
	assert.ErrorIs(t, err, ErrInvalidRequest)
	result, err := service.Introspect(context.Background(), url.Values{"client_id": {confidential.ID}, "client_secret": {secret}, "token": {token}}, "")
	require.NoError(t, err)
	assert.Equal(t, true, result["active"])
}

func TestAuthorizeRejectsRedirectAndPlainPKCE(t *testing.T) {
	assert.False(t, allowedRedirect(`["https://client.example/callback"]`, "https://evil.example/callback"))
	assert.False(t, verifyPKCE("wrong", "verifier"))
	assert.True(t, allowedScopes("openid profile", "openid"))
	assert.False(t, allowedScopes("openid", "admin"))
}

func TestClientCredentialsAndSecretRotation(t *testing.T) {
	service, authentication, _ := newOAuthTestService(t)
	client, secret, err := service.CreateClient(t.Context(), "tenant", "Worker", nil, []string{"client_credentials"}, []string{"jobs.read", "jobs.write"}, false)
	require.NoError(t, err)
	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte(client.ID+":"+secret))
	tokens, err := service.Token(t.Context(), url.Values{"grant_type": {"client_credentials"}, "scope": {"jobs.read"}}, basic)
	require.NoError(t, err)
	claims, err := authentication.Authenticate(t.Context(), "Bearer "+tokens.AccessToken, "")
	require.NoError(t, err)
	assert.Equal(t, "client:"+client.ID, claims.Subject)
	assert.Equal(t, authnext.SubjectTypeOAuthClient, claims.SubjectType)
	assert.Equal(t, "tenant", claims.OrganizationID)

	_, err = service.db.NewUpdate().Model((*clientRow)(nil)).Set("status = 'disabled'").Where("id = ?", client.ID).Exec(t.Context())
	require.NoError(t, err)
	_, err = authentication.Authenticate(t.Context(), "Bearer "+tokens.AccessToken, "")
	assert.ErrorIs(t, err, authnext.ErrInvalidToken)
	_, err = service.db.NewUpdate().Model((*clientRow)(nil)).Set("status = 'active'").Where("id = ?", client.ID).Exec(t.Context())
	require.NoError(t, err)
	_, err = authentication.Authenticate(t.Context(), "Bearer "+tokens.AccessToken, "")
	require.NoError(t, err)

	updated, err := service.UpdateClient(t.Context(), "tenant", client.ID, "Worker Updated", nil, []string{"client_credentials"}, []string{"jobs.read"}, false, "active")
	require.NoError(t, err)
	assert.Equal(t, "Worker Updated", updated.Name)
	rotated, err := service.RotateClientSecret(t.Context(), "tenant", client.ID)
	require.NoError(t, err)
	assert.NotEqual(t, secret, rotated)
	_, err = service.Token(t.Context(), url.Values{"grant_type": {"client_credentials"}, "scope": {"jobs.read"}}, basic)
	assert.ErrorIs(t, err, ErrInvalidRequest)
	assert.NoError(t, service.DeleteClient(t.Context(), "tenant", client.ID))
	_, err = authentication.Authenticate(t.Context(), "Bearer "+tokens.AccessToken, "")
	assert.ErrorIs(t, err, authnext.ErrInvalidToken)
}

func TestServiceAccountClientCredentials(t *testing.T) {
	service, authentication, _ := newOAuthTestService(t)
	identities := identity.NewService(service.db, oauthAuditAppender(service.audit), iam.NewAdministratorGuard())
	account, err := identities.CreateServiceAccount(t.Context(), "tenant", "automation", "Automation", "", nil)
	require.NoError(t, err)
	credential, err := identities.CreateServiceAccountCredential(t.Context(), "tenant", account.ID, "primary", []string{"jobs.read", "jobs.write"}, nil)
	require.NoError(t, err)

	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {credential.Credential.ID}, "client_secret": {credential.Secret}, "scope": {"jobs.read"}}
	tokens, err := service.Token(t.Context(), form, "")
	require.NoError(t, err)
	assert.Equal(t, "jobs.read", tokens.Scope)
	claims, err := authentication.Authenticate(t.Context(), "Bearer "+tokens.AccessToken, "")
	require.NoError(t, err)
	assert.Equal(t, account.ID, claims.Subject)
	assert.Equal(t, authnext.SubjectTypeServiceAccount, claims.SubjectType)
	assert.Equal(t, "tenant", claims.OrganizationID)

	form.Set("scope", "jobs.delete")
	_, err = service.Token(t.Context(), form, "")
	assert.ErrorIs(t, err, ErrInvalidRequest)
	form.Set("scope", "")
	form.Set("client_secret", "wrong secret")
	_, err = service.Token(t.Context(), form, "")
	assert.ErrorIs(t, err, ErrInvalidRequest)
	require.NoError(t, identities.RevokeServiceAccountCredential(t.Context(), "tenant", account.ID, credential.Credential.ID))
	form.Set("client_secret", credential.Secret)
	_, err = service.Token(t.Context(), form, "")
	assert.ErrorIs(t, err, ErrInvalidRequest)
}

func TestOAuthClientValidation(t *testing.T) {
	validRedirect := []string{"https://app.example/callback"}
	validScopes := []string{"openid"}
	tests := []struct {
		name      string
		tenant    string
		client    string
		redirects []string
		grants    []string
		scopes    []string
		public    bool
	}{
		{name: "tenant", client: "client", grants: []string{"client_credentials"}, scopes: validScopes},
		{name: "name", tenant: "tenant", grants: []string{"client_credentials"}, scopes: validScopes},
		{name: "grant", tenant: "tenant", client: "client", grants: []string{"password"}, scopes: validScopes},
		{name: "public credentials", tenant: "tenant", client: "client", grants: []string{"client_credentials"}, scopes: validScopes, public: true},
		{name: "redirect required", tenant: "tenant", client: "client", grants: []string{"authorization_code"}, scopes: validScopes},
		{name: "redirect scheme", tenant: "tenant", client: "client", redirects: []string{"file:///tmp/callback"}, grants: []string{"authorization_code"}, scopes: validScopes},
		{name: "redirect fragment", tenant: "tenant", client: "client", redirects: []string{"https://app.example/callback#fragment"}, grants: []string{"authorization_code"}, scopes: validScopes},
		{name: "scope", tenant: "tenant", client: "client", redirects: validRedirect, grants: []string{"authorization_code"}, scopes: []string{"two words"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.ErrorIs(t, validateClient(test.tenant, test.client, test.redirects, test.grants, test.scopes, test.public), ErrInvalidRequest)
		})
	}
	assert.NoError(t, validateClient("tenant", "client", validRedirect, []string{"authorization_code", "refresh_token"}, validScopes, true))
	assert.Equal(t, "email openid profile", normalizeWords(" profile  openid email profile "))
	assert.False(t, allowedRedirect("not-json", "https://app.example/callback"))
}

func TestOAuthProtocolRejectionPaths(t *testing.T) {
	service, authentication, principalID := newOAuthTestService(t)
	public, _, err := service.CreateClient(t.Context(), "tenant", "Browser", []string{"https://app.example/callback"}, []string{"authorization_code", "refresh_token"}, []string{"openid"}, true)
	require.NoError(t, err)
	confidential, secret, err := service.CreateClient(t.Context(), "tenant", "Worker", nil, []string{"client_credentials"}, []string{"jobs.read"}, false)
	require.NoError(t, err)

	_, err = service.ListClients(t.Context(), "")
	assert.ErrorIs(t, err, ErrInvalidRequest)
	_, err = service.UpdateClient(t.Context(), "tenant", public.ID, "Browser", public.RedirectURIs, public.GrantTypes, public.Scopes, true, "unknown")
	assert.ErrorIs(t, err, ErrInvalidRequest)
	_, err = service.Token(t.Context(), url.Values{"grant_type": {"client_credentials"}, "client_id": {public.ID}}, "")
	assert.ErrorIs(t, err, ErrInvalidRequest)
	_, err = service.Token(t.Context(), url.Values{"grant_type": {"unknown"}, "client_id": {confidential.ID}, "client_secret": {secret}}, "")
	assert.ErrorIs(t, err, ErrInvalidRequest)
	_, err = service.Token(t.Context(), url.Values{}, "Basic invalid-base64")
	assert.ErrorIs(t, err, ErrInvalidRequest)
	_, err = service.Token(t.Context(), url.Values{}, "Basic "+base64.StdEncoding.EncodeToString([]byte("missing-colon")))
	assert.ErrorIs(t, err, ErrInvalidRequest)
	_, err = service.Token(t.Context(), url.Values{"client_id": {public.ID}, "client_secret": {"unexpected"}}, "")
	assert.ErrorIs(t, err, ErrInvalidRequest)

	_, err = service.Authorize(t.Context(), "", public.ID, public.RedirectURIs[0], "token", "openid", "", strings.Repeat("a", 43), "S256", "", "")
	assert.ErrorIs(t, err, ErrInvalidRequest)
	_, err = service.Authorize(t.Context(), "", public.ID, public.RedirectURIs[0], "code", "openid", "", strings.Repeat("a", 43), "S256", "", "")
	assert.ErrorIs(t, err, authnext.ErrInvalidCredentials)
	session, _, err := authentication.Login(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	_, err = service.db.ExecContext(t.Context(), "UPDATE iam_tenant_members SET status = 'disabled' WHERE tenant_id = ? AND user_subject = ?", "tenant", principalID)
	require.NoError(t, err)
	_, err = service.Authorize(t.Context(), authentication.SessionCookie(session), public.ID, public.RedirectURIs[0], "code", "openid", "", strings.Repeat("a", 43), "S256", "", "")
	assert.ErrorIs(t, err, ErrInvalidRequest)

	assert.NoError(t, service.Revoke(t.Context(), url.Values{"client_id": {public.ID}}, ""))
	assert.NoError(t, service.Revoke(t.Context(), url.Values{"client_id": {public.ID}, "token": {"unknown"}}, ""))
	assert.ErrorIs(t, service.Revoke(t.Context(), url.Values{"client_id": {"missing"}}, ""), ErrInvalidRequest)
	inactive, err := service.Introspect(t.Context(), url.Values{"client_id": {confidential.ID}, "client_secret": {secret}, "token": {"invalid"}}, "")
	require.NoError(t, err)
	assert.Equal(t, false, inactive["active"])
	_, _, err = service.CreateClient(t.Context(), "", "Worker", nil, []string{"client_credentials"}, []string{"jobs.read"}, false)
	assert.ErrorIs(t, err, ErrInvalidRequest)
	_, err = service.UpdateClient(t.Context(), "", public.ID, "Browser", public.RedirectURIs, public.GrantTypes, public.Scopes, true, "active")
	assert.ErrorIs(t, err, ErrInvalidRequest)
	_, err = service.client(t.Context(), "")
	assert.ErrorIs(t, err, ErrInvalidRequest)
	_, err = service.Token(t.Context(), url.Values{"grant_type": {"authorization_code"}, "client_id": {public.ID}}, "")
	assert.ErrorIs(t, err, ErrInvalidRequest)
}

func TestOAuthStoredGrantExpirationAndReplay(t *testing.T) {
	service, authentication, principalID := newOAuthTestService(t)
	client, _, err := service.CreateClient(t.Context(), "tenant", "Browser", []string{"https://app.example/callback"}, []string{"authorization_code", "refresh_token"}, []string{"openid", "profile"}, true)
	require.NoError(t, err)
	session, _, err := authentication.Login(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])

	_, err = service.Authorize(t.Context(), authentication.SessionCookie(session), client.ID, client.RedirectURIs[0], "code", "admin", "", challenge, "S256", "", "")
	assert.ErrorIs(t, err, ErrInvalidRequest)

	redirect, err := service.Authorize(t.Context(), authentication.SessionCookie(session), client.ID, client.RedirectURIs[0], "code", "openid", "", challenge, "S256", "", "")
	require.NoError(t, err)
	parsed, err := url.Parse(redirect)
	require.NoError(t, err)
	code := parsed.Query().Get("code")
	_, err = service.db.NewUpdate().Model((*codeRow)(nil)).Set("expires_at = 0").Where("code_hash = ?", hashToken(code)).Exec(t.Context())
	require.NoError(t, err)
	_, err = service.Token(t.Context(), url.Values{"grant_type": {"authorization_code"}, "client_id": {client.ID}, "code": {code}, "redirect_uri": {client.RedirectURIs[0]}, "code_verifier": {verifier}}, "")
	assert.ErrorIs(t, err, ErrInvalidRequest)

	now := service.now().UTC().UnixMilli()
	_, err = service.db.NewInsert().Model(&refreshRow{IDHash: hashToken("expired-refresh"), FamilyID: "family", PrincipalID: principalID, ClientID: client.ID, Scope: "openid", CreatedAt: now - 1000, ExpiresAt: now - 1}).Exec(t.Context())
	require.NoError(t, err)
	_, err = service.Token(t.Context(), url.Values{"grant_type": {"refresh_token"}, "client_id": {client.ID}, "refresh_token": {"expired-refresh"}}, "")
	assert.ErrorIs(t, err, ErrInvalidRequest)
	_, err = service.Token(t.Context(), url.Values{"grant_type": {"refresh_token"}, "client_id": {client.ID}}, "")
	assert.ErrorIs(t, err, ErrInvalidRequest)

	disabled, err := service.UpdateClient(t.Context(), "tenant", client.ID, client.Name, client.RedirectURIs, client.GrantTypes, client.Scopes, true, "disabled")
	require.NoError(t, err)
	assert.Equal(t, "disabled", disabled.Status)
	_, err = service.Token(t.Context(), url.Values{"grant_type": {"refresh_token"}, "client_id": {client.ID}, "refresh_token": {"expired-refresh"}}, "")
	assert.ErrorIs(t, err, ErrInvalidRequest)
}

func TestOAuthDatabaseFailurePropagation(t *testing.T) {
	service, _, _ := newOAuthTestService(t)
	require.NoError(t, service.db.Close())

	_, _, err := service.CreateClient(t.Context(), "tenant", "Worker", nil, []string{"client_credentials"}, []string{"jobs.read"}, false)
	assert.ErrorContains(t, err, "create OAuth client")
	_, err = service.ListClients(t.Context(), "tenant")
	assert.Error(t, err)
	_, err = service.UpdateClient(t.Context(), "tenant", "client", "Worker", nil, []string{"client_credentials"}, []string{"jobs.read"}, false, "active")
	assert.Error(t, err)
	_, err = service.RotateClientSecret(t.Context(), "tenant", "client")
	assert.Error(t, err)
	assert.Error(t, service.DeleteClient(t.Context(), "tenant", "client"))
}

func TestOAuthServiceRequiresDependencies(t *testing.T) {
	assert.Panics(t, func() { NewService(nil, nil) })
}

func TestOAuthAuthorizationPersistenceFailures(t *testing.T) {
	t.Run("authorization code storage", func(t *testing.T) {
		service, authentication, _ := newOAuthTestService(t)
		client, _, err := service.CreateClient(t.Context(), "tenant", "Browser", []string{"https://app.example/callback"}, []string{"authorization_code"}, []string{"openid"}, true)
		require.NoError(t, err)
		session, _, err := authentication.Login(t.Context(), "admin", "correct horse battery staple", "")
		require.NoError(t, err)
		_, err = service.db.ExecContext(t.Context(), "DROP TABLE iam_oauth_codes")
		require.NoError(t, err)
		_, err = service.Authorize(t.Context(), authentication.SessionCookie(session), client.ID, client.RedirectURIs[0], "code", "openid", "", strings.Repeat("a", 43), "S256", "", "")
		assert.ErrorContains(t, err, "store authorization code")
	})

	t.Run("membership lookup", func(t *testing.T) {
		service, authentication, _ := newOAuthTestService(t)
		client, _, err := service.CreateClient(t.Context(), "tenant", "Browser", []string{"https://app.example/callback"}, []string{"authorization_code"}, []string{"openid"}, true)
		require.NoError(t, err)
		session, _, err := authentication.Login(t.Context(), "admin", "correct horse battery staple", "")
		require.NoError(t, err)
		verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"
		digest := sha256.Sum256([]byte(verifier))
		redirect, err := service.Authorize(t.Context(), authentication.SessionCookie(session), client.ID, client.RedirectURIs[0], "code", "openid", "", base64.RawURLEncoding.EncodeToString(digest[:]), "S256", "", "")
		require.NoError(t, err)
		location, err := url.Parse(redirect)
		require.NoError(t, err)
		_, err = service.db.ExecContext(t.Context(), "DROP TABLE iam_tenant_members")
		require.NoError(t, err)
		_, err = service.Token(t.Context(), url.Values{
			"grant_type": {"authorization_code"}, "client_id": {client.ID}, "code": {location.Query().Get("code")},
			"redirect_uri": {client.RedirectURIs[0]}, "code_verifier": {verifier},
		}, "")
		assert.ErrorIs(t, err, ErrInvalidRequest)
	})

	t.Run("invalid stored redirect", func(t *testing.T) {
		service, authentication, _ := newOAuthTestService(t)
		now := time.Now().UTC().UnixMilli()
		row := clientRow{ID: "invalid-redirect", TenantID: "tenant", Name: "Browser", RedirectURIs: `["%"]`, GrantTypes: "authorization_code", Scopes: "openid", PublicClient: true, Status: "active", CreatedAt: now, UpdatedAt: now}
		_, err := service.db.NewInsert().Model(&row).Exec(t.Context())
		require.NoError(t, err)
		session, _, err := authentication.Login(t.Context(), "admin", "correct horse battery staple", "")
		require.NoError(t, err)
		_, err = service.Authorize(t.Context(), authentication.SessionCookie(session), row.ID, "%", "code", "openid", "", strings.Repeat("a", 43), "S256", "", "")
		assert.ErrorIs(t, err, ErrInvalidRequest)
	})

	t.Run("refresh token storage", func(t *testing.T) {
		service, _, principalID := newOAuthTestService(t)
		client, _, err := service.CreateClient(t.Context(), "tenant", "Browser", []string{"https://app.example/callback"}, []string{"refresh_token"}, []string{"openid"}, true)
		require.NoError(t, err)
		now := service.now().UTC().UnixMilli()
		_, err = service.db.NewInsert().Model(&refreshRow{IDHash: hashToken("refresh"), FamilyID: "family", PrincipalID: principalID, ClientID: client.ID, Scope: "openid", CreatedAt: now, ExpiresAt: now + time.Hour.Milliseconds()}).Exec(t.Context())
		require.NoError(t, err)
		_, err = service.db.ExecContext(t.Context(), "DROP TABLE iam_refresh_tokens")
		require.NoError(t, err)
		_, err = service.Token(t.Context(), url.Values{"grant_type": {"refresh_token"}, "client_id": {client.ID}, "refresh_token": {"refresh"}}, "")
		assert.ErrorIs(t, err, ErrInvalidRequest)
	})
}

// TestOAuthServicePropagatesDatabaseFailures drops the real client table so the
// driver produces genuine errors, exercising the failure branches that the
// happy-path tests never reach.
func TestOAuthServicePropagatesDatabaseFailures(t *testing.T) {
	service, _, _ := newOAuthTestService(t)
	_, err := service.db.ExecContext(t.Context(), "DROP TABLE iam_oauth_clients")
	require.NoError(t, err)

	_, listErr := service.ListClients(t.Context(), "tenant")
	assert.Error(t, listErr)
	_, _, createErr := service.CreateClient(t.Context(), "tenant", "Broken", []string{"https://app.example/cb"}, []string{"authorization_code"}, []string{"openid"}, true)
	assert.Error(t, createErr)
	_, updateErr := service.UpdateClient(t.Context(), "tenant", "missing", "Broken", []string{"https://app.example/cb"}, []string{"authorization_code"}, []string{"openid"}, true, "active")
	assert.Error(t, updateErr)
	_, rotateErr := service.RotateClientSecret(t.Context(), "tenant", "missing")
	assert.Error(t, rotateErr)
	assert.Error(t, service.DeleteClient(t.Context(), "tenant", "missing"))
}

func newOAuthTestService(t *testing.T) (*Service, *authnmod.WebService, string) {
	t.Helper()
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(context.Background(), db))
	require.NoError(t, organization.Migrate(context.Background(), db))
	require.NoError(t, organization.EnsureTenant(context.Background(), db, "tenant"))
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	auth, err := authnmod.NewWebService(authnext.Config{
		Enabled: true, Issuer: "https://iam.example", Audience: []string{"api"}, SigningKey: base64.RawStdEncoding.EncodeToString(seed),
		MFA: authnext.MFAConfig{EncryptionKey: base64.RawStdEncoding.EncodeToString(seed)},
		Web: authnext.WebConfig{Enabled: true, CookieName: "session", SessionTTL: time.Hour, IdleTTL: time.Minute, PostLoginURL: "https://app.example/", AllowedReturnURLs: []string{"https://app.example/"}, AllowedOrigins: []string{"https://app.example"}},
	}, db)
	require.NoError(t, err)
	principalID, err := authnmod.EnsureBootstrapPrincipal(context.Background(), db, authnmod.BootstrapPrincipal{LoginName: "admin", Password: "correct horse battery staple"})
	require.NoError(t, err)
	_, err = db.ExecContext(context.Background(), `INSERT INTO iam_tenant_members
 (tenant_id, user_subject, display_name, email, status, created_at, updated_at, disabled_at)
 VALUES (?, ?, ?, ?, 'active', ?, ?, 0)`, "tenant", principalID, "Admin", "", time.Now().UTC().UnixMilli(), time.Now().UTC().UnixMilli())
	require.NoError(t, err)
	return NewService(db, auth), auth, principalID
}

func oauthAuditAppender(service *auditmod.Service) auditx.Appender {
	return func(ctx context.Context, db bun.IDB, event auditx.Event) error {
		_, err := service.AppendTo(ctx, db, auditmod.EventInput{
			TenantID: event.TenantID, PrincipalID: event.PrincipalID,
			EventType: event.EventType, TargetType: event.TargetType, TargetID: event.TargetID,
			Outcome: "success", Detail: event.Detail,
		})
		return err
	}
}
