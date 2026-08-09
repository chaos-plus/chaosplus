package app

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	authnmod "github.com/chaos-plus/chaosplus/internal/modules/authn"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
)

func TestRESTOperationsDeclareAuthorization(t *testing.T) {
	registry := authz.DefaultRegistry()
	app := &App{
		cfg:            Config{Authn: authn.Config{Enabled: true}},
		dbr:            bunx.DatasourceRouter{Writer: []*bun.DB{nil}},
		authnRequest:   &authn.Verifier{},
		authzRegistrar: authz.NewDeclarationOnlyRegistrar(registry),
	}
	app.mods = app.buildModules()
	_, api := humatest.New(t)
	app.registerREST(api)
	require.NoError(t, authz.ValidateOperations(api, registry))
	require.Equal(t, []string{
		"authn-me",
		"federation-callback-login",
		"federation-callback-saml-login",
		"federation-saml-metadata",
		"federation-saml-slo",
		"federation-saml-slo-post",
		"federation-saml-sso",
		"federation-saml-sso-post",
		"federation-start-login",
		"iam-accept-entity-invite",
		"iam-lookup-entity-invite",
		"lookup-geoip",
		"lookup-geoip-self",
		"next-guid",
		"next-guid-batch",
		"organization-accept-invitation",
		"organization-my-tenants",
		"scim-bulk",
		"scim-create-group",
		"scim-create-user",
		"scim-delete-group",
		"scim-delete-user",
		"scim-get-group",
		"scim-get-resource-type",
		"scim-get-schema",
		"scim-get-user",
		"scim-list-groups",
		"scim-list-resource-types",
		"scim-list-schemas",
		"scim-list-users",
		"scim-patch-group",
		"scim-patch-user",
		"scim-replace-group",
		"scim-replace-user",
		"scim-service-provider-config",
	}, unguardedOperationIDs(api))
	require.Equal(t, []string{
		"governance-create-access-request",
		"governance-list-my-access-requests",
		"governance-list-requestable-roles",
		"governance-withdraw-access-request",
		"iam-effective-menus",
	}, tenantMemberOperationIDs(api))
}

func TestOpenAPIQualityContract(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(context.Background(), db))
	seed := make([]byte, ed25519.SeedSize)
	web, err := authnmod.NewWebService(authn.Config{
		Enabled:    true,
		Issuer:     "https://iam.example",
		Audience:   []string{"api"},
		SigningKey: base64.RawStdEncoding.EncodeToString(seed),
		MFA:        authn.MFAConfig{EncryptionKey: base64.RawStdEncoding.EncodeToString(seed)},
		Web:        authn.WebConfig{Enabled: true, CookieName: "session", SessionTTL: time.Hour, IdleTTL: 10 * time.Minute},
	}, db)
	require.NoError(t, err)
	registry := authz.DefaultRegistry()
	registrar := authz.NewRegistrar(registry, web, iam.NewAuthorizer(db), iam.NewMembershipChecker(db))
	app := &App{
		cfg:            Config{Authn: authn.Config{Enabled: true}},
		dbr:            bunx.DatasourceRouter{Writer: []*bun.DB{db}},
		authnRequest:   web,
		authnWeb:       web,
		authzRegistrar: registrar,
	}
	app.mods = app.buildModules()
	_, api := humatest.New(t)
	app.registerREST(api)
	require.NoError(t, authz.ValidateOperations(api, registry))

	seen := map[string]string{}
	for path, item := range api.OpenAPI().Paths {
		for method, op := range operations(item) {
			where := method + " " + path
			require.NotEmpty(t, op.OperationID, where)
			require.NotEmpty(t, op.Summary, op.OperationID)
			require.NotEmpty(t, op.Tags, op.OperationID)
			require.NotEmpty(t, op.Responses, op.OperationID)
			require.True(t, successfulResponse(op), "%s has no 2xx or 3xx response", op.OperationID)
			for status, response := range op.Responses {
				require.NotNil(t, response, "%s response %s", op.OperationID, status)
				require.NotEmpty(t, response.Description, "%s response %s", op.OperationID, status)
			}
			if previous, exists := seen[op.OperationID]; exists {
				t.Fatalf("duplicate operation ID %q at %s and %s", op.OperationID, previous, where)
			}
			seen[op.OperationID] = where
			_, guarded := op.Extensions[authz.GuardExtensionKey]
			if guarded || authz.IsTenantMemberOperation(op) {
				require.NotEmpty(t, op.Security, op.OperationID)
				if authz.IsPlatformOperation(op) {
					require.False(t, requiredHeader(op, authz.TenantHeader), op.OperationID)
				} else {
					require.True(t, requiredHeader(op, authz.TenantHeader), op.OperationID)
				}
			}
		}
	}
	assertEntityOpenAPIContract(t, api)
}

func assertEntityOpenAPIContract(t *testing.T, api huma.API) {
	t.Helper()
	tests := []struct {
		method, path, operationID, summary, permission string
		statuses                                       []string
	}{
		{http.MethodGet, "/iam/entities", "iam-list-entities", "List tenant entities", "entity_view", []string{"200", "401", "403", "422", "500", "503"}},
		{http.MethodPost, "/iam/entities", "iam-create-entity", "Create a tenant entity", "entity_create", []string{"201", "401", "403", "404", "409", "422", "500", "503"}},
		{http.MethodGet, "/iam/entities/{entity_id}", "iam-get-entity", "Get a tenant entity", "entity_view", []string{"200", "401", "403", "404", "422", "500", "503"}},
		{http.MethodPatch, "/iam/entities/{entity_id}", "iam-update-entity", "Update a tenant entity", "entity_update", []string{"200", "401", "403", "404", "409", "422", "500", "503"}},
		{http.MethodDelete, "/iam/entities/{entity_id}", "iam-delete-entity", "Delete an empty tenant entity", "entity_delete", []string{"200", "401", "403", "404", "409", "422", "500", "503"}},
		{http.MethodGet, "/iam/entities/{entity_id}/role-bindings", "iam-list-entity-role-bindings", "List direct principal role bindings for an entity", "entity_view", []string{"200", "401", "403", "404", "422", "500", "503"}},
		{http.MethodPut, "/iam/entities/{entity_id}/role-bindings/{role_id}/{principal_id}", "iam-put-entity-role-binding", "Assign a direct principal role at an entity scope", "entity_manage_binding", []string{"200", "401", "403", "404", "409", "422", "500", "503"}},
		{http.MethodDelete, "/iam/entities/{entity_id}/role-bindings/{role_id}/{principal_id}", "iam-delete-entity-role-binding", "Remove a direct principal role from an entity scope", "entity_manage_binding", []string{"200", "401", "403", "404", "422", "500", "503"}},
		{http.MethodPost, "/iam/authorization/constraints", "iam-authorization-constraint", "Compute an entity data constraint for a tenant subject", "role_view", []string{"200", "401", "403", "422", "500", "503"}},
		{http.MethodPost, "/iam/authorization/explain", "iam-explain-authorization", "Explain an entity or business-resource authorization decision", "role_view", []string{"200", "401", "403", "404", "422", "500", "503"}},
		{http.MethodGet, "/iam/relationships", "iam-list-relationships", "List tenant entity or business-resource relationship grants", "entity_view", []string{"200", "401", "403", "422", "500", "503"}},
		{http.MethodPost, "/iam/relationships", "iam-put-relationship", "Create or update a constrained relationship grant", "entity_manage_binding", []string{"200", "401", "403", "404", "409", "422", "500", "503"}},
		{http.MethodDelete, "/iam/relationships", "iam-delete-relationship", "Revoke a relationship grant immediately", "entity_manage_binding", []string{"200", "401", "403", "422", "500", "503"}},
		{http.MethodPost, "/iam/authorization/check", "iam-check-authorization", "Check an entity or business-resource authorization decision", "role_view", []string{"200", "401", "403", "404", "422", "500", "503"}},
	}
	for _, test := range tests {
		t.Run(test.operationID, func(t *testing.T) {
			item := api.OpenAPI().Paths[test.path]
			require.NotNil(t, item)
			op := operations(item)[test.method]
			require.NotNil(t, op)
			assert.Equal(t, test.operationID, op.OperationID)
			assert.Equal(t, test.summary, op.Summary)
			assert.Equal(t, []string{"iam"}, op.Tags)
			assert.Equal(t, authz.UserSecurity(), op.Security)
			assert.True(t, requiredHeader(op, authz.TenantHeader))
			assert.Equal(t, test.permission, op.Extensions[authz.GuardExtensionKey])
			statuses := make([]string, 0, len(op.Responses))
			for status := range op.Responses {
				statuses = append(statuses, status)
			}
			assert.ElementsMatch(t, test.statuses, statuses)
		})
	}
	requestSchema := api.OpenAPI().Paths["/iam/relationships"].Post.RequestBody.Content["application/json"].Schema
	if requestSchema.Ref != "" {
		requestSchema = api.OpenAPI().Components.Schemas.SchemaFromRef(requestSchema.Ref)
	}
	require.NotNil(t, requestSchema)
	conditionSchema := requestSchema.Properties["condition"]
	require.NotNil(t, conditionSchema)
	assert.Equal(t, "object", conditionSchema.Type)
}

func successfulResponse(operation *huma.Operation) bool {
	for status := range operation.Responses {
		if len(status) == 3 && (status[0] == '2' || status[0] == '3') {
			return true
		}
	}
	return false
}

func operations(item *huma.PathItem) map[string]*huma.Operation {
	result := map[string]*huma.Operation{}
	for method, operation := range map[string]*huma.Operation{
		http.MethodGet: item.Get, http.MethodPost: item.Post, http.MethodPut: item.Put,
		http.MethodPatch: item.Patch, http.MethodDelete: item.Delete,
	} {
		if operation != nil {
			result[method] = operation
		}
	}
	return result
}

func requiredHeader(operation *huma.Operation, name string) bool {
	for _, parameter := range operation.Parameters {
		if parameter.In == "header" && parameter.Name == name {
			return parameter.Required
		}
	}
	return false
}

func unguardedOperationIDs(api huma.API) []string {
	var ids []string
	for _, item := range api.OpenAPI().Paths {
		for _, op := range []*huma.Operation{
			item.Get, item.Post, item.Put, item.Patch, item.Delete,
			item.Options, item.Head, item.Trace,
		} {
			if op == nil {
				continue
			}
			if _, guarded := op.Extensions[authz.GuardExtensionKey]; !guarded && !authz.IsTenantMemberOperation(op) {
				ids = append(ids, op.OperationID)
			}
		}
	}
	sort.Strings(ids)
	return ids
}

func tenantMemberOperationIDs(api huma.API) []string {
	var ids []string
	for _, item := range api.OpenAPI().Paths {
		for _, op := range []*huma.Operation{
			item.Get, item.Post, item.Put, item.Patch, item.Delete,
			item.Options, item.Head, item.Trace,
		} {
			if authz.IsTenantMemberOperation(op) {
				ids = append(ids, op.OperationID)
			}
		}
	}
	sort.Strings(ids)
	return ids
}

func TestStartRestServer_SuccessThenShutdown(t *testing.T) {
	a := withCtx(NewApp(Config{Name: "test", RestServer: RestServer{Host: "127.0.0.1", Port: 0}}))
	require.NoError(t, a.StartRestServer())
	require.NotNil(t, a.rest)
	require.NoError(t, a.shutdown())
}

func TestTrustedProxyClientIP(t *testing.T) {
	middleware, err := trustedProxyClientIP([]string{"10.0.0.0/8"})
	require.NoError(t, err)
	handler := middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(request.RemoteAddr))
	}))

	tests := []struct {
		name, remote, forwarded, want string
	}{
		{"direct spoof ignored", "198.51.100.10:1234", "192.0.2.1", "198.51.100.10:1234"},
		{"trusted proxy", "10.0.0.2:1234", "192.0.2.1", "192.0.2.1"},
		{"spoof before client ignored", "10.0.0.2:1234", "198.51.100.99, 192.0.2.1", "192.0.2.1"},
		{"trusted proxy chain", "10.0.0.2:1234", "192.0.2.1, 10.0.0.3", "192.0.2.1"},
		{"invalid chain fails closed", "10.0.0.2:1234", "invalid", "10.0.0.2:1234"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.RemoteAddr = test.remote
			request.Header.Set("X-Forwarded-For", test.forwarded)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			assert.Equal(t, test.want, recorder.Body.String())
		})
	}

	_, err = trustedProxyClientIP([]string{"invalid"})
	assert.ErrorContains(t, err, "trusted proxy")
}

func buildRouter(cfg Config) chi.Router {
	app := &App{cfg: cfg}
	r := chi.NewMux()
	app.useSecurity(r)
	app.useCors(r)
	r.Get("/", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	return r
}

func TestUseSecurity_SendsHeadersWhenEnabled(t *testing.T) {
	r := buildRouter(Config{Security: Security{Enabled: true, HSTS: true}})
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, "nosniff", rr.Header().Get("X-Content-Type-Options"))
	assert.Contains(t, rr.Header().Get("Strict-Transport-Security"), "max-age=")
}

func TestUseSecurity_DisabledSendsNothing(t *testing.T) {
	r := buildRouter(Config{Security: Security{Enabled: false}})
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Empty(t, rr.Header().Get("X-Content-Type-Options"))
}

func TestUseCors_PreflightAllowsOrigin(t *testing.T) {
	r := buildRouter(Config{Cors: Cors{Enabled: true}})
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	assert.NotEmpty(t, rr.Header().Get("Access-Control-Allow-Origin"))
}

func TestUseCors_DisabledNoHeaders(t *testing.T) {
	r := buildRouter(Config{Cors: Cors{Enabled: false}})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	assert.Empty(t, rr.Header().Get("Access-Control-Allow-Origin"))
}

func TestUseRateLimitConfigurationAndUnavailableRedis(t *testing.T) {
	client := redis.NewClient(&redis.Options{
		Addr: "127.0.0.1:1", DialTimeout: 10 * time.Millisecond,
		ReadTimeout: 10 * time.Millisecond, WriteTimeout: 10 * time.Millisecond,
	})
	t.Cleanup(func() { _ = client.Close() })
	application := &App{
		cfg: Config{RateLimit: RateLimit{
			Enabled: true, Prefix: "test",
			IP:      RateRule{Enabled: true, Rate: 5, Period: time.Minute},
			Account: AccountRule{Enabled: true, Rate: 3, Period: time.Minute, Header: "X-Account-Id"},
		}},
		redis: client,
	}
	router := chi.NewMux()
	application.useRateLimit(router)
	router.Get("/", func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNoContent) })
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("X-Account-Id", "account")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusNoContent, recorder.Code)

	withoutRedis := chi.NewMux()
	(&App{cfg: Config{RateLimit: RateLimit{Enabled: true}}}).useRateLimit(withoutRedis)
	noDimensions := chi.NewMux()
	(&App{cfg: Config{RateLimit: RateLimit{Enabled: true}}, redis: client}).useRateLimit(noDimensions)
}
