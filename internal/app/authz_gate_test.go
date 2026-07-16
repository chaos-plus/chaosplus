package app

import (
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/require"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
)

// This is the repository-wide CI gate: every registered REST operation must
// declare a Guard or an explicit Public exception.
func TestRESTOperationsDeclareAuthorization(t *testing.T) {
	registry := authz.DefaultRegistry()
	app := &App{authzRegistrar: authz.NewDeclarationOnlyRegistrar(registry)}
	app.mods = app.buildModules()
	_, api := humatest.New(t)
	app.registerREST(api)
	require.NoError(t, authz.ValidateOperations(api, registry))
}
