package authz_test

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"hash/fnv"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	authnmod "github.com/chaos-plus/chaosplus/internal/modules/authn"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func testID(value string) guid.ID {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(value))
	id, err := guid.Parse(strconv.FormatUint(hash.Sum64()>>1, 10))
	if err != nil {
		panic(err)
	}
	return id
}

func wireID(value string) string { return strconv.FormatInt(int64(testID(value)), 10) }

func parseGUID(value string) guid.ID {
	id, _ := guid.Parse(value)
	return id
}

func guidString(value guid.ID) string { return value.String() }

func newTestIDGenerator() func() (guid.ID, error) {
	var next atomic.Int64
	return func() (guid.ID, error) { return guid.ID(next.Add(1)), nil }
}

type routeOutput struct{ Body string }

type adapterVerifier struct{}

func (adapterVerifier) Authenticate(context.Context, string, string) (*authnext.Claims, error) {
	return &authnext.Claims{Subject: "31", PrincipalID: 31}, nil
}

type adapterMembership struct{ active bool }

func (m adapterMembership) IsMemberActive(context.Context, guid.ID, guid.ID) (bool, error) {
	return m.active, nil
}

type adapterChecker struct{ allowed bool }

func (c adapterChecker) Check(context.Context, guid.ID, string, guid.ID) (bool, error) {
	return c.allowed, nil
}
func (c adapterChecker) CheckPlatform(context.Context, string, guid.ID) (bool, error) {
	return c.allowed, nil
}
func (c adapterChecker) CheckEntity(context.Context, guid.ID, guid.ID, string, guid.ID) (bool, error) {
	return c.allowed, nil
}

func TestRegisterDeclaresGuard(t *testing.T) {
	_, api := humatest.New(t)
	registry := authz.MustRegistry(authz.Action{Resource: "store", Verb: "view"})
	registrar := authz.NewDeclarationOnlyRegistrar(registry)
	authz.Register(registrar, api, huma.Operation{OperationID: "view-store", Method: http.MethodPost, Path: "/stores/query", Summary: "Query stores"}, authz.Guard{Resource: "store", Verb: "view"}, okRoute)

	op := api.OpenAPI().Paths["/stores/query"].Post
	assert.Equal(t, "store_view", op.Extensions[authz.GuardExtensionKey])
	require.Len(t, op.Security, 2)
	assert.Contains(t, op.Security[0], authz.BearerScheme)
	assert.Contains(t, op.Security[1], authz.SessionScheme)
	require.NotNil(t, api.OpenAPI().Components.SecuritySchemes[authz.BearerScheme])
	require.True(t, requiredHeader(op, authz.TenantHeader))
	assert.Contains(t, op.Errors, http.StatusUnauthorized)
	assert.Contains(t, op.Errors, http.StatusForbidden)
	assert.NoError(t, authz.ValidateOperations(api, registry))
	assert.Equal(t, http.StatusOK, api.Post("/stores/query").Code)
}

func TestRegisterEntityAdapterExecutesEntityAuthorization(t *testing.T) {
	registry := authz.MustRegistry(authz.Action{Resource: "workflow", Verb: "view", Scope: "entity"})
	newAPI := func(allowed, active bool) humatest.TestAPI {
		_, api := humatest.New(t)
		registrar := authz.NewRegistrar(registry, adapterVerifier{}, adapterChecker{allowed: allowed}, adapterMembership{active: active})
		authz.RegisterEntityAdapter(registrar, api, huma.Operation{OperationID: "workflow-events", Method: http.MethodGet, Path: "/runs/{id}/events"}, authz.Guard{Resource: "workflow", Verb: "view"}, func(ctx huma.Context) {
			claims, ok := authnext.FromContext(ctx.Context())
			if !ok || claims.TenantID != 11 || claims.EntityID != 21 || claims.PrincipalID != 31 {
				ctx.SetStatus(http.StatusInternalServerError)
				return
			}
			ctx.SetStatus(http.StatusNoContent)
		})
		return api
	}

	api := newAPI(true, true)
	operation := api.OpenAPI().Paths["/runs/{id}/events"].Get
	assert.Equal(t, "workflow_view", operation.Extensions[authz.GuardExtensionKey])
	require.True(t, requiredParameter(operation, authz.TenantQuery, "query"))
	require.True(t, requiredParameter(operation, authz.EntityQuery, "query"))
	require.NoError(t, authz.ValidateOperations(api, registry))
	assert.Equal(t, http.StatusForbidden, api.Get("/runs/41/events").Code)
	assert.Equal(t, http.StatusNoContent, api.Get("/runs/41/events?tenant_id=11&entity_id=21").Code)
	assert.Equal(t, http.StatusForbidden, newAPI(false, true).Get("/runs/41/events?tenant_id=11&entity_id=21").Code)
	assert.Equal(t, http.StatusForbidden, newAPI(true, false).Get("/runs/41/events?tenant_id=11&entity_id=21").Code)
}

func TestRegisterTenantMemberDeclaresProtectedRoute(t *testing.T) {
	_, api := humatest.New(t)
	registry := authz.MustRegistry(authz.Action{Resource: "store", Verb: "view"})
	registrar := authz.NewDeclarationOnlyRegistrar(registry)
	authz.RegisterTenantMember(registrar, api, huma.Operation{OperationID: "member-profile", Method: http.MethodGet, Path: "/member/profile", Summary: "Read member profile"}, okRoute)

	op := api.OpenAPI().Paths["/member/profile"].Get
	assert.True(t, authz.IsTenantMemberOperation(op))
	assert.False(t, authz.IsTenantMemberOperation(nil))
	require.Len(t, op.Security, 2)
	assert.Contains(t, op.Security[0], authz.BearerScheme)
	assert.Contains(t, op.Security[1], authz.SessionScheme)
	assert.NotContains(t, op.Extensions, authz.GuardExtensionKey)
	require.True(t, requiredHeader(op, authz.TenantHeader))
	assert.Contains(t, op.Errors, http.StatusUnauthorized)
	assert.Contains(t, op.Errors, http.StatusForbidden)
	assert.Contains(t, op.Errors, http.StatusServiceUnavailable)
	assert.NoError(t, authz.ValidateOperations(api, registry))
	assert.Equal(t, http.StatusOK, api.Get("/member/profile").Code)

	assert.Panics(t, func() {
		_, invalidAPI := humatest.New(t)
		authz.RegisterTenantMember(registrar, invalidAPI, huma.Operation{Method: http.MethodGet, Path: "/missing-id"}, okRoute)
	})
}

func TestRegisterPlatformDeclaresAndEnforcesPlatformRoute(t *testing.T) {
	t.Run("declaration", func(t *testing.T) {
		_, api := humatest.New(t)
		registry := authz.DefaultRegistry()
		registrar := authz.NewDeclarationOnlyRegistrar(registry)
		authz.RegisterPlatform(registrar, api, huma.Operation{OperationID: "platform-tenants", Method: http.MethodGet, Path: "/platform/tenants", Summary: "List tenants"}, authz.Guard{Resource: "tenant", Verb: "view"}, okRoute)
		op := api.OpenAPI().Paths["/platform/tenants"].Get
		assert.True(t, authz.IsPlatformOperation(op))
		assert.False(t, authz.IsPlatformOperation(nil))
		assert.False(t, requiredHeader(op, authz.TenantHeader))
		assert.Equal(t, "tenant_view", op.Extensions[authz.GuardExtensionKey])
		assert.NoError(t, authz.ValidateOperations(api, registry))
		assert.Panics(t, func() {
			authz.RegisterPlatform(registrar, api, huma.Operation{OperationID: "bad-platform", Method: http.MethodGet, Path: "/bad-platform"}, authz.Guard{Resource: "store", Verb: "view"}, okRoute)
		})
	})

	t.Run("runtime", func(t *testing.T) {
		environment := newAuthorizationEnvironment(t)
		_, api := humatest.New(t)
		authz.RegisterPlatform(environment.registrar, api, huma.Operation{OperationID: "platform-tenants", Method: http.MethodGet, Path: "/platform/tenants", Summary: "List tenants"}, authz.Guard{Resource: "tenant", Verb: "view"}, okRoute)
		authz.RegisterPlatform(environment.registrar, api, huma.Operation{OperationID: "platform-create-tenant", Method: http.MethodPost, Path: "/platform/tenants", Summary: "Create tenant"}, authz.Guard{Resource: "tenant", Verb: "create"}, okRoute)

		assert.Equal(t, http.StatusUnauthorized, api.Get("/platform/tenants").Code)
		assert.Equal(t, http.StatusForbidden, api.Get("/platform/tenants", "Cookie: "+environment.cookie).Code)
		repo := iam.NewRepository(environment.db, newTestIDGenerator())
		changed, err := repo.GrantPlatformAdministrator(t.Context(), parseGUID(environment.principalID))
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, http.StatusOK, api.Get("/platform/tenants", "Cookie: "+environment.cookie).Code)
		assert.Equal(t, http.StatusForbidden, api.Post("/platform/tenants", "Cookie: "+environment.cookie, "Origin: https://evil.example").Code)

		_, err = environment.db.ExecContext(t.Context(), "DELETE FROM iam_platform_administrators WHERE principal_id = ?", environment.principalID)
		require.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, api.Get("/platform/tenants", "Cookie: "+environment.cookie).Code)
		require.NoError(t, environment.db.Close())
		assert.Equal(t, http.StatusServiceUnavailable, api.Get("/platform/tenants", "Cookie: "+environment.cookie).Code)
	})
}

func TestRegisterEnforcesRealAuthenticationAndRBAC(t *testing.T) {
	t.Run("unauthenticated and missing tenant", func(t *testing.T) {
		environment := newAuthorizationEnvironment(t)
		_, api := humatest.New(t)
		registerRoute(api, environment.registrar)
		assert.Equal(t, http.StatusUnauthorized, api.Get("/guarded").Code)
		assert.Equal(t, http.StatusForbidden, api.Get("/guarded", "Cookie: "+environment.cookie).Code)
	})

	t.Run("permission and tenant membership", func(t *testing.T) {
		environment := newAuthorizationEnvironment(t)
		_, api := humatest.New(t)
		registerRoute(api, environment.registrar)
		assert.Equal(t, http.StatusOK, api.Get("/guarded", "Cookie: "+environment.cookie, authz.TenantHeader+": "+wireID("tenant-a")).Code)
		assert.Equal(t, http.StatusForbidden, api.Get("/guarded", "Cookie: "+environment.cookie, authz.TenantHeader+": "+wireID("tenant-b")).Code)
		_, err := environment.db.ExecContext(context.Background(), "UPDATE iam_tenant_members SET status = 'disabled' WHERE tenant_id = ? AND principal_id = ?", testID("tenant-a"), parseGUID(environment.principalID))
		require.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, api.Get("/guarded", "Cookie: "+environment.cookie, authz.TenantHeader+": "+wireID("tenant-a")).Code)
	})

	t.Run("csrf and unavailable database", func(t *testing.T) {
		environment := newAuthorizationEnvironment(t)
		_, api := humatest.New(t)
		registerRoute(api, environment.registrar)
		assert.Equal(t, http.StatusForbidden, api.Post("/guarded", "Cookie: "+environment.cookie, "Origin: https://evil.example", authz.TenantHeader+": "+wireID("tenant-a")).Code)
		require.NoError(t, environment.db.Close())
		assert.Equal(t, http.StatusServiceUnavailable, api.Get("/guarded", "Cookie: "+environment.cookie, authz.TenantHeader+": "+wireID("tenant-a")).Code)
	})
}

func TestRegisterTenantMemberEnforcesAuthenticationAndMembership(t *testing.T) {
	environment := newAuthorizationEnvironment(t)
	_, api := humatest.New(t)
	registerMemberRoutes(api, environment.registrar)

	assert.Equal(t, http.StatusUnauthorized, api.Get("/member", authz.TenantHeader+": "+wireID("tenant-b")).Code)
	assert.Equal(t, http.StatusForbidden, api.Get("/member", "Cookie: "+environment.cookie).Code)
	assert.Equal(t, http.StatusOK, api.Get("/member", "Cookie: "+environment.cookie, authz.TenantHeader+": "+wireID("tenant-b")).Code)
	assert.Equal(t, http.StatusForbidden, api.Post("/member", "Cookie: "+environment.cookie, "Origin: https://evil.example", authz.TenantHeader+": "+wireID("tenant-b")).Code)

	_, err := environment.db.ExecContext(context.Background(), "UPDATE iam_tenant_members SET status = 'disabled' WHERE tenant_id = ? AND principal_id = ?", testID("tenant-b"), parseGUID(environment.principalID))
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, api.Get("/member", "Cookie: "+environment.cookie, authz.TenantHeader+": "+wireID("tenant-b")).Code)

	require.NoError(t, environment.db.Close())
	assert.Equal(t, http.StatusServiceUnavailable, api.Get("/member", "Cookie: "+environment.cookie, authz.TenantHeader+": "+wireID("tenant-a")).Code)
}

func TestOperationGateAndRegistrarValidation(t *testing.T) {
	registry := authz.MustRegistry(authz.Action{Resource: "store", Verb: "update"})
	_, api := humatest.New(t)
	huma.Register(api, huma.Operation{OperationID: "view-store", Method: http.MethodGet, Path: "/stores/{id}"}, okRoute)
	err := authz.ValidateOperations(api, registry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GET /stores/{id}")

	_, publicAPI := humatest.New(t)
	authz.RegisterPublic(publicAPI, huma.Operation{OperationID: "oidc-callback", Method: http.MethodPost, Path: "/oidc/callback", Summary: "OIDC callback"}, okRoute)
	assert.NoError(t, authz.ValidateOperations(publicAPI, registry))
	assert.Panics(t, func() { authz.NewDeclarationOnlyRegistrar(nil) })
	assert.Panics(t, func() { authz.NewRegistrar(nil, nil, nil, nil) })
	assert.Panics(t, func() {
		_, invalidAPI := humatest.New(t)
		authz.Register(authz.NewDeclarationOnlyRegistrar(registry), invalidAPI, huma.Operation{OperationID: "bad", Method: http.MethodGet, Path: "/bad"}, authz.Guard{Resource: "missing", Verb: "view"}, okRoute)
	})
}

func TestRegistrarAndSecurityMetadata(t *testing.T) {
	registry := authz.MustRegistry(authz.Action{Resource: "store", Verb: "view"})
	declarations := authz.NewDeclarationOnlyRegistrar(registry)
	assert.Same(t, registry, declarations.Registry())
	assert.True(t, declarations.IsDeclarationOnly())
	assert.Len(t, authz.UserSecurity(), 2)
	assert.Len(t, authz.BearerSecurity(), 1)
	assert.Len(t, authz.ClientSecurity(), 1)

	_, api := humatest.New(t)
	authz.EnsureSecuritySchemes(api, "custom_session")
	assert.Equal(t, "custom_session", api.OpenAPI().Components.SecuritySchemes[authz.SessionScheme].Name)
	assert.Panics(t, func() {
		authz.Register(declarations, api, huma.Operation{Method: http.MethodGet, Path: "/missing-id"}, authz.Guard{Resource: "store", Verb: "view"}, okRoute)
	})
	assert.Panics(t, func() { authz.NewRegistrar(registry, nil, nil, nil) })
}

type authorizationEnvironment struct {
	db          *bun.DB
	registrar   *authz.Registrar
	principalID string
	cookie      string
}

func newAuthorizationEnvironment(t *testing.T) authorizationEnvironment {
	t.Helper()
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(context.Background(), db))
	require.NoError(t, organization.Migrate(context.Background(), db))
	seed := make([]byte, ed25519.SeedSize)
	web, err := authnmod.NewWebService(authnext.Config{
		Enabled: true, Issuer: "https://iam.example", Audience: []string{"api"}, SigningKey: base64.RawStdEncoding.EncodeToString(seed),
		MFA: authnext.MFAConfig{EncryptionKey: base64.RawStdEncoding.EncodeToString(seed)},
		Web: authnext.WebConfig{
			Enabled: true, CookieName: "session", SessionTTL: time.Hour, IdleTTL: time.Minute,
			PostLoginURL: "https://app.example/", AllowedReturnURLs: []string{"https://app.example/"}, AllowedOrigins: []string{"https://app.example"},
		},
	}, db, authnmod.WithIDGenerator(newTestIDGenerator()))
	require.NoError(t, err)
	principalID, err := authnmod.EnsureBootstrapPrincipal(context.Background(), db, authnmod.BootstrapPrincipal{LoginName: "alice", Password: "correct horse battery staple"}, newTestIDGenerator())
	require.NoError(t, err)
	now := time.Now().UTC().UnixMilli()
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		require.NoError(t, organization.EnsureTenant(context.Background(), db, testID(tenant)))
		_, err = db.ExecContext(context.Background(), `INSERT INTO iam_tenant_members
 (tenant_id,principal_id,display_name,email,status,created_at,updated_at,disabled_at) VALUES (?,?,?,'','active',?,?,0)`, testID(tenant), principalID, "Alice", now, now)
		require.NoError(t, err)
	}
	_, err = db.ExecContext(context.Background(), "INSERT INTO iam_roles (tenant_id,id,name,description,created_at,updated_at) VALUES (?,?,?,?,?,?)", testID("tenant-a"), testID("viewer"), "Viewer", "", now, now)
	require.NoError(t, err)
	_, err = db.ExecContext(context.Background(), "INSERT INTO iam_role_permissions (tenant_id,role_id,permission_code,created_at) VALUES (?,?,?,?)", testID("tenant-a"), testID("viewer"), "store_view", now)
	require.NoError(t, err)
	_, err = db.ExecContext(context.Background(), "INSERT INTO iam_role_members (tenant_id,role_id,principal_id,created_at) VALUES (?,?,?,?)", testID("tenant-a"), testID("viewer"), principalID, now)
	require.NoError(t, err)
	session, _, err := web.Login(context.Background(), "alice", "correct horse battery staple", "")
	require.NoError(t, err)
	registry := authz.DefaultRegistry()
	return authorizationEnvironment{
		db: db, registrar: authz.NewRegistrar(registry, web, iam.NewAuthorizer(db), iam.NewMembershipChecker(db)),
		principalID: guidString(principalID), cookie: web.SessionCookieName() + "=" + session,
	}
}

func registerRoute(api huma.API, registrar *authz.Registrar) {
	authz.Register(registrar, api, huma.Operation{OperationID: "guarded-get", Method: http.MethodGet, Path: "/guarded", Summary: "Guarded read"}, authz.Guard{Resource: "store", Verb: "view"}, okRoute)
	authz.Register(registrar, api, huma.Operation{OperationID: "guarded-post", Method: http.MethodPost, Path: "/guarded", Summary: "Guarded write"}, authz.Guard{Resource: "store", Verb: "view"}, okRoute)
}

func registerMemberRoutes(api huma.API, registrar *authz.Registrar) {
	authz.RegisterTenantMember(registrar, api, huma.Operation{OperationID: "member-get", Method: http.MethodGet, Path: "/member", Summary: "Member read"}, okRoute)
	authz.RegisterTenantMember(registrar, api, huma.Operation{OperationID: "member-post", Method: http.MethodPost, Path: "/member", Summary: "Member write"}, okRoute)
}

func okRoute(context.Context, *struct{}) (*routeOutput, error) { return &routeOutput{Body: "ok"}, nil }

func requiredHeader(operation *huma.Operation, name string) bool {
	return requiredParameter(operation, name, "header")
}

func requiredParameter(operation *huma.Operation, name, location string) bool {
	for _, parameter := range operation.Parameters {
		if parameter.In == location && parameter.Name == name {
			return parameter.Required
		}
	}
	return false
}
