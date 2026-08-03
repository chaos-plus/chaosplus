package app

import (
	"context"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/core/extension/plugin"
	"github.com/chaos-plus/chaosplus/internal/infra/geoip"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/chaos-plus/chaosplus/internal/modules/audit"
	"github.com/chaos-plus/chaosplus/internal/modules/governance"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/chaos-plus/chaosplus/internal/modules/provisioning"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"google.golang.org/grpc"
)

func TestRealModuleLifecycle(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	registry := authz.DefaultRegistry()
	registrar := authz.NewDeclarationOnlyRegistrar(registry)
	module := iam.NewDeclarationOnlyModule(registrar)
	application := &App{mods: []any{module}}

	require.NoError(t, application.migrateModules(context.Background()))
	require.NoError(t, application.startModules(context.Background()))
	_, api := humatest.New(t)
	application.registerREST(api)
	require.NoError(t, authz.ValidateOperations(api, registry))
	require.NoError(t, application.stopModules(context.Background()))
}

func TestBuildModules(t *testing.T) {
	application := &App{}
	modules := application.buildModules()
	require.Len(t, modules, 1)
	_, isGeoIP := modules[0].(*geoip.Module)
	assert.True(t, isGeoIP)

	application = &App{dbr: bunx.DatasourceRouter{Writer: []*bun.DB{nil}}}
	require.Len(t, application.buildModules(), 2)

	application = &App{authzRegistrar: authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry())}
	modules = application.buildModules()
	require.Len(t, modules, 6)
	_, isAudit := modules[0].(*audit.Module)
	assert.True(t, isAudit)
	_, isIAM := modules[1].(*iam.Module)
	assert.True(t, isIAM)
	_, isOrganization := modules[2].(*organization.Module)
	assert.True(t, isOrganization)
	_, isProvisioning := modules[3].(*provisioning.Module)
	assert.True(t, isProvisioning)
	_, isGovernance := modules[4].(*governance.Module)
	assert.True(t, isGovernance)

	claims := &plugin.Claims{}
	application = &App{claimPlugins: claims}
	modules = application.buildModules()
	require.Len(t, modules, 2)
	assert.Same(t, claims, modules[0])
}

func TestRealModuleLifecycleErrorsArePropagated(t *testing.T) {
	t.Run("migrate", func(t *testing.T) {
		db, err := bunxtest.Memory()
		require.NoError(t, err)
		require.NoError(t, db.Close())
		application := &App{mods: []any{guid.NewModule(db, time.Minute)}}
		assert.ErrorContains(t, application.migrateModules(t.Context()), "migrate")
	})

	t.Run("start", func(t *testing.T) {
		db, err := bunxtest.Memory()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		application := &App{mods: []any{guid.NewModule(db, time.Minute)}}
		assert.ErrorContains(t, application.startModules(t.Context()), "start")
	})

	t.Run("stop", func(t *testing.T) {
		db, err := bunxtest.Memory()
		require.NoError(t, err)
		module := guid.NewModule(db, time.Minute)
		require.NoError(t, module.Migrate(t.Context()))
		require.NoError(t, module.Start(t.Context()))
		require.NoError(t, db.Close())
		application := &App{mods: []any{module}}
		assert.ErrorContains(t, application.stopModules(t.Context()), "stop")
	})
}

func TestRegisterGRPCWithRealModule(t *testing.T) {
	application := &App{mods: []any{guid.NewModule(nil, 0)}}
	server := grpc.NewServer()
	application.registerGRPC(server)
	assert.Contains(t, server.GetServiceInfo(), "chaosplus.guid.v1.GuidService")
}
