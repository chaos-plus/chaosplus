package iam_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
)

func seedRESTPrincipal(t *testing.T, db *bun.DB, principalID string) {
	t.Helper()
	now := time.Now().UTC().UnixMilli()
	_, err := db.ExecContext(t.Context(), `INSERT INTO iam_principals
		(id, login_name, email, display_name, status, created_at, updated_at, disabled_at)
		VALUES (?, ?, ?, ?, 'active', ?, ?, 0)`,
		principalID, principalID, principalID+"@example.test", principalID, now, now)
	require.NoError(t, err)
}

func decodePlatformAdministrators(t *testing.T, body []byte) []iam.PlatformAdministratorView {
	t.Helper()
	var envelope struct {
		Data []iam.PlatformAdministratorView `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &envelope))
	return envelope.Data
}

func TestPlatformAdministratorHTTPLifecycle(t *testing.T) {
	db, api, authorization := newIAMAPI(t)
	header := authorization.header
	seedRESTPrincipal(t, db, "operator-2")

	// The platform catalog is the only way to discover platform codes, since
	// the tenant catalog deliberately excludes them.
	catalog := api.Get("/iam/platform-permission-catalog", header)
	require.Equal(t, http.StatusOK, catalog.Code, catalog.Body.String())
	var catalogEnvelope struct {
		Data []authz.Action `json:"data"`
	}
	require.NoError(t, json.Unmarshal(catalog.Body.Bytes(), &catalogEnvelope))
	require.NotEmpty(t, catalogEnvelope.Data)
	for _, action := range catalogEnvelope.Data {
		assert.Equal(t, "platform", action.Scope, action.Code)
	}

	// The bootstrap principal shows up as a full administrator.
	listed := api.Get("/iam/platform-administrators", header)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	administrators := decodePlatformAdministrators(t, listed.Body.Bytes())
	require.Len(t, administrators, 1)
	assert.Equal(t, authorization.principalID, administrators[0].PrincipalID)
	assert.True(t, administrators[0].FullAdministrator)
	assert.Empty(t, administrators[0].Permissions)

	// A restricted grant is accepted and echoed back sorted.
	restricted := api.Put("/iam/platform-administrators/operator-2", header, map[string]any{
		"full_administrator": false, "permissions": []string{"tenant_view", "tenant_create"},
	})
	require.Equal(t, http.StatusOK, restricted.Code, restricted.Body.String())
	var single struct {
		Data iam.PlatformAdministratorView `json:"data"`
	}
	require.NoError(t, json.Unmarshal(restricted.Body.Bytes(), &single))
	assert.False(t, single.Data.FullAdministrator)
	assert.Equal(t, []string{"tenant_create", "tenant_view"}, single.Data.Permissions)

	// Promotion clears the explicit grants.
	promoted := api.Put("/iam/platform-administrators/operator-2", header, map[string]any{"full_administrator": true})
	require.Equal(t, http.StatusOK, promoted.Code, promoted.Body.String())
	require.NoError(t, json.Unmarshal(promoted.Body.Bytes(), &single))
	assert.True(t, single.Data.FullAdministrator)
	assert.Empty(t, single.Data.Permissions)

	listed = api.Get("/iam/platform-administrators", header)
	require.Equal(t, http.StatusOK, listed.Code)
	assert.Len(t, decodePlatformAdministrators(t, listed.Body.Bytes()), 2)

	// Revoking one of two full administrators is allowed; revoking twice is 404.
	removed := api.Delete("/iam/platform-administrators/operator-2", header)
	require.Equal(t, http.StatusOK, removed.Code, removed.Body.String())
	assert.Equal(t, http.StatusNotFound, api.Delete("/iam/platform-administrators/operator-2", header).Code)
}

func TestPlatformAdministratorHTTPFailures(t *testing.T) {
	db, api, authorization := newIAMAPI(t)
	header := authorization.header
	seedRESTPrincipal(t, db, "operator-3")

	// Tenant-scoped and unknown codes are refused.
	scope := api.Put("/iam/platform-administrators/operator-3", header, map[string]any{
		"full_administrator": false, "permissions": []string{"store_view"},
	})
	assert.Equal(t, http.StatusUnprocessableEntity, scope.Code, scope.Body.String())
	unknown := api.Put("/iam/platform-administrators/operator-3", header, map[string]any{
		"full_administrator": false, "permissions": []string{"not_declared"},
	})
	assert.Equal(t, http.StatusUnprocessableEntity, unknown.Code, unknown.Body.String())

	// A full administrator may not also carry explicit permissions, and a
	// restricted one may not be empty.
	conflicting := api.Put("/iam/platform-administrators/operator-3", header, map[string]any{
		"full_administrator": true, "permissions": []string{"tenant_view"},
	})
	assert.Equal(t, http.StatusUnprocessableEntity, conflicting.Code, conflicting.Body.String())
	empty := api.Put("/iam/platform-administrators/operator-3", header, map[string]any{"full_administrator": false})
	assert.Equal(t, http.StatusUnprocessableEntity, empty.Code, empty.Body.String())

	// The platform must always retain one full administrator.
	last := api.Delete("/iam/platform-administrators/"+authorization.principalID, header)
	assert.Equal(t, http.StatusConflict, last.Code, last.Body.String())
	demote := api.Put("/iam/platform-administrators/"+authorization.principalID, header, map[string]any{
		"full_administrator": false, "permissions": []string{"tenant_view"},
	})
	assert.Equal(t, http.StatusConflict, demote.Code, demote.Body.String())

	// The roster survived both refusals unchanged.
	listed := api.Get("/iam/platform-administrators", header)
	require.Equal(t, http.StatusOK, listed.Code)
	administrators := decodePlatformAdministrators(t, listed.Body.Bytes())
	require.Len(t, administrators, 1)
	assert.True(t, administrators[0].FullAdministrator)
}

func TestPlatformRoutesRejectNonPlatformPrincipal(t *testing.T) {
	db, api, authorization := newIAMAPI(t)
	// Dropping platform authorization must close the platform routes even
	// though the principal is still a tenant administrator.
	_, err := db.ExecContext(t.Context(), "DELETE FROM iam_platform_administrators WHERE principal_id = ?", authorization.principalID)
	require.NoError(t, err)

	for _, code := range []int{
		api.Get("/iam/platform-administrators", authorization.header).Code,
		api.Get("/iam/platform-permission-catalog", authorization.header).Code,
		api.Delete("/iam/platform-administrators/whoever", authorization.header).Code,
	} {
		assert.Equal(t, http.StatusForbidden, code)
	}
}
