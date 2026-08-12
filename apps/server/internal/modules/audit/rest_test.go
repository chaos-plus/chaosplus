package audit

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditHTTPQueryAndIntegrity(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	service := NewService(db, newTestIDGenerator())
	event, err := service.Append(t.Context(), EventInput{TenantID: testID("tenant"), EventType: "client_created", TargetType: "oauth_client", TargetID: testID("client"), Outcome: "success"})
	require.NoError(t, err)
	_, api := humatest.New(t)
	RegisterREST(api, service, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	header := authz.TenantHeader + ": " + wireID("tenant")

	response := api.Get("/iam/audit-events?event_type=client_created&limit=25", header)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var list struct {
		Data []Event `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &list))
	require.Len(t, list.Data, 1)
	assert.Equal(t, event.ID, list.Data[0].ID)
	assert.Equal(t, http.StatusOK, api.Get("/iam/audit-events/"+event.ID.String(), header).Code)
	assert.Equal(t, http.StatusOK, api.Get("/iam/audit-integrity", header).Code)
	assert.Equal(t, http.StatusNotFound, api.Get("/iam/audit-events/"+wireID("missing"), header).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Get("/iam/audit-events?from=invalid", header).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Get("/iam/audit-events?to=invalid", header).Code)
	exported := api.Get("/iam/audit-events/export?event_type=client_created", header)
	require.Equal(t, http.StatusOK, exported.Code, exported.Body.String())
	assert.Equal(t, "application/x-ndjson", exported.Header().Get("Content-Type"))
	assert.Contains(t, exported.Header().Get("Content-Disposition"), "audit-")
	assert.Equal(t, "no-store", exported.Header().Get("Cache-Control"))
	lines := strings.Split(strings.TrimSpace(exported.Body.String()), "\n")
	require.Len(t, lines, 3)
	var complete exportComplete
	require.NoError(t, json.Unmarshal([]byte(lines[2]), &complete))
	assert.Equal(t, int64(1), complete.ExportedEvents)
}

func TestAuditExportUsesRealHTTPListener(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	service := NewService(db, newTestIDGenerator())
	_, err = service.Append(t.Context(), EventInput{TenantID: testID("tenant"), EventType: "client_created", Outcome: "success"})
	require.NoError(t, err)
	router := chi.NewMux()
	api := humachi.New(router, huma.DefaultConfig("Chaosplus API", "test"))
	RegisterREST(api, service, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/iam/audit-events/export", nil)
	require.NoError(t, err)
	request.Header.Set(authz.TenantHeader, wireID("tenant"))
	response, err := server.Client().Do(request)
	require.NoError(t, err)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusOK, response.StatusCode, string(body))
	assert.Equal(t, "application/x-ndjson", response.Header.Get("Content-Type"))
	assert.Contains(t, string(body), `"schema":"audit-export.v1"`)
	assert.Contains(t, string(body), `"type":"complete"`)
}

func TestAuditGovernanceHTTPEndpoints(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	service := NewService(db, newTestIDGenerator())
	_, err = service.Append(t.Context(), EventInput{TenantID: testID("tenant"), EventType: "created", Outcome: "success"})
	require.NoError(t, err)
	_, api := humatest.New(t)
	RegisterREST(api, service, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	header := authz.TenantHeader + ": " + wireID("tenant")

	response := api.Get("/iam/audit/governance", header)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Contains(t, response.Body.String(), `"min_days":365`)
	assert.Contains(t, response.Body.String(), `"total_events":1`)
	assert.Contains(t, response.Body.String(), `"anchored_events":0`)

	put := api.Put("/iam/audit/retention", header, map[string]any{"min_days": 90, "archive_after_days": 180})
	require.Equal(t, http.StatusOK, put.Code, put.Body.String())
	assert.Contains(t, put.Body.String(), `"min_days":90`)
	assert.Contains(t, put.Body.String(), `"archive_after_days":180`)

	assert.Equal(t, http.StatusUnprocessableEntity, api.Put("/iam/audit/retention", header, map[string]any{"min_days": 365, "archive_after_days": 30}).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Put("/iam/audit/retention", header, map[string]any{"min_days": 0, "archive_after_days": 10}).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Put("/iam/audit/retention", header, map[string]any{"min_days": 90, "archive_after_days": 89}).Code)

	response = api.Get("/iam/audit/governance", header)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Contains(t, response.Body.String(), `"min_days":90`)

	assert.Equal(t, http.StatusServiceUnavailable, api.Post("/iam/audit/roots/sign", header).Code, "root signing without anchoring must fail closed")
}

func TestAuditRootSigningHTTPWithRealMinIO(t *testing.T) {
	endpoint, _ := startTestMinIO(t)
	store, err := NewAnchorStore(AnchorConfig{
		Enabled: true, Endpoint: endpoint, Bucket: "audit-http-signed",
		AccessKey: minioTestUser, SecretKey: minioTestPassword, RetentionDays: 30,
	})
	require.NoError(t, err)
	signer, err := NewRootSigner(testSignerSeed(t))
	require.NoError(t, err)
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	service := NewServiceWithAnchorAndSigner(db, store, signer, newTestIDGenerator())
	_, err = service.Append(t.Context(), EventInput{TenantID: testID("tenant"), EventType: "created", Outcome: "success"})
	require.NoError(t, err)
	_, api := humatest.New(t)
	RegisterREST(api, service, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	header := authz.TenantHeader + ": " + wireID("tenant")

	response := api.Post("/iam/audit/roots/sign", header)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var body struct {
		Data Anchor `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	assert.NotEmpty(t, body.Data.RootSignature)
	assert.NotEmpty(t, body.Data.RootPublicKey)
	assert.NotEmpty(t, body.Data.SigningKeyID)
	assert.True(t, VerifyRootSignature(body.Data))
	require.Equal(t, http.StatusOK, api.Post("/iam/audit/roots/sign", header).Code, "re-signing an already signed root is idempotent")

	unsignedStore, err := NewAnchorStore(AnchorConfig{
		Enabled: true, Endpoint: endpoint, Bucket: "audit-http-unsigned",
		AccessKey: minioTestUser, SecretKey: minioTestPassword, RetentionDays: 30,
	})
	require.NoError(t, err)
	unsignedService := NewServiceWithAnchor(db, unsignedStore, newTestIDGenerator())
	_, unsignedAPI := humatest.New(t)
	RegisterREST(unsignedAPI, unsignedService, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	unsignedResponse := unsignedAPI.Post("/iam/audit/roots/sign", header)
	assert.Equal(t, http.StatusUnprocessableEntity, unsignedResponse.Code, unsignedResponse.Body.String())
	assert.Contains(t, unsignedResponse.Body.String(), "audit_root_signing_not_enabled")
}

func TestAuditExportRejectsInvalidRangeAndBrokenChain(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	service := NewService(db, newTestIDGenerator())
	event, err := service.Append(t.Context(), EventInput{TenantID: testID("tenant"), EventType: "created", Outcome: "success"})
	require.NoError(t, err)
	_, api := humatest.New(t)
	RegisterREST(api, service, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	header := authz.TenantHeader + ": " + wireID("tenant")
	assert.Equal(t, http.StatusUnprocessableEntity, api.Get("/iam/audit-events/export?from=2026-08-02T00:00:00Z&to=2026-08-01T00:00:00Z", header).Code)

	_, err = db.ExecContext(t.Context(), `DROP TRIGGER trg_iam_audit_events_no_update`)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `UPDATE iam_audit_events SET event_hash = 'tampered' WHERE id = ?`, event.ID)
	require.NoError(t, err)
	broken := api.Get("/iam/audit-events/export", header)
	assert.Equal(t, http.StatusConflict, broken.Code, broken.Body.String())
	assert.Contains(t, broken.Body.String(), "audit_integrity_failed")
}

func TestAuditHTTPFailsClosedWhenDatabaseUnavailable(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	require.NoError(t, iam.Migrate(t.Context(), db))
	_, api := humatest.New(t)
	RegisterREST(api, NewService(db, newTestIDGenerator()), authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	require.NoError(t, db.Close())
	header := authz.TenantHeader + ": " + wireID("tenant")
	assert.Equal(t, http.StatusInternalServerError, api.Get("/iam/audit-events", header).Code)
	assert.Equal(t, http.StatusInternalServerError, api.Get("/iam/audit-events/export", header).Code)
	assert.Equal(t, http.StatusInternalServerError, api.Get("/iam/audit-events/"+wireID("event"), header).Code)
	assert.Equal(t, http.StatusInternalServerError, api.Get("/iam/audit-integrity", header).Code)
}

func TestAuditOpenAPIContract(t *testing.T) {
	_, api := humatest.New(t)
	RegisterREST(api, nil, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	for _, path := range []string{"/iam/audit-events", "/iam/audit-events/export", "/iam/audit-events/{id}", "/iam/audit-integrity"} {
		require.NotNil(t, api.OpenAPI().Paths[path].Get)
		assert.Equal(t, []string{"audit"}, api.OpenAPI().Paths[path].Get.Tags)
	}
	export := api.OpenAPI().Paths["/iam/audit-events/export"].Get
	assert.Equal(t, "audit-export-events", export.OperationID)
	assert.Equal(t, "audit_event_export", export.Extensions[authz.GuardExtensionKey])
	assert.Contains(t, export.Responses["200"].Content, "application/x-ndjson")
	assert.Contains(t, export.Responses, "409")
	assert.Contains(t, export.Responses, "500")
	_, documentsNotFound := api.OpenAPI().Paths["/iam/audit-events/{id}"].Get.Responses["404"]
	assert.True(t, documentsNotFound)
}
