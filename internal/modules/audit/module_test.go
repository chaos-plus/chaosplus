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
	module := NewModule(db, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()), Config{})
	require.NotNil(t, module.service)
	declaration := NewDeclarationOnlyModule(authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	assert.Nil(t, declaration.service)
	assert.Panics(t, func() { NewModule(nil, nil, Config{}) })
	assert.Panics(t, func() { NewModule(db, nil, Config{}) })
	assert.Panics(t, func() { NewDeclarationOnlyModule(nil) })
	assert.Panics(t, func() {
		NewModule(db, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()), Config{Anchor: AnchorConfig{Enabled: true}})
	})

	require.NoError(t, iam.Migrate(t.Context(), db))
	_, api := humatest.New(t)
	module.RegisterREST(api)
	assert.Equal(t, http.StatusOK, api.Get("/iam/audit-integrity", authz.TenantHeader+": tenant").Code)
	_, declarationAPI := humatest.New(t)
	declaration.RegisterREST(declarationAPI)
	require.NotNil(t, declarationAPI.OpenAPI().Paths["/iam/audit-events"].Get)
}

func TestAuditModuleWithAnchoredService(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	endpoint, _ := startTestMinIO(t)
	module := NewModule(db, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()), Config{Anchor: AnchorConfig{
		Enabled: true, Endpoint: endpoint, Bucket: "audit-module-anchored",
		AccessKey: minioTestUser, SecretKey: minioTestPassword, RetentionDays: 30,
	}})
	require.NotNil(t, module.service)
	require.NotNil(t, module.service.anchor)
	require.Nil(t, module.service.signer)

	signed := NewModule(db, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()), Config{Anchor: AnchorConfig{
		Enabled: true, Endpoint: endpoint, Bucket: "audit-module-signed",
		AccessKey: minioTestUser, SecretKey: minioTestPassword, RetentionDays: 30,
		SigningKey: testSignerSeed(t),
	}})
	require.NotNil(t, signed.service.anchor)
	require.NotNil(t, signed.service.signer)
	assert.Panics(t, func() {
		NewModule(db, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()), Config{Anchor: AnchorConfig{
			Enabled: true, Endpoint: endpoint, Bucket: "audit-module-signed",
			AccessKey: minioTestUser, SecretKey: minioTestPassword, RetentionDays: 30,
			SigningKey: "not-a-seed",
		}})
	})
}
