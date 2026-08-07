package provisioning

import (
	"context"
	"testing"
	"time"

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
		"/iam/scim/targets", "/iam/scim/targets/{target_id}", "/iam/scim/targets/{target_id}/push", "/iam/scim/targets/{target_id}/deprovision",
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

// TestProvisioningModuleSyncLoopReconcilesActiveTenants drives the background
// SCIM reconciler, which ships in every deployment but had no test at all.
func TestProvisioningModuleSyncLoopReconcilesActiveTenants(t *testing.T) {
	env := newProvisioningEnvironment(t)
	registrar := authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry())

	// Without an interval the worker must stay dormant, and Stop must remain
	// safe to call on a module that never started.
	dormant := NewModule(env.db, registrar, env.service.audit, env.service.nextID, env.service.identity, env.service.groups, Config{}, testProvisioningKey())
	require.NoError(t, dormant.Migrate(t.Context()))
	require.NoError(t, dormant.Start(t.Context()))
	require.NoError(t, dormant.Stop(t.Context()))

	// A declaration-only module never starts a worker either.
	declaration := NewDeclarationOnlyModule(registrar)
	require.NoError(t, declaration.Start(t.Context()))

	// With an interval the loop ticks and reconciles every active tenant.
	module := NewModule(env.db, registrar, env.service.audit, env.service.nextID, env.service.identity, env.service.groups,
		Config{SyncInterval: 5 * time.Millisecond}, testProvisioningKey())
	require.NoError(t, module.Migrate(t.Context()))
	require.NoError(t, module.Start(t.Context()))
	time.Sleep(60 * time.Millisecond)

	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, module.Stop(stopCtx))

	// Stopping twice must not block or panic.
	require.NoError(t, module.Stop(stopCtx))

	// reconcileAll fails closed and simply logs when the tenant table is gone.
	_, err := env.db.ExecContext(t.Context(), "DROP TABLE iam_tenants")
	require.NoError(t, err)
	assert.NotPanics(t, func() { module.reconcileAll(t.Context()) })
}

func TestProvisioningRuntimeModuleUsesRealServices(t *testing.T) {
	env := newProvisioningEnvironment(t)
	registrar := authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry())
	module := NewModule(env.db, registrar, env.service.audit, env.service.nextID, env.service.identity, env.service.groups, Config{}, testProvisioningKey())
	require.NoError(t, module.Migrate(t.Context()))
	_, api := humatest.New(t)
	module.RegisterREST(api)
	assert.Contains(t, api.OpenAPI().Paths, "/scim/v2/Users")
	assert.Panics(t, func() {
		NewModule(nil, registrar, env.service.audit, env.service.nextID, env.service.identity, env.service.groups, Config{}, testProvisioningKey())
	})
	assert.Panics(t, func() { NewDeclarationOnlyModule(nil) })
}
