package iam

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
)

func TestModuleRegistersREST(t *testing.T) {
	m := NewDeclarationOnlyModule(authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	require.NotNil(t, m.service)
	assert.IsType(t, (*Service)(nil), m.service)

	_, a := humatest.New(t)
	m.RegisterREST(a)
	assert.Equal(t, http.StatusOK, a.Get("/iam/permission-catalog").Code)
}

func TestNewModuleRequiresRegistrar(t *testing.T) {
	assert.Panics(t, func() { NewDeclarationOnlyModule(nil) })
	assert.Panics(t, func() { NewModule(nil, nil, nil, nil, nil) })
}

func TestModuleLifecycle(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	var id atomic.Int64
	m := NewModule(
		db,
		authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()),
		NewAuthorizer(db),
		newTestAuditAppender(db),
		func() (string, error) { return fmt.Sprint(id.Add(1)), nil },
	)
	require.NoError(t, m.Migrate(context.Background()))

	declaration := NewDeclarationOnlyModule(authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	require.NoError(t, declaration.Migrate(context.Background()))
}

func TestServiceReadModels(t *testing.T) {
	svc := newDeclarationService(authz.MustRegistry(
		authz.Action{Resource: "store", Verb: "view", Menu: true},
		authz.Action{Resource: "role", Verb: "view"},
		authz.Action{Resource: "dept", Verb: "view"},
		authz.Action{Resource: "position", Verb: "view"},
		authz.Action{Resource: "user", Verb: "view"},
		authz.Action{Resource: "menu", Verb: "view"},
		authz.Action{Resource: "tenant", Verb: "view"},
		authz.Action{Resource: "merchant", Verb: "view"},
	))
	ctx := context.Background()

	perms := svc.PermissionCatalog(ctx)
	require.NotEmpty(t, perms)
	assert.Equal(t, "dept_view", perms[0].Code)

	scopes := svc.ScopeModel(ctx)
	require.Len(t, scopes, 4)
	assert.Equal(t, "platform", scopes[0].Type)
	assert.Equal(t, "entity", scopes[3].ParentType)

	menus := svc.MenuCatalog(ctx)
	require.Len(t, menus, 1)
	require.Len(t, menus[0].Children, 14)
	assert.Equal(t, "/iam/users", menus[0].Children[0].Path)
	assert.Equal(t, "/iam/invitations", menus[0].Children[1].Path)
	assert.Equal(t, "/iam/service-accounts", menus[0].Children[2].Path)
	assert.Equal(t, "/iam/entities", menus[0].Children[3].Path)
	assert.Equal(t, "/iam/departments", menus[0].Children[4].Path)
	assert.Equal(t, "/iam/positions", menus[0].Children[5].Path)
	assert.Equal(t, "/iam/groups", menus[0].Children[6].Path)
	assert.Equal(t, "/iam/access-requests", menus[0].Children[8].Path)
	assert.Equal(t, "/iam/access-reviews", menus[0].Children[9].Path)
	assert.Equal(t, "/iam/scim-directories", menus[0].Children[12].Path)
	assert.Equal(t, "audit_event_view", menus[0].Children[13].PermissionCode)
	assert.Len(t, DefaultMenus(), 14)
}
