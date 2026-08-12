package identity

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrincipalHTTPWorkflow(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(context.Background(), db))
	require.NoError(t, organization.Migrate(context.Background(), db))

	_, api := humatest.New(t)
	RegisterREST(api, newIdentityService(db), authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	tenant := authz.TenantHeader + ": " + wireID("tenant-a")
	created := api.Post("/iam/principals", tenant, map[string]any{
		"login_name": "alice", "password": "correct horse battery staple",
		"display_name": "Alice", "email": "alice@example.com",
	})
	require.Equal(t, http.StatusOK, created.Code, created.Body.String())
	var envelope struct {
		Data Principal `json:"data"`
	}
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &envelope))
	require.NotEmpty(t, envelope.Data.ID)
	assert.False(t, envelope.Data.EmailVerified)

	listed := api.Get("/iam/principals?search=ali", tenant)
	assert.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	assert.Equal(t, http.StatusOK, api.Get("/iam/principals/"+envelope.Data.ID.String(), tenant).Code)
	assert.Equal(t, http.StatusOK, api.Patch("/iam/principals/"+envelope.Data.ID.String(), tenant, map[string]any{"display_name": "Alice Updated"}).Code)
	assert.Equal(t, http.StatusOK, api.Post("/iam/principals/"+envelope.Data.ID.String()+"/disable", tenant).Code)
	assert.Equal(t, http.StatusOK, api.Post("/iam/principals/"+envelope.Data.ID.String()+"/restore", tenant).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Patch("/iam/principals/"+envelope.Data.ID.String(), tenant, map[string]any{}).Code)
	assert.Equal(t, http.StatusNotFound, api.Post("/iam/principals/"+wireID("missing")+"/disable", tenant).Code)
	assert.Equal(t, http.StatusNotFound, api.Post("/iam/principals/"+wireID("missing")+"/restore", tenant).Code)
	assert.Equal(t, http.StatusNotFound, api.Get("/iam/principals/"+envelope.Data.ID.String(), authz.TenantHeader+": "+wireID("tenant-b")).Code)
	assert.Equal(t, http.StatusConflict, api.Post("/iam/principals", tenant, map[string]any{
		"login_name": "alice", "password": "another secure password",
	}).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Post("/iam/principals", tenant, map[string]any{
		"login_name": "bob", "password": "short",
	}).Code)
	require.NoError(t, db.Close())
	assert.Equal(t, http.StatusInternalServerError, api.Get("/iam/principals", tenant).Code)
}

func TestPrincipalHTTPCreateReturnsInternalErrorAndRollsBackWhenAuditFails(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	require.NoError(t, organization.Migrate(t.Context(), db))
	rejectIdentityAuditEvent(t, db, "principal_created")

	_, api := humatest.New(t)
	RegisterREST(api, newIdentityService(db), authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	response := api.Post("/iam/principals", authz.TenantHeader+": "+wireID("tenant-a"), map[string]any{
		"login_name": "alice", "password": "correct horse battery staple",
		"display_name": "Alice", "email": "alice@example.test",
	})
	assert.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())
	assertIdentityCreateStateEmpty(t, db)
}

func TestPrincipalHTTPRejectsLastAdministratorDisable(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	require.NoError(t, organization.Migrate(t.Context(), db))
	service := newIdentityService(db)
	principal, err := service.Create(t.Context(), testID("tenant"), "administrator", "correct horse battery staple", "Administrator", "")
	require.NoError(t, err)
	now := time.Now().UTC().UnixMilli()
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_roles (tenant_id,id,name,description,created_at,updated_at) VALUES (?,?,?,'',?,?)`, testID("tenant"), testID("administrator"), "Administrator", now, now)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_role_permissions (tenant_id,role_id,permission_code,created_at) VALUES (?,?, 'tenant_administer',?)`, testID("tenant"), testID("administrator"), now)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_role_members (tenant_id,role_id,principal_id,created_at) VALUES (?,?,?,?)`, testID("tenant"), testID("administrator"), principal.ID, now)
	require.NoError(t, err)

	_, api := humatest.New(t)
	RegisterREST(api, service, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	response := api.Post("/iam/principals/"+principal.ID.String()+"/disable", authz.TenantHeader + ": " + wireID("tenant"))
	assert.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	assert.Contains(t, response.Body.String(), "last_tenant_administrator")
}
