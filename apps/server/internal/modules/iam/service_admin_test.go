package iam

import (
	"context"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

func TestTenantMemberLifecycleAndFilters(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	member, err := svc.PutTenantMember(ctx, testID("t1"), testID("u1"), " Alice ", "alice@example.com", 0, MemberActive)
	require.NoError(t, err)
	assert.Equal(t, "Alice", member.DisplayName)
	member, err = svc.PutTenantMember(ctx, testID("t1"), testID("u1"), "Alice Updated", "alice@example.com", 0, MemberActive)
	require.NoError(t, err)
	assert.Equal(t, "Alice Updated", member.DisplayName)
	_, err = svc.PutTenantMember(ctx, testID("t1"), testID("u2"), "Bob", "", 0, MemberDisabled)
	require.NoError(t, err)

	members, total, err := svc.ListTenantMembers(ctx, testID("t1"), MemberFilter{Search: "alice", Status: MemberActive, Limit: 50})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, testID("u1"), members[0].PrincipalID)
	member, err = svc.SetTenantMemberStatus(ctx, testID("t1"), testID("u1"), MemberDisabled)
	require.NoError(t, err)
	assert.False(t, member.DisabledAt.IsZero())
	active, err := svc.repo.IsMemberActive(ctx, testID("t1"), testID("u1"))
	require.NoError(t, err)
	assert.False(t, active)
	_, err = svc.SetTenantMemberStatus(ctx, testID("t1"), testID("missing"), MemberActive)
	assert.ErrorIs(t, err, ErrMemberNotFound)
}

func TestTenantMemberRoleAssignmentRequiresActiveMembership(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	role, err := svc.CreateRole(ctx, testID("t1"), "Managers", "")
	require.NoError(t, err)
	emptyPermissions, err := svc.ListPermissions(ctx, testID("t1"), role.ID)
	require.NoError(t, err)
	assert.NotNil(t, emptyPermissions)
	emptyMembers, err := svc.ListMembers(ctx, testID("t1"), role.ID)
	require.NoError(t, err)
	assert.NotNil(t, emptyMembers)
	_, err = svc.AddMember(ctx, testID("t1"), role.ID, testID("missing"))
	assert.ErrorIs(t, err, ErrMemberInactive)
	_, err = svc.PutTenantMember(ctx, testID("t1"), testID("u1"), "User", "", 0, MemberActive)
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, testID("t1"), role.ID, testID("u1"))
	require.NoError(t, err)
	roles, err := svc.ListTenantMemberRoles(ctx, testID("t1"), testID("u1"))
	require.NoError(t, err)
	assert.Equal(t, []guid.ID{role.ID}, roles)
	_, err = svc.PutTenantMember(ctx, testID("t1"), testID("u2"), "Other", "", 0, MemberActive)
	require.NoError(t, err)
	emptyRoles, err := svc.ListTenantMemberRoles(ctx, testID("t1"), testID("u2"))
	require.NoError(t, err)
	assert.NotNil(t, emptyRoles)
	assert.Empty(t, emptyRoles)
	_, err = svc.ListTenantMemberRoles(ctx, testID("t1"), testID("missing"))
	assert.ErrorIs(t, err, ErrMemberNotFound)
}

func TestTenantMemberValidation(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	for _, tc := range []struct {
		subject     guid.ID
		name, email string
		status      MemberStatus
	}{
		{0, "User", "", MemberActive}, {testID("u"), "", "", MemberActive}, {testID("u"), "User", "bad", MemberActive}, {testID("u"), "User", "", "unknown"},
	} {
		_, err := svc.PutTenantMember(ctx, testID("t1"), tc.subject, tc.name, tc.email, 0, tc.status)
		assert.ErrorIs(t, err, ErrInvalidArgument)
	}
	_, _, err := svc.ListTenantMembers(ctx, testID("t1"), MemberFilter{Limit: 0})
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, _, err = svc.ListTenantMembers(ctx, testID("t1"), MemberFilter{Limit: 201})
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.GetTenantMember(ctx, 0, testID("u1"))
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.SetTenantMemberStatus(ctx, testID("t1"), testID("u1"), "bad")
	assert.ErrorIs(t, err, ErrInvalidArgument)
}

func TestMenuCRUDCycleAndEffectiveTree(t *testing.T) {
	repo := newIAMRepository(t)
	require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, testID("t1")))
	svc := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	ctx := context.Background()
	_, err := svc.PutTenantMember(ctx, testID("t1"), testID("u1"), "User", "", 0, MemberActive)
	require.NoError(t, err)
	role, err := svc.CreateRole(ctx, testID("t1"), "User readers", "")
	require.NoError(t, err)
	_, err = svc.GrantPermission(ctx, testID("t1"), role.ID, "user_view")
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, testID("t1"), role.ID, testID("u1"))
	require.NoError(t, err)
	root, err := svc.CreateMenu(ctx, Menu{TenantID: testID("t1"), Label: "IAM", Route: "/iam", PermissionCode: "menu_view", Status: MenuActive})
	require.NoError(t, err)
	child, err := svc.CreateMenu(ctx, Menu{TenantID: testID("t1"), ParentID: root.ID, Label: "Users", Route: "/iam/users", Icon: "Users", SortOrder: 2, PermissionCode: "user_view", Status: MenuActive})
	require.NoError(t, err)
	_, err = svc.CreateMenu(ctx, Menu{TenantID: testID("t1"), Label: "Duplicate", Route: "/iam/users", Status: MenuActive})
	assert.ErrorIs(t, err, ErrMenuConflict)

	menus, err := svc.ListMenus(ctx, testID("t1"))
	require.NoError(t, err)
	assert.Len(t, menus, 2)
	child.Label = "People"
	updated, err := svc.UpdateMenu(ctx, child)
	require.NoError(t, err)
	assert.Equal(t, "People", updated.Label)
	root.ParentID = child.ID
	_, err = svc.UpdateMenu(ctx, root)
	assert.ErrorIs(t, err, ErrInvalidArgument)
	assert.ErrorIs(t, svc.DeleteMenu(ctx, testID("t1"), root.ID), ErrMenuHasChildren)

	tree, err := svc.EffectiveMenus(ctx, testID("t1"), testID("u1"))
	require.NoError(t, err)
	require.Len(t, tree, 1)
	assert.Equal(t, "IAM", tree[0].Label)
	require.Len(t, tree[0].Children, 1)
	assert.Equal(t, "People", tree[0].Children[0].Label)
	require.NoError(t, svc.DeleteMenu(ctx, testID("t1"), child.ID))
	require.NoError(t, svc.DeleteMenu(ctx, testID("t1"), root.ID))
	tree, err = svc.EffectiveMenus(ctx, testID("t1"), testID("u1"))
	require.NoError(t, err)
	assert.NotNil(t, tree)
	assert.Empty(t, tree)
	_, err = svc.GetMenu(ctx, testID("t1"), root.ID)
	assert.ErrorIs(t, err, ErrMenuNotFound)
}

func TestEffectiveMenusFailClosed(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := context.Background()
	require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, testID("t1")))
	putTestMember(t, repo, TenantMember{TenantID: testID("t1"), PrincipalID: testID("u1"), DisplayName: "User", Status: MemberActive})
	_, err := repo.CreateMenu(ctx, Menu{TenantID: testID("t1"), Label: "Bad", Route: "/bad", PermissionCode: "missing_permission", Status: MenuActive})
	require.NoError(t, err)
	svc := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	_, err = svc.EffectiveMenus(ctx, testID("t1"), testID("u1"))
	assert.ErrorContains(t, err, "unknown persisted")
	repo = newIAMRepository(t)
	require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, testID("t1")))
	putTestMember(t, repo, TenantMember{TenantID: testID("t1"), PrincipalID: testID("u1"), DisplayName: "User", Status: MemberActive})
	_, err = repo.CreateMenu(ctx, Menu{TenantID: testID("t1"), Label: "Users", Route: "/users", PermissionCode: "user_view", Status: MenuActive})
	require.NoError(t, err)
	require.NoError(t, Migrate(t.Context(), repo.db))
	_, err = repo.db.ExecContext(ctx, "DROP TABLE iam_role_permissions")
	require.NoError(t, err)
	svc = NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	_, err = svc.EffectiveMenus(ctx, testID("t1"), testID("u1"))
	assert.ErrorContains(t, err, "check effective menu permissions")
}

func TestMenuValidation(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	for _, menu := range []Menu{
		{TenantID: testID("t1"), Label: "", Status: MenuActive},
		{TenantID: testID("t1"), Label: "Bad", Route: "relative", Status: MenuActive},
		{TenantID: testID("t1"), Label: "Bad", PermissionCode: "missing", Status: MenuActive},
		{TenantID: testID("t1"), Label: "Bad", ParentID: testID("missing"), Status: MenuActive},
		{TenantID: testID("t1"), Label: "Bad", Status: "unknown"},
	} {
		_, err := svc.CreateMenu(ctx, menu)
		assert.Error(t, err)
	}
	_, err := svc.GetMenu(ctx, testID("t1"), 0)
	assert.ErrorIs(t, err, ErrInvalidArgument)
	assert.ErrorIs(t, svc.DeleteMenu(ctx, testID("t1"), testID("missing")), ErrMenuNotFound)
}

func TestMembershipChecker(t *testing.T) {
	repo := newIAMRepository(t)
	checker := NewMembershipChecker(repo.db)
	require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, testID("t1")))
	active, err := checker.IsMemberActive(context.Background(), testID("t1"), testID("u1"))
	require.NoError(t, err)
	assert.False(t, active)
	putTestMember(t, repo, TenantMember{TenantID: testID("t1"), PrincipalID: testID("u1"), DisplayName: "User", Status: MemberActive})
	active, err = checker.IsMemberActive(context.Background(), testID("t1"), testID("u1"))
	require.NoError(t, err)
	assert.True(t, active)
	assert.Panics(t, func() { NewMembershipChecker(nil) })
}

func TestAdminRepositoryDialectAndConflictBranches(t *testing.T) {
	assert.Contains(t, memberUpsertSQL("mysql"), "DUPLICATE KEY")
	assert.Contains(t, memberUpsertSQL("postgres"), "ON CONFLICT")
	assert.Contains(t, memberUpsertSQL("sqlite"), "ON CONFLICT")
	assert.Empty(t, memberUpsertSQL("unknown"))
	repo := newIAMRepository(t)
	repo.dialect = "unknown"
	_, err := repo.PutMember(context.Background(), TenantMember{TenantID: testID("t1"), PrincipalID: testID("u1"), DisplayName: "User", Status: MemberActive})
	assert.ErrorContains(t, err, "unsupported")
	repo.dialect = "sqlite"
	_, err = repo.UpdateMenu(context.Background(), Menu{TenantID: testID("t1"), ID: testID("missing"), Label: "Missing", Status: MenuActive})
	assert.ErrorIs(t, err, ErrMenuNotFound)
	first, err := repo.CreateMenu(context.Background(), Menu{TenantID: testID("t1"), Label: "One", Route: "/one", Status: MenuActive})
	require.NoError(t, err)
	second, err := repo.CreateMenu(context.Background(), Menu{TenantID: testID("t1"), Label: "Two", Route: "/two", Status: MenuActive})
	require.NoError(t, err)
	second.Route = first.Route
	_, err = repo.UpdateMenu(context.Background(), second)
	assert.ErrorIs(t, err, ErrMenuConflict)
}

func TestAdminServiceTenantValidationBranches(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	_, err := svc.ListMenus(ctx, 0)
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.GetTenantMember(ctx, testID("t1"), 0)
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.ListTenantMemberRoles(ctx, testID("t1"), 0)
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.UpdateMenu(ctx, Menu{TenantID: testID("t1"), ID: testID("missing"), Label: "x", Status: MenuActive})
	assert.ErrorIs(t, err, ErrMenuNotFound)
	_, err = svc.EffectiveMenus(ctx, 0, testID("u1"))
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.ListPermissions(ctx, 0, testID("r1"))
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.ListMembers(ctx, 0, testID("r1"))
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.SetTenantMemberStatus(ctx, 0, testID("u1"), MemberActive)
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.UpdateMenu(ctx, Menu{TenantID: testID("t1"), ID: 0, Label: "x", Status: MenuActive})
	assert.ErrorIs(t, err, ErrInvalidArgument)
	assert.ErrorIs(t, svc.DeleteMenu(ctx, 0, testID("m1")), ErrInvalidArgument)
}
