package app

import (
	"sort"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
)

// This is the repository-wide CI gate: every registered REST operation must
// declare a Guard or an explicit Public exception.
func TestRESTOperationsDeclareAuthorization(t *testing.T) {
	registry := authz.DefaultRegistry()
	app := &App{
		cfg:            Config{Authn: authn.Config{Enabled: true}},
		dbr:            bunx.DatasourceRouter{Writer: []*bun.DB{nil}},
		authnVerifier:  &authn.Verifier{},
		authzRegistrar: authz.NewDeclarationOnlyRegistrar(registry),
	}
	app.mods = app.buildModules()
	_, api := humatest.New(t)
	app.registerREST(api)
	require.NoError(t, authz.ValidateOperations(api, registry))
	require.Equal(t, []string{
		"authn-me",
		"lookup-geoip",
		"lookup-geoip-self",
		"next-guid",
		"next-guid-batch",
	}, unguardedOperationIDs(api))
}

func unguardedOperationIDs(api huma.API) []string {
	var ids []string
	for _, item := range api.OpenAPI().Paths {
		for _, op := range []*huma.Operation{
			item.Get,
			item.Post,
			item.Put,
			item.Patch,
			item.Delete,
			item.Options,
			item.Head,
			item.Trace,
		} {
			if op == nil {
				continue
			}
			if _, guarded := op.Extensions[authz.GuardExtensionKey]; !guarded {
				ids = append(ids, op.OperationID)
			}
		}
	}
	sort.Strings(ids)
	return ids
}
