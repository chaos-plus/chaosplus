package app

import (
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestExtensionAuthorizationRegistry(t *testing.T) {
	extension := Extension{Name: "ai", Actions: []authz.Action{{Resource: "workflow", Verb: "view", Scope: "tenant"}}}
	application := NewResourceApp(Config{}, extension)
	registry, err := application.buildAuthorizationRegistry()
	require.NoError(t, err)
	_, exists := registry.Find("workflow_view")
	assert.True(t, exists)

	_, err = NewApp(Config{}, Extension{Name: "duplicate", Actions: []authz.Action{{Resource: "tenant", Verb: "view"}}}).buildAuthorizationRegistry()
	assert.ErrorContains(t, err, "duplicate authz code")
	_, err = NewApp(Config{}, Extension{}).buildAuthorizationRegistry()
	assert.ErrorContains(t, err, "requires a name")
}

func TestBuildExtensionModulesFailsClosed(t *testing.T) {
	db := new(bun.DB)
	registry := authz.DefaultRegistry()
	application := &App{
		ctx:            t.Context(),
		dbr:            bunx.DatasourceRouter{Writer: []*bun.DB{db}},
		authzRegistrar: authz.NewDeclarationOnlyRegistrar(registry),
		extensions:     []Extension{{Name: "ai", BuildModules: func(ModuleDependencies) ([]any, error) { return nil, nil }}},
	}
	assert.ErrorContains(t, application.buildExtensionModules(), "production authorization stack")

	application.authzRegistrar = nil
	assert.ErrorContains(t, application.buildExtensionModules(), "production authorization stack")
	application.dbr = bunx.DatasourceRouter{}
	assert.ErrorContains(t, application.buildExtensionModules(), "writable database")
}

func TestNewResourceAppPreservesExtensions(t *testing.T) {
	extension := Extension{Name: "ai"}
	application := NewResourceApp(Config{Name: "resource"}, extension)
	assert.True(t, application.resourceProfile)
	require.Len(t, application.extensions, 1)
	assert.Equal(t, "ai", application.extensions[0].Name)
	assert.Equal(t, "RESOURCE", application.name)
}
