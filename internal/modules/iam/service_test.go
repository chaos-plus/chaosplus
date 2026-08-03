package iam

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	auditmod "github.com/chaos-plus/chaosplus/internal/modules/audit"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	repo := newIAMRepository(t)
	require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, "t1"))
	return NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
}

func TestServiceDialectContract(t *testing.T) {
	db := newLifecycleDatabase(t)
	require.NoError(t, Migrate(t.Context(), db))
	require.NoError(t, organization.Migrate(t.Context(), db))
	require.NoError(t, organization.EnsureTenant(t.Context(), db, "tenant-a"))
	require.NoError(t, organization.EnsureTenant(t.Context(), db, "tenant-b"))

	var id atomic.Int64
	repo := NewRepository(db, func() (string, error) { return fmt.Sprint(id.Add(1)), nil })
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(db), newTestAuditAppender(db))
	ctx := authnext.WithClaims(t.Context(), &authnext.Claims{Subject: "dialect-administrator"})

	roleA, err := service.CreateRole(ctx, "tenant-a", "Operators", "")
	require.NoError(t, err)
	roleB, err := service.CreateRole(ctx, "tenant-b", "Operators", "")
	require.NoError(t, err)
	assert.NotEqual(t, roleA.ID, roleB.ID)
	_, err = service.CreateRole(ctx, "tenant-a", "Operators", "duplicate")
	assert.ErrorIs(t, err, ErrRoleNameConflict)

	_, err = service.PutTenantMember(ctx, "tenant-a", "principal-a", "Principal A", "principal-a@example.test", "", MemberActive)
	require.NoError(t, err)
	member, err := service.PutTenantMember(ctx, "tenant-a", "principal-a", "Principal A Updated", "updated@example.test", "", MemberActive)
	require.NoError(t, err)
	assert.Equal(t, "Principal A Updated", member.DisplayName)
	assert.Equal(t, "updated@example.test", member.Email)

	changed, err := service.GrantPermission(ctx, "tenant-a", roleA.ID, "store_view")
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = service.GrantPermission(ctx, "tenant-a", roleA.ID, "store_view")
	require.NoError(t, err)
	assert.False(t, changed)
	changed, err = service.AddMember(ctx, "tenant-a", roleA.ID, "principal-a")
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = service.AddMember(ctx, "tenant-a", roleA.ID, "principal-a")
	require.NoError(t, err)
	assert.False(t, changed)

	authorizer := NewAuthorizer(db)
	allowed, err := authorizer.Check(ctx, "tenant-a", "store_view", "principal-a")
	require.NoError(t, err)
	assert.True(t, allowed)
	allowed, err = authorizer.Check(ctx, "tenant-b", "store_view", "principal-a")
	require.NoError(t, err)
	assert.False(t, allowed)
	allowed, err = authorizer.Check(ctx, "tenant-a", "store_view", "principal-b")
	require.NoError(t, err)
	assert.False(t, allowed)

	revision, err := repo.policyRevision(ctx, "tenant-a")
	require.NoError(t, err)
	assert.Equal(t, int64(5), revision, "idempotent grants must not advance policy revision")
	_, err = service.SetTenantMemberStatus(ctx, "tenant-a", "principal-a", MemberDisabled)
	require.NoError(t, err)
	allowed, err = authorizer.Check(ctx, "tenant-a", "store_view", "principal-a")
	require.NoError(t, err)
	assert.False(t, allowed, "disabled tenant membership must revoke existing role access immediately")
	revision, err = repo.policyRevision(ctx, "tenant-a")
	require.NoError(t, err)
	assert.Equal(t, int64(6), revision)

	auditService := auditmod.NewService(db)
	integrity, err := auditService.Verify(ctx, "tenant-a")
	require.NoError(t, err)
	assert.True(t, integrity.Valid)
	assert.Equal(t, int64(8), integrity.VerifiedEvents)
	integrity, err = auditService.Verify(ctx, "tenant-b")
	require.NoError(t, err)
	assert.True(t, integrity.Valid)
	assert.Equal(t, int64(1), integrity.VerifiedEvents)
}

func TestServiceRoleAndBindingFlow(t *testing.T) {
	svc := newTestService(t)
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
	condition := json.RawMessage(`{"gte":[{"context":"auth.acr"},{"value":2}],"version":1}`)
	grant, changed, err := svc.SetPermissionCondition(ctx, "t1", role.ID, "store_view", condition)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.JSONEq(t, `{"version":1,"gte":[{"context":"auth.acr"},{"value":2}]}`, string(grant.Condition))
	grants, err := svc.ListPermissionGrants(ctx, "t1", role.ID)
	require.NoError(t, err)
	require.Len(t, grants, 1)
	assert.JSONEq(t, string(grant.Condition), string(grants[0].Condition))
	_, changed, err = svc.SetPermissionCondition(ctx, "t1", role.ID, "store_view", condition)
	require.NoError(t, err)
	assert.False(t, changed)

	_, err = svc.PutTenantMember(ctx, "t1", "user-sub", "User", "", "", MemberActive)
	require.NoError(t, err)
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
	require.NoError(t, svc.DeleteRole(ctx, "t1", role.ID))
	_, err = svc.GetRole(ctx, "t1", role.ID)
	assert.ErrorIs(t, err, ErrRoleNotFound)
}

func TestServiceValidation(t *testing.T) {
	svc := newTestService(t)
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
	_, err = svc.GrantPermission(ctx, "t1", role.ID, "platform_administer")
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, _, err = svc.SetPermissionCondition(ctx, "t1", role.ID, "missing_code", json.RawMessage(`{"version":1,"gte":[]}`))
	assert.ErrorIs(t, err, ErrPermissionNotFound)
	_, _, err = svc.SetPermissionCondition(ctx, "t1", role.ID, "platform_administer", nil)
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, _, err = svc.SetPermissionCondition(ctx, "t1", role.ID, "store_view", json.RawMessage(`{"version":2,"gte":[]}`))
	assert.ErrorIs(t, err, ErrInvalidRolePermissionCondition)
	_, _, err = svc.SetPermissionCondition(ctx, "t1", role.ID, "store_view", nil)
	assert.ErrorIs(t, err, ErrRolePermissionNotGranted)
	for _, action := range svc.PermissionCatalog(ctx) {
		assert.NotEqual(t, "platform", action.Scope)
	}
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
	authorizer := NewAuthorizer(repo.db)
	assert.Panics(t, func() { NewService(nil, repo, authorizer, newTestAuditAppender(repo.db)) })
	assert.Panics(t, func() { NewService(authz.DefaultRegistry(), nil, authorizer, newTestAuditAppender(repo.db)) })
	assert.Panics(t, func() { NewService(authz.DefaultRegistry(), repo, nil, newTestAuditAppender(repo.db)) })
	assert.Panics(t, func() { NewService(authz.DefaultRegistry(), repo, authorizer, nil) })
}
