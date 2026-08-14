package federation

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	auditmod "github.com/chaos-plus/chaosplus/internal/modules/audit"
	authnmod "github.com/chaos-plus/chaosplus/internal/modules/authn"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	identitymod "github.com/chaos-plus/chaosplus/internal/modules/identity"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

type federationEnvironment struct {
	db         *bun.DB
	service    *Service
	web        *authnmod.WebService
	audit      auditx.Appender
	identities *identitymod.Service
	key        []byte
	idp        *testIDP
}

func newFederationEnvironment(t *testing.T) *federationEnvironment {
	t.Helper()
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	require.NoError(t, organization.Migrate(t.Context(), db))
	require.NoError(t, Migrate(t.Context(), db))
	require.NoError(t, organization.EnsureTenant(t.Context(), db, testID("tenant-a")))

	auditService := auditmod.NewService(db, newTestIDGenerator())
	appendAudit := func(ctx context.Context, executor bun.IDB, event auditx.Event) error {
		_, err := auditService.AppendTo(ctx, executor, auditmod.EventInput{
			TenantID: event.TenantID, PrincipalID: event.PrincipalID, EventType: event.EventType,
			TargetType: event.TargetType, TargetID: event.TargetID, Outcome: "success", Detail: event.Detail,
		})
		return err
	}
	identities := identitymod.NewService(db, appendAudit, iam.NewAdministratorGuard(), newTestIDGenerator())
	seed := make([]byte, 32)
	_, err = rand.Read(seed)
	require.NoError(t, err)
	web, err := authnmod.NewWebService(authnext.Config{
		Enabled: true, Issuer: "https://iam.example", Audience: []string{"api"},
		SigningKey: base64.RawStdEncoding.EncodeToString(seed), AccessTokenTTL: time.Minute,
		MFA:     authnext.MFAConfig{EncryptionKey: base64.RawStdEncoding.EncodeToString(seed)},
		Passkey: authnext.PasskeyConfig{Enabled: true, RPID: "app.example", DisplayName: "Chaosplus", Origins: []string{"https://app.example"}},
		Web:     authnext.WebConfig{Enabled: true, CookieName: "cp_session", SessionTTL: time.Hour, IdleTTL: 10 * time.Minute, PostLoginURL: "https://app.example/", PostLogoutURL: "https://app.example/login", AllowedReturnURLs: []string{"https://app.example/"}, AllowedOrigins: []string{"https://app.example"}, CookieSecure: true},
	}, db, authnmod.WithIDGenerator(newTestIDGenerator()))
	require.NoError(t, err)
	key := make([]byte, 32)
	_, err = rand.Read(key)
	require.NoError(t, err)
	service := NewService(db, appendAudit, identities, web, Config{HTTPTimeout: 5 * time.Second, ClockSkew: 30 * time.Second, StateTTL: 10 * time.Minute}, key, newTestIDGenerator())
	return &federationEnvironment{db: db, service: service, web: web, audit: appendAudit, identities: identities, key: key, idp: newTestIDP(t)}
}

// TestFederationServicePropagatesDatabaseFailures drops the real provider table
// so the driver produces genuine errors, exercising the failure branches the
// happy-path tests never reach.
func TestFederationServicePropagatesDatabaseFailures(t *testing.T) {
	env := newFederationEnvironment(t)
	input := ProviderInput{
		Name: "Broken", ProviderType: "oidc", Issuer: "https://idp.example",
		ClientID: "client", ClientSecret: "secret", Scopes: "openid", Status: "active",
	}
	_, err := env.db.ExecContext(t.Context(), "DROP TABLE iam_identity_providers")
	require.NoError(t, err)

	_, listErr := env.service.ListProviders(t.Context(), testID("tenant-a"))
	assert.Error(t, listErr)
	_, createErr := env.service.CreateProvider(t.Context(), testID("tenant-a"), input)
	assert.Error(t, createErr)
	_, updateErr := env.service.UpdateProvider(t.Context(), testID("tenant-a"), testID("missing"), input)
	assert.Error(t, updateErr)
	assert.Error(t, env.service.DeleteProvider(t.Context(), testID("tenant-a"), testID("missing")))
	_, startErr := env.service.StartLogin(t.Context(), testID("missing"), "https://app.example/", "https://app.example/cb")
	assert.Error(t, startErr)
}

func (env *federationEnvironment) createProvider(t *testing.T, input ProviderInput) Provider {
	t.Helper()
	provider, err := env.service.CreateProvider(t.Context(), testID("tenant-a"), input)
	require.NoError(t, err)
	return provider
}

func (env *federationEnvironment) login(t *testing.T, provider Provider, returnURL string) (LoginComplete, error) {
	t.Helper()
	ctx := t.Context()
	callback := env.idp.issuer + callbackURL(provider.ID)
	start, err := env.service.StartLogin(ctx, provider.ID, returnURL, callback)
	if err != nil {
		return LoginComplete{}, err
	}
	stateValue, err := cookieValue(start.StateCookie, stateCookieName)
	require.NoError(t, err)
	state, err := openState(env.key, stateValue)
	require.NoError(t, err)
	code, returnedState := env.idp.authorize(t, env.idp.server.Client(), provider.Issuer, state.CodeVerifier, state.Nonce)
	require.Equal(t, state.Nonce, returnedState)
	return env.service.CompleteLogin(ctx, provider.ID, code, state.Nonce, "cp_federation_state="+stateValue, callback)
}

func (env *federationEnvironment) providerInput(name, issuer string) ProviderInput {
	return ProviderInput{
		Name: name, ProviderType: ProviderOIDC, Issuer: issuer, ClientID: env.idp.clientID,
		ClientSecret: "client-secret", Scopes: "", AutoProvision: true, Status: ProviderActive,
	}
}

func insertRole(t *testing.T, db *bun.DB, tenantID, roleID string) {
	t.Helper()
	now := time.Now().UTC().UnixMilli()
	_, err := db.ExecContext(t.Context(), `INSERT INTO iam_roles (tenant_id,id,name,description,created_at,updated_at) VALUES (?,?,?,?,?,?)`, testID(tenantID), testID(roleID), roleID, "", now, now)
	require.NoError(t, err)
}

func TestProviderManagement(t *testing.T) {
	env := newFederationEnvironment(t)
	ctx := t.Context()

	_, err := env.service.CreateProvider(ctx, 0, env.providerInput("X", "https://issuer.example"))
	assert.ErrorIs(t, err, ErrInvalidProvider)
	_, err = env.service.CreateProvider(ctx, testID("tenant-a"), env.providerInput("X", "not a url"))
	assert.ErrorIs(t, err, ErrInvalidProvider)
	longName := make([]byte, 129)
	for i := range longName {
		longName[i] = 'a'
	}
	_, err = env.service.CreateProvider(ctx, testID("tenant-a"), env.providerInput(string(longName), "https://issuer.example"))
	assert.ErrorIs(t, err, ErrInvalidProvider)
	input := env.providerInput("X", "https://issuer.example")
	input.Status = "pending"
	_, err = env.service.CreateProvider(ctx, testID("tenant-a"), input)
	assert.ErrorIs(t, err, ErrInvalidProvider)
	input = env.providerInput("X", "https://issuer.example")
	input.Scopes = "openid " + string(make([]byte, 65))
	_, err = env.service.CreateProvider(ctx, testID("tenant-a"), input)
	assert.ErrorIs(t, err, ErrInvalidProvider)

	missingRole := env.providerInput("X", "https://issuer.example/")
	missingRole.DefaultRoleID = testID("role-a")
	_, err = env.service.CreateProvider(ctx, testID("tenant-a"), missingRole)
	assert.ErrorIs(t, err, ErrProviderRoleMissing)
	insertRole(t, env.db, "tenant-a", "role-a")
	provider := env.createProvider(t, ProviderInput{
		Name: "IdP", ProviderType: ProviderOIDC, Issuer: "https://issuer.example/", ClientID: env.idp.clientID,
		ClientSecret: "s3cret", Scopes: "", AutoProvision: true, DefaultRoleID: testID("role-a"), Status: ProviderActive,
	})
	assert.Equal(t, "https://issuer.example", provider.Issuer)
	assert.Equal(t, defaultScopes, provider.Scopes)
	assert.True(t, provider.ClientSecretSet)

	var row providerRow
	require.NoError(t, env.db.NewSelect().Model(&row).Where("id = ?", provider.ID).Scan(ctx))
	plain, err := env.service.decryptSecret(row.ID, row.ClientSecretCiphertext)
	require.NoError(t, err)
	assert.Equal(t, "s3cret", plain)

	_, err = env.service.CreateProvider(ctx, testID("tenant-a"), env.providerInput("Other", "https://issuer.example"))
	assert.ErrorIs(t, err, ErrProviderIssuerExists)

	items, err := env.service.ListProviders(ctx, testID("tenant-a"))
	require.NoError(t, err)
	assert.Len(t, items, 1)
	other, err := env.service.ListProviders(ctx, testID("tenant-b"))
	require.NoError(t, err)
	assert.Empty(t, other)
	_, err = env.service.ListProviders(ctx, 0)
	assert.ErrorIs(t, err, ErrInvalidProvider)

	updated, err := env.service.UpdateProvider(ctx, testID("tenant-a"), provider.ID, ProviderInput{
		Name: "Renamed", ProviderType: ProviderOIDC, Issuer: "https://issuer.example", ClientID: env.idp.clientID,
		AutoProvision: true, DefaultRoleID: testID("role-a"), Status: ProviderActive,
	})
	require.NoError(t, err)
	assert.Equal(t, "Renamed", updated.Name)
	require.NoError(t, env.db.NewSelect().Model(&row).Where("id = ?", provider.ID).Scan(ctx))
	plain, err = env.service.decryptSecret(row.ID, row.ClientSecretCiphertext)
	require.NoError(t, err)
	assert.Equal(t, "s3cret", plain)

	_, err = env.service.UpdateProvider(ctx, testID("tenant-a"), provider.ID, ProviderInput{
		Name: "Rotated", ProviderType: ProviderOIDC, Issuer: "https://issuer.example", ClientID: env.idp.clientID,
		ClientSecret: "new-secret", AutoProvision: true, DefaultRoleID: testID("role-a"), Status: ProviderActive,
	})
	require.NoError(t, err)
	require.NoError(t, env.db.NewSelect().Model(&row).Where("id = ?", provider.ID).Scan(ctx))
	plain, err = env.service.decryptSecret(row.ID, row.ClientSecretCiphertext)
	require.NoError(t, err)
	assert.Equal(t, "new-secret", plain)

	_, err = env.service.UpdateProvider(ctx, testID("tenant-a"), testID("missing"), env.providerInput("X", "https://other.example"))
	assert.ErrorIs(t, err, ErrProviderNotFound)

	require.NoError(t, env.service.DeleteProvider(ctx, testID("tenant-a"), provider.ID))
	assert.ErrorIs(t, env.service.DeleteProvider(ctx, testID("tenant-a"), provider.ID), ErrProviderNotFound)
	second := env.createProvider(t, env.providerInput("Second", "https://second.example"))
	assert.ErrorIs(t, env.service.DeleteProvider(ctx, testID("tenant-b"), second.ID), ErrProviderNotFound)
}

func TestParseAndResolveEncryptionKey(t *testing.T) {
	_, err := ParseEncryptionKey("")
	assert.Error(t, err)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	parsed, err := ParseEncryptionKey(base64.RawStdEncoding.EncodeToString(key))
	require.NoError(t, err)
	assert.Equal(t, key, parsed)
	parsed, err = ParseEncryptionKey(base64.URLEncoding.EncodeToString(key))
	require.NoError(t, err)
	assert.Equal(t, key, parsed)
	_, err = ParseEncryptionKey(base64.RawStdEncoding.EncodeToString([]byte("short")))
	assert.Error(t, err)
	_, err = ParseEncryptionKey("!!not-base64!!")
	assert.Error(t, err)

	file := filepath.Join(t.TempDir(), "federation.key")
	require.NoError(t, os.WriteFile(file, []byte(base64.RawStdEncoding.EncodeToString(key)), 0o600))
	resolved, err := ResolveEncryptionKey(Config{EncryptionKeyFile: file})
	require.NoError(t, err)
	assert.Equal(t, key, resolved)
	resolved, err = ResolveEncryptionKey(Config{EncryptionKey: base64.RawStdEncoding.EncodeToString(key)})
	require.NoError(t, err)
	assert.Equal(t, key, resolved)
	_, err = ResolveEncryptionKey(Config{})
	assert.Error(t, err)
	_, err = ResolveEncryptionKey(Config{EncryptionKeyFile: filepath.Join(t.TempDir(), "missing")})
	assert.Error(t, err)
}

func TestFederationLoginJITProvisioning(t *testing.T) {
	env := newFederationEnvironment(t)
	ctx := t.Context()
	env.idp.secret = "client-secret"
	provider := env.createProvider(t, env.providerInput("GitLab", env.idp.issuer))

	callback := env.idp.issuer + callbackURL(provider.ID)
	start, err := env.service.StartLogin(ctx, provider.ID, "https://app.example/", callback)
	require.NoError(t, err)
	authorizationURL, err := url.Parse(start.AuthorizationURL)
	require.NoError(t, err)
	query := authorizationURL.Query()
	assert.Equal(t, "code", query.Get("response_type"))
	assert.Equal(t, callback, query.Get("redirect_uri"))
	assert.Equal(t, "S256", query.Get("code_challenge_method"))
	assert.NotEmpty(t, query.Get("code_challenge"))
	assert.NotEmpty(t, query.Get("state"))
	assert.Contains(t, start.StateCookie, stateCookieName+"=")

	result, err := env.login(t, provider, "https://app.example/")
	require.NoError(t, err)
	assert.Equal(t, "https://app.example/", result.ReturnURL)
	assert.NotEmpty(t, result.SessionToken)

	claims, err := env.web.Authenticate(ctx, "", env.web.SessionCookie(result.SessionToken))
	require.NoError(t, err)
	assert.Equal(t, "user@example.com", claims.Email)
	assert.True(t, claims.EmailVerified)
	assert.Equal(t, []string{"oidc"}, claims.AMR)

	var link identityLinkRow
	require.NoError(t, env.db.NewSelect().Model(&link).Where("provider_id = ?", provider.ID).Scan(ctx))
	assert.Equal(t, parseGUID(claims.Subject), link.PrincipalID)
	assert.Equal(t, "external-user-1", link.ExternalSubject)
	assert.Equal(t, "user@example.com", link.Email)

	second, err := env.login(t, provider, "https://app.example/")
	require.NoError(t, err)
	secondClaims, err := env.web.Authenticate(ctx, "", env.web.SessionCookie(second.SessionToken))
	require.NoError(t, err)
	assert.Equal(t, claims.Subject, secondClaims.Subject)
	count, err := env.db.NewSelect().Table("iam_principals").Where("email = ?", "user@example.com").Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.Contains(t, env.service.StateClearCookie(), stateCookieName+"=")
}

func TestFederationLoginPublicClientAndDenials(t *testing.T) {
	env := newFederationEnvironment(t)
	ctx := t.Context()

	public := env.createProvider(t, ProviderInput{
		Name: "Public", ProviderType: ProviderOIDC, Issuer: env.idp.issuer, ClientID: env.idp.clientID,
		AutoProvision: true, Status: ProviderActive,
	})
	result, err := env.login(t, public, "https://app.example/")
	require.NoError(t, err)
	assert.NotEmpty(t, result.SessionToken)

	noProvision := env.createProvider(t, ProviderInput{
		Name: "NoProvision", ProviderType: ProviderOIDC, Issuer: env.idp.issuer + "/no-provision", ClientID: env.idp.clientID,
		AutoProvision: false, Status: ProviderActive,
	})
	_, err = env.login(t, noProvision, "https://app.example/")
	assert.ErrorIs(t, err, ErrProvisioningDisabled)

	unverified := env.createProvider(t, env.providerInput("Unverified", env.idp.issuer+"/unverified"))
	env.idp.setClaim("email_verified", false)
	_, err = env.login(t, unverified, "https://app.example/")
	assert.ErrorIs(t, err, ErrProvisioningUnverified)
	env.idp.setClaim("email_verified", true)

	disabled := env.createProvider(t, env.providerInput("Disabled", env.idp.issuer+"/disabled"))
	_, err = env.service.UpdateProvider(ctx, testID("tenant-a"), disabled.ID, ProviderInput{
		Name: "Disabled", ProviderType: ProviderOIDC, Issuer: env.idp.issuer + "/disabled", ClientID: env.idp.clientID,
		AutoProvision: true, Status: ProviderDisabled,
	})
	require.NoError(t, err)
	_, err = env.service.StartLogin(ctx, disabled.ID, "https://app.example/", env.idp.issuer+callbackURL(disabled.ID))
	assert.ErrorIs(t, err, ErrProviderDisabled)

	missingState := env.createProvider(t, env.providerInput("MissingState", env.idp.issuer+"/missing-state"))
	_, err = env.service.CompleteLogin(ctx, missingState.ID, "code", "state", "", env.idp.issuer+callbackURL(missingState.ID))
	assert.ErrorIs(t, err, ErrOIDCState)

	stateProvider := env.createProvider(t, env.providerInput("State", env.idp.issuer+"/state"))
	callback := env.idp.issuer + callbackURL(stateProvider.ID)
	start, err := env.service.StartLogin(ctx, stateProvider.ID, "https://app.example/", callback)
	require.NoError(t, err)
	stateValue, err := cookieValue(start.StateCookie, stateCookieName)
	require.NoError(t, err)
	_, err = env.service.CompleteLogin(ctx, stateProvider.ID, "code", "wrong-state", "cp_federation_state="+stateValue, callback)
	assert.ErrorIs(t, err, ErrOIDCState)

	_, err = env.service.StartLogin(ctx, stateProvider.ID, "https://app.example/", "/federation/"+stateProvider.ID.String()+"/callback")
	assert.ErrorIs(t, err, ErrInvalidProvider)

	corrupted := env.createProvider(t, env.providerInput("Corrupted", env.idp.issuer+"/corrupted"))
	_, err = env.db.NewUpdate().Table("iam_identity_providers").Set("client_secret_ciphertext = ?", "garbage").Where("id = ?", corrupted.ID).Exec(ctx)
	require.NoError(t, err)
	_, err = env.login(t, corrupted, "https://app.example/")
	assert.Error(t, err)
}

func TestFederationDefaultRoleAndMemberStatus(t *testing.T) {
	env := newFederationEnvironment(t)
	ctx := t.Context()
	insertRole(t, env.db, "tenant-a", "role-a")
	provider := env.createProvider(t, ProviderInput{
		Name: "RoleGrant", ProviderType: ProviderOIDC, Issuer: env.idp.issuer + "/role-grant", ClientID: env.idp.clientID,
		AutoProvision: true, DefaultRoleID: testID("role-a"), Status: ProviderActive,
	})
	result, err := env.login(t, provider, "https://app.example/")
	require.NoError(t, err)
	claims, err := env.web.Authenticate(ctx, "", env.web.SessionCookie(result.SessionToken))
	require.NoError(t, err)
	count, err := env.db.NewSelect().Table("iam_role_members").Where("tenant_id = ? AND role_id = ? AND principal_id = ?", testID("tenant-a"), testID("role-a"), parseGUID(claims.Subject)).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	_, err = env.db.NewUpdate().Table("iam_tenant_members").Set("status = ?", "disabled").Where("tenant_id = ? AND principal_id = ?", testID("tenant-a"), parseGUID(claims.Subject)).Exec(ctx)
	require.NoError(t, err)
	_, err = env.login(t, provider, "https://app.example/")
	assert.ErrorIs(t, err, ErrPrincipalInactive)
}

func TestFederationMissingDefaultRoleAtProvisioning(t *testing.T) {
	env := newFederationEnvironment(t)
	insertRole(t, env.db, "tenant-a", "role-a")
	provider := env.createProvider(t, ProviderInput{
		Name: "RoleGone", ProviderType: ProviderOIDC, Issuer: env.idp.issuer + "/role-gone", ClientID: env.idp.clientID,
		AutoProvision: true, DefaultRoleID: testID("role-a"), Status: ProviderActive,
	})
	_, err := env.db.NewDelete().Table("iam_roles").Where("tenant_id = ? AND id = ?", "tenant-a", "role-a").Exec(t.Context())
	require.NoError(t, err)
	result, err := env.login(t, provider, "https://app.example/")
	require.NoError(t, err)
	assert.NotEmpty(t, result.SessionToken)
}

func TestFederationLoginStateAndProviderDenials(t *testing.T) {
	env := newFederationEnvironment(t)
	ctx := t.Context()

	_, err := env.service.StartLogin(ctx, testID("missing"), "https://app.example/", env.idp.issuer+callbackURL(testID("missing")))
	assert.ErrorIs(t, err, ErrProviderNotFound)
	_, err = env.service.StartLogin(ctx, 0, "https://app.example/", env.idp.issuer+callbackURL(testID("missing")))
	assert.ErrorIs(t, err, ErrProviderNotFound)

	provider := env.createProvider(t, env.providerInput("Denied", env.idp.issuer+"/denied"))
	_, err = env.service.StartLogin(ctx, provider.ID, "javascript:alert(1)", env.idp.issuer+callbackURL(provider.ID))
	assert.Error(t, err)

	disabled := env.createProvider(t, env.providerInput("DisabledCallback", env.idp.issuer+"/disabled-callback"))
	_, err = env.service.UpdateProvider(ctx, testID("tenant-a"), disabled.ID, ProviderInput{
		Name: "DisabledCallback", ProviderType: ProviderOIDC, Issuer: env.idp.issuer + "/disabled-callback", ClientID: env.idp.clientID,
		AutoProvision: true, Status: ProviderDisabled,
	})
	require.NoError(t, err)
	_, err = env.service.CompleteLogin(ctx, disabled.ID, "code", "state", "", env.idp.issuer+callbackURL(disabled.ID))
	assert.ErrorIs(t, err, ErrProviderDisabled)

	callback := env.idp.issuer + callbackURL(provider.ID)
	start, err := env.service.StartLogin(ctx, provider.ID, "https://app.example/", callback)
	require.NoError(t, err)
	stateValue, err := cookieValue(start.StateCookie, stateCookieName)
	require.NoError(t, err)
	state, err := openState(env.key, stateValue)
	require.NoError(t, err)

	state.ExpiresAt = time.Now().Add(-time.Minute).Unix()
	stale, err := sealState(env.key, state)
	require.NoError(t, err)
	_, err = env.service.CompleteLogin(ctx, provider.ID, "code", state.Nonce, "cp_federation_state="+stale, callback)
	assert.ErrorIs(t, err, ErrOIDCState)

	state.ExpiresAt = time.Now().Add(time.Minute).Unix()
	state.ProviderID = "other-provider"
	wrongProvider, err := sealState(env.key, state)
	require.NoError(t, err)
	_, err = env.service.CompleteLogin(ctx, provider.ID, "code", state.Nonce, "cp_federation_state="+wrongProvider, callback)
	assert.ErrorIs(t, err, ErrOIDCState)
}

func TestFederationHelpersAndSecretErrors(t *testing.T) {
	assert.Equal(t, defaultScopes, normalizeScopes(""))
	assert.Equal(t, "openid profile", normalizeScopes(" openid profile openid "))
	assert.Equal(t, "", normalizeScopes("openid "+strings.Repeat("x", 65)))

	env := newFederationEnvironment(t)
	ctx := t.Context()
	_, err := env.service.getProviderByID(ctx, 0)
	assert.ErrorIs(t, err, ErrProviderNotFound)
	_, err = env.service.getProviderByID(ctx, testID("missing"))
	assert.ErrorIs(t, err, ErrProviderNotFound)
	assert.ErrorIs(t, verifyPrincipalAndMember(ctx, env.db, testID("tenant-a"), testID("no-such-principal")), ErrPrincipalInactive)

	provider := env.createProvider(t, env.providerInput("Secret", "https://secret.example"))
	var row providerRow
	require.NoError(t, env.db.NewSelect().Model(&row).Where("id = ?", provider.ID).Scan(ctx))
	_, err = env.service.decryptSecret(row.ID, "v9."+strings.TrimPrefix(row.ClientSecretCiphertext, "v1."))
	assert.Error(t, err)
	_, err = env.service.decryptSecret(row.ID, "v1.!!bad-base64!!")
	assert.Error(t, err)
	_, err = env.service.decryptSecret(row.ID, "v1.AA")
	assert.Error(t, err)
	tampered := row.ClientSecretCiphertext[:len(row.ClientSecretCiphertext)-3] + "AAA"
	_, err = env.service.decryptSecret(row.ID, tampered)
	assert.Error(t, err)

	other := env.createProvider(t, env.providerInput("Other", "https://other.example"))
	_, err = env.service.UpdateProvider(ctx, testID("tenant-a"), other.ID, ProviderInput{
		Name: "Other", ProviderType: ProviderOIDC, Issuer: "https://secret.example", ClientID: env.idp.clientID,
		AutoProvision: true, Status: ProviderActive,
	})
	assert.ErrorIs(t, err, ErrProviderIssuerExists)
}

func TestFederationServicePanicsOnNilDependencies(t *testing.T) {
	env := newFederationEnvironment(t)
	assert.Panics(t, func() { NewService(nil, env.audit, env.identities, env.web, Config{}, env.key, newTestIDGenerator()) })
	assert.Panics(t, func() { NewService(env.db, nil, env.identities, env.web, Config{}, env.key, newTestIDGenerator()) })
	assert.Panics(t, func() { NewService(env.db, env.audit, nil, env.web, Config{}, env.key, newTestIDGenerator()) })
	assert.Panics(t, func() { NewService(env.db, env.audit, env.identities, nil, Config{}, env.key, newTestIDGenerator()) })
}

func TestFederationServiceSurfacesDatabaseErrors(t *testing.T) {
	env := newFederationEnvironment(t)
	ctx := t.Context()
	require.NoError(t, env.db.Close())
	_, err := env.service.ListProviders(ctx, testID("tenant-a"))
	assert.Error(t, err)
	_, err = env.service.CreateProvider(ctx, testID("tenant-a"), env.providerInput("X", "https://x.example"))
	assert.Error(t, err)
	_, err = env.service.UpdateProvider(ctx, testID("tenant-a"), testID("id"), env.providerInput("X", "https://x.example"))
	assert.Error(t, err)
	_, err = env.service.StartLogin(ctx, testID("id"), "https://app.example/", "https://app.example/federation/id/callback")
	assert.Error(t, err)
	_, err = env.service.CompleteLogin(ctx, testID("id"), "code", "state", "cp_federation_state=x", "https://app.example/federation/id/callback")
	assert.Error(t, err)
	assert.Error(t, env.service.DeleteProvider(ctx, testID("tenant-a"), testID("id")))
}
