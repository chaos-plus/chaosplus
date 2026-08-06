package organization

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantHTTPWorkflow(t *testing.T) {
	_, service := newTenantService(t)
	_, api := humatest.New(t)
	RegisterTenantREST(api, service, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))

	created := api.Post("/iam/tenants", map[string]any{"slug": "acme", "name": "Acme"})
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	tenant := decodeTenant(t, created.Body.Bytes())
	assert.Equal(t, http.StatusOK, api.Get("/iam/tenants/"+tenant.ID).Code)
	assert.Equal(t, http.StatusOK, api.Get("/iam/tenants").Code)

	updated := api.Patch("/iam/tenants/"+tenant.ID, map[string]any{"status": TenantSuspended, "version": tenant.Version})
	require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
	tenant = decodeTenant(t, updated.Body.Bytes())
	assert.Equal(t, TenantSuspended, tenant.Status)
	assert.Equal(t, http.StatusConflict, api.Patch("/iam/tenants/"+tenant.ID, map[string]any{"name": "Stale", "version": 1}).Code)
	assert.Equal(t, http.StatusOK, api.Delete("/iam/tenants/"+tenant.ID+"?version=2").Code)
	assert.Equal(t, http.StatusConflict, api.Delete("/iam/tenants/"+tenant.ID+"?version=2").Code)

	assert.Equal(t, http.StatusConflict, api.Post("/iam/tenants", map[string]any{"slug": "acme", "name": "Duplicate"}).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Post("/iam/tenants", map[string]any{"slug": "BAD", "name": "Invalid"}).Code)
	assert.Equal(t, http.StatusNotFound, api.Get("/iam/tenants/missing").Code)
}

func TestTenantHTTPStorageFailure(t *testing.T) {
	db, service := newTenantService(t)
	_, api := humatest.New(t)
	RegisterTenantREST(api, service, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	require.NoError(t, db.Close())
	assert.Equal(t, http.StatusInternalServerError, api.Get("/iam/tenants").Code)
}

func TestTenantErrorMapping(t *testing.T) {
	for _, item := range []struct {
		err    error
		status int
	}{
		{ErrTenantNotFound, http.StatusNotFound},
		{ErrTenantSlugConflict, http.StatusConflict},
		{ErrTenantVersionConflict, http.StatusConflict},
		{ErrInvalidTenant, http.StatusUnprocessableEntity},
		{errors.New("storage unavailable"), http.StatusInternalServerError},
	} {
		var statusError huma.StatusError
		require.True(t, errors.As(tenantError(item.err), &statusError))
		assert.Equal(t, item.status, statusError.GetStatus())
	}
}

func decodeTenant(t *testing.T, body []byte) Tenant {
	t.Helper()
	var envelope struct {
		Data Tenant `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &envelope))
	return envelope.Data
}
