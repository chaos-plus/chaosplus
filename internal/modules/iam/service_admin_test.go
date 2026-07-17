package iam

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/spicedbx"
)

type selectiveBulkChecker struct {
	allowed map[string]bool
	err     error
}

func (c selectiveBulkChecker) CheckBulk(_ context.Context, _ spicedbx.ObjectRef, permissions []string, _ spicedbx.SubjectRef, _ spicedbx.ZedToken) (map[string]bool, error) {
	if c.err != nil {
		return nil, c.err
	}
	result := make(map[string]bool, len(permissions))
	for _, code := range permissions {
		result[code] = c.allowed[code]
	}
	return result, nil
}

func TestTenantMemberLifecycleAndFilters(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	member, err := svc.PutTenantMember(ctx, "t1", "u1", " Alice ", "alice@example.com", MemberActive)
	require.NoError(t, err)
	assert.Equal(t, "Alice", member.DisplayName)
	member, err = svc.PutTenantMember(ctx, "t1", "u1", "Alice Updated", "alice@example.com", MemberActive)
	require.NoError(t, err)
	assert.Equal(t, "Alice Updated", member.DisplayName)
	_, err = svc.PutTenantMember(ctx, "t1", "u2", "Bob", "", MemberDisabled)
	require.NoError(t, err)

	members, total, err := svc.ListTenantMembers(ctx, "t1", MemberFilter{Search: "alice", Status: MemberActive, Limit: 50})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "u1", members[0].Subject)
	member, err = svc.SetTenantMemberStatus(ctx, "t1", "u1", MemberDisabled)
	require.NoError(t, err)
	assert.False(t, member.DisabledAt.IsZero())
	active, err := svc.repo.IsMemberActive(ctx, "t1", "u1")
	require.NoError(t, err)
	assert.False(t, active)
	_, err = svc.SetTenantMemberStatus(ctx, "t1", "missing", MemberActive)
	assert.ErrorIs(t, err, ErrMemberNotFound)
}

func TestTenantMemberRoleAssignmentRequiresActiveMembership(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	role, err := svc.CreateRole(ctx, "t1", "Managers", "")
	require.NoError(t, err)
	emptyPermissions, err := svc.ListPermissions(ctx, "t1", role.ID)
	require.NoError(t, err)
	assert.NotNil(t, emptyPermissions)
	emptyMembers, err := svc.ListMembers(ctx, "t1", role.ID)
	require.NoError(t, err)
	assert.NotNil(t, emptyMembers)
	_, err = svc.AddMember(ctx, "t1", role.ID, "missing")
	assert.ErrorIs(t, err, ErrMemberInactive)
	_, err = svc.PutTenantMember(ctx, "t1", "u1", "User", "", MemberActive)
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, "t1", role.ID, "u1")
	require.NoError(t, err)
	roles, err := svc.ListTenantMemberRoles(ctx, "t1", "u1")
	require.NoError(t, err)
	assert.Equal(t, []string{role.ID}, roles)
	_, err = svc.PutTenantMember(ctx, "t1", "u2", "Other", "", MemberActive)
	require.NoError(t, err)
	emptyRoles, err := svc.ListTenantMemberRoles(ctx, "t1", "u2")
	require.NoError(t, err)
	assert.NotNil(t, emptyRoles)
	assert.Empty(t, emptyRoles)
	_, err = svc.ListTenantMemberRoles(ctx, "t1", "missing")
	assert.ErrorIs(t, err, ErrMemberNotFound)
}

func TestTenantMemberValidation(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	for _, tc := range []struct {
		subject, name, email string
		status               MemberStatus
	}{
		{"", "User", "", MemberActive}, {"u", "", "", MemberActive}, {"u", "User", "bad", MemberActive}, {"u", "User", "", "unknown"},
	} {
		_, err := svc.PutTenantMember(ctx, "t1", tc.subject, tc.name, tc.email, tc.status)
		assert.ErrorIs(t, err, ErrInvalidArgument)
	}
	_, _, err := svc.ListTenantMembers(ctx, "t1", MemberFilter{Limit: 0})
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, _, err = svc.ListTenantMembers(ctx, "t1", MemberFilter{Limit: 201})
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.GetTenantMember(ctx, "", "u1")
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.SetTenantMemberStatus(ctx, "t1", "u1", "bad")
	assert.ErrorIs(t, err, ErrInvalidArgument)
}

func TestMenuCRUDCycleAndEffectiveTree(t *testing.T) {
	repo := newIAMRepository(t)
	wake := &wakeRecorder{}
	checker := selectiveBulkChecker{allowed: map[string]bool{"menu_view": false, "user_view": true}}
	svc := NewService(authz.DefaultRegistry(), repo, wake, checker)
	ctx := context.Background()
	root, err := svc.CreateMenu(ctx, Menu{TenantID: "t1", Label: "IAM", Route: "/iam", PermissionCode: "menu_view", Status: MenuActive})
	require.NoError(t, err)
	child, err := svc.CreateMenu(ctx, Menu{TenantID: "t1", ParentID: root.ID, Label: "Users", Route: "/iam/users", Icon: "Users", SortOrder: 2, PermissionCode: "user_view", Status: MenuActive})
	require.NoError(t, err)
	_, err = svc.CreateMenu(ctx, Menu{TenantID: "t1", Label: "Duplicate", Route: "/iam/users", Status: MenuActive})
	assert.ErrorIs(t, err, ErrMenuConflict)

	menus, err := svc.ListMenus(ctx, "t1")
	require.NoError(t, err)
	assert.Len(t, menus, 2)
	child.Label = "People"
	updated, err := svc.UpdateMenu(ctx, child)
	require.NoError(t, err)
	assert.Equal(t, "People", updated.Label)
	root.ParentID = child.ID
	_, err = svc.UpdateMenu(ctx, root)
	assert.ErrorIs(t, err, ErrInvalidArgument)
	assert.ErrorIs(t, svc.DeleteMenu(ctx, "t1", root.ID), ErrMenuHasChildren)

	tree, err := svc.EffectiveMenus(ctx, "t1", "u1")
	require.NoError(t, err)
	require.Len(t, tree, 1)
	assert.Equal(t, "IAM", tree[0].Label)
	require.Len(t, tree[0].Children, 1)
	assert.Equal(t, "People", tree[0].Children[0].Label)
	require.NoError(t, svc.DeleteMenu(ctx, "t1", child.ID))
	require.NoError(t, svc.DeleteMenu(ctx, "t1", root.ID))
	_, err = svc.GetMenu(ctx, "t1", root.ID)
	assert.ErrorIs(t, err, ErrMenuNotFound)
}

func TestEffectiveMenusFailClosed(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := context.Background()
	_, err := repo.CreateMenu(ctx, Menu{TenantID: "t1", Label: "Bad", Route: "/bad", PermissionCode: "missing_permission", Status: MenuActive})
	require.NoError(t, err)
	svc := NewService(authz.DefaultRegistry(), repo, &wakeRecorder{}, selectiveBulkChecker{})
	_, err = svc.EffectiveMenus(ctx, "t1", "u1")
	assert.ErrorContains(t, err, "unknown persisted")
	repo = newIAMRepository(t)
	_, err = repo.CreateMenu(ctx, Menu{TenantID: "t1", Label: "Users", Route: "/users", PermissionCode: "user_view", Status: MenuActive})
	require.NoError(t, err)
	svc = NewService(authz.DefaultRegistry(), repo, &wakeRecorder{}, selectiveBulkChecker{err: errors.New("spicedb down")})
	_, err = svc.EffectiveMenus(ctx, "t1", "u1")
	assert.ErrorContains(t, err, "spicedb down")
}

func TestMenuValidation(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	for _, menu := range []Menu{
		{TenantID: "t1", Label: "", Status: MenuActive},
		{TenantID: "t1", Label: "Bad", Route: "relative", Status: MenuActive},
		{TenantID: "t1", Label: "Bad", PermissionCode: "missing", Status: MenuActive},
		{TenantID: "t1", Label: "Bad", ParentID: "missing", Status: MenuActive},
		{TenantID: "t1", Label: "Bad", Status: "unknown"},
	} {
		_, err := svc.CreateMenu(ctx, menu)
		assert.Error(t, err)
	}
	_, err := svc.GetMenu(ctx, "t1", "")
	assert.ErrorIs(t, err, ErrInvalidArgument)
	assert.ErrorIs(t, svc.DeleteMenu(ctx, "t1", "missing"), ErrMenuNotFound)
}

func TestMembershipChecker(t *testing.T) {
	repo := newIAMRepository(t)
	checker := NewMembershipChecker(repo.db)
	active, err := checker.IsMemberActive(context.Background(), "t1", "u1")
	require.NoError(t, err)
	assert.False(t, active)
	_, err = repo.PutMember(context.Background(), TenantMember{TenantID: "t1", Subject: "u1", DisplayName: "User", Status: MemberActive})
	require.NoError(t, err)
	active, err = checker.IsMemberActive(context.Background(), "t1", "u1")
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
	_, err := repo.PutMember(context.Background(), TenantMember{TenantID: "t1", Subject: "u1", DisplayName: "User", Status: MemberActive})
	assert.ErrorContains(t, err, "unsupported")
	repo.dialect = "sqlite"
	_, err = repo.UpdateMenu(context.Background(), Menu{TenantID: "t1", ID: "missing", Label: "Missing", Status: MenuActive})
	assert.ErrorIs(t, err, ErrMenuNotFound)
	first, err := repo.CreateMenu(context.Background(), Menu{TenantID: "t1", Label: "One", Route: "/one", Status: MenuActive})
	require.NoError(t, err)
	second, err := repo.CreateMenu(context.Background(), Menu{TenantID: "t1", Label: "Two", Route: "/two", Status: MenuActive})
	require.NoError(t, err)
	second.Route = first.Route
	_, err = repo.UpdateMenu(context.Background(), second)
	assert.ErrorIs(t, err, ErrMenuConflict)
}

func TestAdminServiceTenantValidationBranches(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	_, err := svc.ListMenus(ctx, "")
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.GetTenantMember(ctx, "t1", "")
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.ListTenantMemberRoles(ctx, "t1", "")
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.UpdateMenu(ctx, Menu{TenantID: "t1", ID: "missing", Label: "x", Status: MenuActive})
	assert.ErrorIs(t, err, ErrMenuNotFound)
	_, err = svc.EffectiveMenus(ctx, "", "u1")
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.ListPermissions(ctx, "", "r1")
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.ListMembers(ctx, "", "r1")
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.SetTenantMemberStatus(ctx, "", "u1", MemberActive)
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.UpdateMenu(ctx, Menu{TenantID: "t1", ID: "", Label: "x", Status: MenuActive})
	assert.ErrorIs(t, err, ErrInvalidArgument)
	assert.ErrorIs(t, svc.DeleteMenu(ctx, "", "m1"), ErrInvalidArgument)
}
