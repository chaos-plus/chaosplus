package provisioning

import (
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProvisioningModuleDeclaresManagementAndProtocolOperations(t *testing.T) {
	registry := authz.DefaultRegistry()
	registrar := authz.NewDeclarationOnlyRegistrar(registry)
	module := NewDeclarationOnlyModule(registrar)
	_, api := humatest.New(t)
	module.RegisterREST(api)
	require.NoError(t, authz.ValidateOperations(api, registry))

	for _, path := range []string{
		"/iam/scim/directories", "/iam/scim/directories/{directory_id}/credentials",
		"/scim/v2/Users", "/scim/v2/Users/{id}", "/scim/v2/Groups", "/scim/v2/Groups/{id}",
		"/scim/v2/Bulk", "/scim/v2/ServiceProviderConfig", "/scim/v2/Schemas", "/scim/v2/ResourceTypes",
	} {
		assert.Contains(t, api.OpenAPI().Paths, path)
	}
	admin := api.OpenAPI().Paths["/iam/scim/directories"].Post
	assert.Equal(t, "tenant_administer", admin.Extensions[authz.GuardExtensionKey])
	protocol := api.OpenAPI().Paths["/scim/v2/Bulk"].Post
	assert.Equal(t, []map[string][]string{{scimBearerScheme: {}}}, protocol.Security)
	assert.Contains(t, protocol.Responses, "413")
	require.NoError(t, module.Migrate(t.Context()))
}

func TestProvisioningRuntimeModuleUsesRealServices(t *testing.T) {
	env := newProvisioningEnvironment(t)
	registrar := authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry())
	module := NewModule(env.db, registrar, env.service.audit, env.service.nextID, env.service.identity, env.service.groups)
	require.NoError(t, module.Migrate(t.Context()))
	_, api := humatest.New(t)
	module.RegisterREST(api)
	assert.Contains(t, api.OpenAPI().Paths, "/scim/v2/Users")
	assert.Panics(t, func() {
		NewModule(nil, registrar, env.service.audit, env.service.nextID, env.service.identity, env.service.groups)
	})
	assert.Panics(t, func() { NewDeclarationOnlyModule(nil) })
}
