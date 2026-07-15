package iam

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/modules/iam/api"
)

func TestModuleRegistersREST(t *testing.T) {
	m := NewModule()
	require.NotNil(t, m.service)
	assert.Implements(t, (*api.Service)(nil), m.service)

	_, a := humatest.New(t)
	m.RegisterREST(a)
	assert.Equal(t, http.StatusOK, a.Get("/iam/permission-catalog").Code)
}

func TestServiceReadModels(t *testing.T) {
	svc := NewService(authz.MustRegistry(
		authz.Action{Resource: "store", Verb: "view", Menu: true},
		authz.Action{Resource: "role", Verb: "view"},
		authz.Action{Resource: "dept", Verb: "view"},
		authz.Action{Resource: "user", Verb: "view"},
		authz.Action{Resource: "menu", Verb: "view"},
		authz.Action{Resource: "tenant", Verb: "view"},
		authz.Action{Resource: "merchant", Verb: "view"},
	))
	ctx := context.Background()

	perms := svc.PermissionCatalog(ctx)
	require.NotEmpty(t, perms)
	assert.Equal(t, "dept_view", perms[0].Code)

	schema := svc.SpiceDBSchema(ctx)
	assert.True(t, strings.Contains(schema, "definition tenant"))
	assert.True(t, strings.Contains(schema, "relation store_view_role"))

	scopes := svc.ScopeModel(ctx)
	require.Len(t, scopes, 5)
	assert.Equal(t, "platform", scopes[0].Type)
	assert.Equal(t, "merchant", scopes[3].ParentType)

	menus := svc.MenuCatalog(ctx)
	require.Len(t, menus, 2)
	assert.Equal(t, "menu_view", menus[0].PermissionCode)
	assert.Equal(t, "store_view", menus[1].Children[2].PermissionCode)
}
