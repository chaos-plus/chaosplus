package audit

import (
	"net/http"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditModuleConstruction(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	module := NewModule(db, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	require.NotNil(t, module.service)
	declaration := NewDeclarationOnlyModule(authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	assert.Nil(t, declaration.service)
	assert.Panics(t, func() { NewModule(nil, nil) })
	assert.Panics(t, func() { NewModule(db, nil) })
	assert.Panics(t, func() { NewDeclarationOnlyModule(nil) })

	require.NoError(t, iam.Migrate(t.Context(), db))
	_, api := humatest.New(t)
	module.RegisterREST(api)
	assert.Equal(t, http.StatusOK, api.Get("/iam/audit-integrity", authz.TenantHeader+": tenant").Code)
	_, declarationAPI := humatest.New(t)
	declaration.RegisterREST(declarationAPI)
	require.NotNil(t, declarationAPI.OpenAPI().Paths["/iam/audit-events"].Get)
}
