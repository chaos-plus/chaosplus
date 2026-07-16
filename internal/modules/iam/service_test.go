package iam

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
)

type wakeRecorder struct{ count int }

func (w *wakeRecorder) Wake() { w.count++ }

func newTestService(t *testing.T) (*Service, *wakeRecorder) {
	t.Helper()
	repo := newIAMRepository(t)
	wake := &wakeRecorder{}
	return NewService(authz.DefaultRegistry(), repo, wake), wake
}

func TestServiceRoleAndBindingFlow(t *testing.T) {
	svc, wake := newTestService(t)
	ctx := context.Background()

	role, err := svc.CreateRole(ctx, "t1", " Managers ", "desc")
	require.NoError(t, err)
	assert.Equal(t, "Managers", role.Name)
	roles, err := svc.ListRoles(ctx, "t1")
	require.NoError(t, err)
	require.Len(t, roles, 1)
	got, err := svc.GetRole(ctx, "t1", role.ID)
	require.NoError(t, err)
	assert.Equal(t, role.ID, got.ID)

	name := "Operators"
	description := "updated"
	got, err = svc.UpdateRole(ctx, "t1", role.ID, &name, &description)
	require.NoError(t, err)
	assert.Equal(t, name, got.Name)

	changed, err := svc.GrantPermission(ctx, "t1", role.ID, "store_view")
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = svc.GrantPermission(ctx, "t1", role.ID, "store_view")
	require.NoError(t, err)
	assert.False(t, changed)
	codes, err := svc.ListPermissions(ctx, "t1", role.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"store_view"}, codes)

	changed, err = svc.AddMember(ctx, "t1", role.ID, " user-sub ")
	require.NoError(t, err)
	assert.True(t, changed)
	members, err := svc.ListMembers(ctx, "t1", role.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"user-sub"}, members)

	changed, err = svc.RevokePermission(ctx, "t1", role.ID, "store_view")
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = svc.RemoveMember(ctx, "t1", role.ID, "user-sub")
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, 5, wake.count)

	require.NoError(t, svc.DeleteRole(ctx, "t1", role.ID))
	assert.Equal(t, 6, wake.count)
	_, err = svc.GetRole(ctx, "t1", role.ID)
	assert.ErrorIs(t, err, ErrRoleNotFound)
}

func TestServiceValidation(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	role, err := svc.CreateRole(ctx, "t1", "role", "")
	require.NoError(t, err)

	_, err = svc.CreateRole(ctx, "", "role", "")
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.CreateRole(ctx, "t1", " ", "")
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.CreateRole(ctx, "t1", strings.Repeat("x", 129), "")
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.CreateRole(ctx, "t1", "role2", strings.Repeat("x", 4097))
	assert.ErrorIs(t, err, ErrInvalidArgument)

	_, err = svc.GetRole(ctx, "t1", "")
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.GetRole(ctx, strings.Repeat("t", 129), role.ID)
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.UpdateRole(ctx, "t1", role.ID, nil, nil)
	assert.ErrorIs(t, err, ErrInvalidArgument)
	empty := " "
	_, err = svc.UpdateRole(ctx, "t1", role.ID, &empty, nil)
	assert.ErrorIs(t, err, ErrInvalidArgument)

	_, err = svc.GrantPermission(ctx, "t1", role.ID, "missing_code")
	assert.ErrorIs(t, err, ErrPermissionNotFound)
	_, err = svc.AddMember(ctx, "t1", role.ID, "")
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.AddMember(ctx, "t1", role.ID, strings.Repeat("u", 256))
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.ListPermissions(ctx, "t1", "missing")
	assert.ErrorIs(t, err, ErrRoleNotFound)
	_, err = svc.ListMembers(ctx, "t1", "missing")
	assert.ErrorIs(t, err, ErrRoleNotFound)
	_, err = svc.ListRoles(ctx, "")
	assert.ErrorIs(t, err, ErrInvalidArgument)
	assert.ErrorIs(t, svc.DeleteRole(ctx, "t1", "missing"), ErrRoleNotFound)
	_, err = svc.RevokePermission(ctx, "t1", "missing", "store_view")
	assert.ErrorIs(t, err, ErrRoleNotFound)
	_, err = svc.RemoveMember(ctx, "t1", "missing", "u1")
	assert.ErrorIs(t, err, ErrRoleNotFound)
	longDescription := strings.Repeat("d", 4097)
	_, err = svc.UpdateRole(ctx, "t1", role.ID, nil, &longDescription)
	assert.ErrorIs(t, err, ErrInvalidArgument)
}

func TestServiceConstructorsRequireDependencies(t *testing.T) {
	repo := newIAMRepository(t)
	wake := &wakeRecorder{}
	assert.Panics(t, func() { NewService(nil, repo, wake) })
	assert.Panics(t, func() { NewService(authz.DefaultRegistry(), nil, wake) })
	assert.Panics(t, func() { NewService(authz.DefaultRegistry(), repo, nil) })
}
