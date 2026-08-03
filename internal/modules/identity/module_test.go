package identity

import (
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModuleRegistersIdentityOperations(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	registrar := authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry())
	module := NewModule(db, registrar, newIdentityAuditAppender(db), iam.NewAdministratorGuard())
	_, api := humatest.New(t)

	module.RegisterREST(api)
	assert.Contains(t, api.OpenAPI().Paths, "/iam/principals")
	assert.Contains(t, api.OpenAPI().Paths, "/iam/principals/{id}")
	assert.Panics(t, func() { NewModule(db, nil, newIdentityAuditAppender(db), iam.NewAdministratorGuard()) })
	assert.Panics(t, func() { NewModule(db, registrar, nil, iam.NewAdministratorGuard()) })
	assert.Panics(t, func() { NewModule(db, registrar, newIdentityAuditAppender(db), nil) })
}
