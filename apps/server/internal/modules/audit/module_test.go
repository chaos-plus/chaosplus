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
	// Start with no anchor config is a no-op.
	require.NoError(t, module.Start(t.Context()))
	declaration := NewDeclarationOnlyModule(authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	assert.Nil(t, declaration.service)
	assert.Panics(t, func() { NewModule(nil, nil, Config{}) })
	assert.Panics(t, func() { NewModule(db, nil, Config{}) })
	assert.Panics(t, func() { NewDeclarationOnlyModule(nil) })

	// Invalid anchor config returns an error from Start, not a panic from NewModule.
	badAnchor := NewModule(db, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()), Config{Anchor: AnchorConfig{Enabled: true}})
	assert.Error(t, badAnchor.Start(t.Context()))

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
	require.NoError(t, module.Start(t.Context()))
	require.NotNil(t, module.service.anchor)
	require.Nil(t, module.service.signer)

	signed := NewModule(db, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()), Config{Anchor: AnchorConfig{
		Enabled: true, Endpoint: endpoint, Bucket: "audit-module-signed",
		AccessKey: minioTestUser, SecretKey: minioTestPassword, RetentionDays: 30,
		SigningKey: testSignerSeed(t),
	}})
	require.NoError(t, signed.Start(t.Context()))
	require.NotNil(t, signed.service.anchor)
	require.NotNil(t, signed.service.signer)

	// Invalid signing key returns an error from Start, not a panic.
	badSigner := NewModule(db, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()), Config{Anchor: AnchorConfig{
		Enabled: true, Endpoint: endpoint, Bucket: "audit-module-signed",
		AccessKey: minioTestUser, SecretKey: minioTestPassword, RetentionDays: 30,
		SigningKey: "not-a-seed",
	}})
	assert.Error(t, badSigner.Start(t.Context()))
}
