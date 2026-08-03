package organization

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDepartmentHTTPWorkflow(t *testing.T) {
	_, service := newOrganizationService(t)
	_, api := humatest.New(t)
	RegisterREST(api, service, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	tenant := authz.TenantHeader + ": tenant-a"

	created := api.Post("/iam/departments", tenant, map[string]any{"name": "Engineering", "sort_order": 10})
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	root := decodeDepartment(t, created.Body.Bytes())
	childResponse := api.Post("/iam/departments", tenant, map[string]any{"parent_id": root.ID, "name": "Platform"})
	require.Equal(t, http.StatusCreated, childResponse.Code, childResponse.Body.String())
	child := decodeDepartment(t, childResponse.Body.Bytes())
	assert.Equal(t, 1, child.Depth)

	listed := api.Get("/iam/departments", tenant)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	var listEnvelope struct {
		Data []Department `json:"data"`
	}
	require.NoError(t, json.Unmarshal(listed.Body.Bytes(), &listEnvelope))
	assert.Len(t, listEnvelope.Data, 2)
	assert.Equal(t, http.StatusOK, api.Get("/iam/departments/"+child.ID, tenant).Code)

	updated := api.Patch("/iam/departments/"+child.ID, tenant, map[string]any{
		"name": "Platform Engineering", "status": "disabled", "version": child.Version,
	})
	require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
	child = decodeDepartment(t, updated.Body.Bytes())
	assert.Equal(t, StatusDisabled, child.Status)
	assert.Equal(t, http.StatusConflict, api.Patch("/iam/departments/"+child.ID, tenant, map[string]any{"name": "Stale", "version": 1}).Code)
	assert.Equal(t, http.StatusConflict, api.Delete("/iam/departments/"+root.ID+"?version=1", tenant).Code)
	assert.Equal(t, http.StatusOK, api.Delete("/iam/departments/"+child.ID+"?version=2", tenant).Code)
	assert.Equal(t, http.StatusOK, api.Delete("/iam/departments/"+root.ID+"?version=1", tenant).Code)
	assert.Equal(t, http.StatusNotFound, api.Get("/iam/departments/"+root.ID, tenant).Code)

	assert.Equal(t, http.StatusNotFound, api.Post("/iam/departments", tenant, map[string]any{"parent_id": "missing", "name": "Unknown"}).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Post("/iam/departments", tenant, map[string]any{"name": ""}).Code)
	assert.Equal(t, http.StatusNotFound, api.Get("/iam/departments/"+child.ID, authz.TenantHeader+": tenant-b").Code)
}

func TestOrganizationErrorMapping(t *testing.T) {
	cases := []struct {
		err    error
		status int
	}{
		{ErrNotFound, http.StatusNotFound},
		{ErrNameConflict, http.StatusConflict},
		{ErrHierarchyCycle, http.StatusConflict},
		{ErrHasChildren, http.StatusConflict},
		{ErrVersionConflict, http.StatusConflict},
		{ErrInUse, http.StatusConflict},
		{ErrInvalid, http.StatusUnprocessableEntity},
		{ErrHierarchyCorrupt, http.StatusInternalServerError},
	}
	for _, item := range cases {
		statusError, ok := organizationError(item.err).(huma.StatusError)
		require.True(t, ok)
		assert.Equal(t, item.status, statusError.GetStatus())
	}
}

func TestDepartmentHTTPEmptyCollectionIsArrayAndErrorsAreStable(t *testing.T) {
	db, service := newOrganizationService(t)
	_, api := humatest.New(t)
	RegisterREST(api, service, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	tenant := authz.TenantHeader + ": tenant-a"

	response := api.Get("/iam/departments", tenant)
	require.Equal(t, http.StatusOK, response.Code)
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	assert.JSONEq(t, `[]`, string(envelope.Data))
	assert.Equal(t, http.StatusUnprocessableEntity, api.Delete("/iam/departments/missing?version=0", tenant).Code)

	require.NoError(t, db.Close())
	assert.Equal(t, http.StatusInternalServerError, api.Get("/iam/departments", tenant).Code)
}

func decodeDepartment(t *testing.T, body []byte) Department {
	t.Helper()
	var envelope struct {
		Data Department `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &envelope))
	return envelope.Data
}
