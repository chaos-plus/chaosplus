package organization

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPositionHTTPWorkflow(t *testing.T) {
	db, service := newPositionService(t)
	addTenantMember(t, db, "tenant-a", "principal/a", "Alice", iam.MemberActive)
	_, api := humatest.New(t)
	RegisterPositionREST(api, service, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	tenant := authz.TenantHeader + ": " + wireID("tenant-a")

	created := api.Post("/iam/positions", tenant, map[string]any{"code": "ENGINEER", "name": "Engineer", "sort_order": 10})
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	position := decodePosition(t, created.Body.Bytes())
	assert.Equal(t, "engineer", position.Code)
	assert.Equal(t, http.StatusOK, api.Get("/iam/positions/"+position.ID.String(), tenant).Code)
	assert.Equal(t, http.StatusConflict, api.Post("/iam/positions", tenant, map[string]any{"code": "engineer", "name": "Duplicate"}).Code)

	listed := api.Get("/iam/positions", tenant)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	var positions struct {
		Data []Position `json:"data"`
	}
	require.NoError(t, json.Unmarshal(listed.Body.Bytes(), &positions))
	require.Len(t, positions.Data, 1)

	updated := api.Patch("/iam/positions/"+position.ID.String(), tenant, map[string]any{"name": "Platform Engineer", "status": "disabled", "version": position.Version})
	require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
	position = decodePosition(t, updated.Body.Bytes())
	assert.Equal(t, int64(2), position.Version)
	assert.Equal(t, http.StatusConflict, api.Patch("/iam/positions/"+position.ID.String(), tenant, map[string]any{"name": "Stale", "version": 1}).Code)

	start := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	memberResponse := api.Put("/iam/positions/"+position.ID.String()+"/members/"+wireID("principal/a"), tenant, map[string]any{"starts_at": start, "ends_at": end})
	require.Equal(t, http.StatusOK, memberResponse.Code, memberResponse.Body.String())
	members := api.Get("/iam/positions/"+position.ID.String()+"/members", tenant)
	require.Equal(t, http.StatusOK, members.Code, members.Body.String())
	var memberEnvelope struct {
		Data []PositionMember `json:"data"`
	}
	require.NoError(t, json.Unmarshal(members.Body.Bytes(), &memberEnvelope))
	require.Len(t, memberEnvelope.Data, 1)
	assert.Equal(t, wireGUID("principal/a"), memberEnvelope.Data[0].PrincipalID)
	assert.Equal(t, http.StatusConflict, api.Delete("/iam/positions/"+position.ID.String()+"?version=2", tenant).Code)
	assert.Equal(t, http.StatusOK, api.Delete("/iam/positions/"+position.ID.String()+"/members/"+wireID("principal/a"), tenant).Code)
	assert.Equal(t, http.StatusOK, api.Delete("/iam/positions/"+position.ID.String()+"?version=2", tenant).Code)
	assert.Equal(t, http.StatusNotFound, api.Get("/iam/positions/"+position.ID.String(), tenant).Code)
	assert.Equal(t, http.StatusNotFound, api.Get("/iam/positions/"+position.ID.String(), authz.TenantHeader + ": " + wireID("tenant-b")).Code)
}

func TestPositionHTTPEmptyCollectionsAndErrors(t *testing.T) {
	db, service := newPositionService(t)
	_, api := humatest.New(t)
	RegisterPositionREST(api, service, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	tenant := authz.TenantHeader + ": " + wireID("tenant")

	response := api.Get("/iam/positions", tenant)
	require.Equal(t, http.StatusOK, response.Code)
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	assert.JSONEq(t, `[]`, string(envelope.Data))
	assert.Equal(t, http.StatusUnprocessableEntity, api.Post("/iam/positions", tenant, map[string]any{"code": "bad code", "name": "Bad"}).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Delete("/iam/positions/"+wireID("missing")+"?version=0", tenant).Code)
	assert.Equal(t, http.StatusNotFound, api.Put("/iam/positions/"+wireID("missing")+"/members/"+wireID("missing"), tenant, map[string]any{}).Code)

	require.NoError(t, db.Close())
	assert.Equal(t, http.StatusInternalServerError, api.Get("/iam/positions", tenant).Code)
}

func TestPositionHTTPPreservesLastAdministrator(t *testing.T) {
	db, service := newPositionService(t)
	addLocalPrincipal(t, db, "administrator")
	addTenantMember(t, db, "tenant", "administrator", "Administrator", iam.MemberActive)
	_, api := humatest.New(t)
	RegisterPositionREST(api, service, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	tenant := authz.TenantHeader + ": " + wireID("tenant")
	created := api.Post("/iam/positions", tenant, map[string]any{"code": "administrator", "name": "Administrator"})
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	position := decodePosition(t, created.Body.Bytes())
	assigned := api.Put("/iam/positions/"+position.ID.String()+"/members/"+wireID("administrator"), tenant, map[string]any{})
	require.Equal(t, http.StatusOK, assigned.Code, assigned.Body.String())
	bindDirectoryAdministrator(t, db, "tenant", "position", position.ID)

	for _, response := range []*httptest.ResponseRecorder{
		api.Patch("/iam/positions/"+position.ID.String(), tenant, map[string]any{"status": "disabled", "version": position.Version}),
		api.Put("/iam/positions/"+position.ID.String()+"/members/"+wireID("administrator"), tenant, map[string]any{"ends_at": time.Now().UTC().Add(time.Hour)}),
		api.Delete("/iam/positions/"+position.ID.String()+"/members/"+wireID("administrator"), tenant),
	} {
		assert.Equal(t, http.StatusConflict, response.Code, response.Body.String())
		assert.Contains(t, response.Body.String(), "last_tenant_administrator")
	}
}

func TestPositionErrorMapping(t *testing.T) {
	for _, item := range []struct {
		err    error
		status int
	}{
		{ErrPositionNotFound, http.StatusNotFound},
		{ErrPositionCodeConflict, http.StatusConflict},
		{ErrPositionVersionConflict, http.StatusConflict},
		{ErrPositionHasMembers, http.StatusConflict},
		{ErrPositionRoleBound, http.StatusConflict},
		{ErrPositionRelationshipBound, http.StatusConflict},
		{ErrPositionMemberInactive, http.StatusConflict},
		{ErrPositionInvalid, http.StatusUnprocessableEntity},
		{ErrPositionMemberNotFound, http.StatusInternalServerError},
	} {
		var statusError huma.StatusError
		require.True(t, errors.As(positionError(item.err), &statusError))
		assert.Equal(t, item.status, statusError.GetStatus())
	}
}

func decodePosition(t *testing.T, body []byte) Position {
	t.Helper()
	var envelope struct {
		Data Position `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &envelope))
	return envelope.Data
}
