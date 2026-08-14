package iam

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	auditmod "github.com/chaos-plus/chaosplus/internal/modules/audit"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	repo := newIAMRepository(t)
	require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, testID("t1")))
	return NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
}

func TestServiceDialectContract(t *testing.T) {
	db := newLifecycleDatabase(t)
	require.NoError(t, Migrate(t.Context(), db))
	require.NoError(t, organization.Migrate(t.Context(), db))
	require.NoError(t, organization.EnsureTenant(t.Context(), db, testID("tenant-a")))
	require.NoError(t, organization.EnsureTenant(t.Context(), db, testID("tenant-b")))

	repo := NewRepository(db, newTestIDGenerator())
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(db), newTestAuditAppender(db))
	makeTenantAdministrator(t, repo, testID("tenant-a"), testID("dialect-administrator"))
	makeTenantAdministrator(t, repo, testID("tenant-b"), testID("dialect-administrator"))
	ctx := authnext.WithClaims(t.Context(), &authnext.Claims{Subject: "dialect-administrator", PrincipalID: testID("dialect-administrator")})

	roleA, err := service.CreateRole(ctx, testID("tenant-a"), "Operators", "")
	require.NoError(t, err)
	roleB, err := service.CreateRole(ctx, testID("tenant-b"), "Operators", "")
	require.NoError(t, err)
	assert.NotEqual(t, roleA.ID, roleB.ID)
	_, err = service.CreateRole(ctx, testID("tenant-a"), "Operators", "duplicate")
	assert.ErrorIs(t, err, ErrRoleNameConflict)

	_, err = service.PutTenantMember(ctx, testID("tenant-a"), testID("principal-a"), "Principal A", "principal-a@example.test", 0, MemberActive)
	require.NoError(t, err)
	member, err := service.PutTenantMember(ctx, testID("tenant-a"), testID("principal-a"), "Principal A Updated", "updated@example.test", 0, MemberActive)
	require.NoError(t, err)
	assert.Equal(t, "Principal A Updated", member.DisplayName)
	assert.Equal(t, "updated@example.test", member.Email)

	changed, err := service.GrantPermission(ctx, testID("tenant-a"), roleA.ID, "store_view")
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = service.GrantPermission(ctx, testID("tenant-a"), roleA.ID, "store_view")
	require.NoError(t, err)
	assert.False(t, changed)
	changed, err = service.AddMember(ctx, testID("tenant-a"), roleA.ID, testID("principal-a"))
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = service.AddMember(ctx, testID("tenant-a"), roleA.ID, testID("principal-a"))
	require.NoError(t, err)
	assert.False(t, changed)

	authorizer := NewAuthorizer(db)
	allowed, err := authorizer.Check(ctx, testID("tenant-a"), "store_view", testID("principal-a"))
	require.NoError(t, err)
	assert.True(t, allowed)
	allowed, err = authorizer.Check(ctx, testID("tenant-b"), "store_view", testID("principal-a"))
	require.NoError(t, err)
	assert.False(t, allowed)
	allowed, err = authorizer.Check(ctx, testID("tenant-a"), "store_view", testID("principal-b"))
	require.NoError(t, err)
	assert.False(t, allowed)

	revision, err := repo.policyRevision(ctx, testID("tenant-a"))
	require.NoError(t, err)
	assert.Equal(t, int64(5), revision, "idempotent grants must not advance policy revision")
	_, err = service.SetTenantMemberStatus(ctx, testID("tenant-a"), testID("principal-a"), MemberDisabled)
	require.NoError(t, err)
	allowed, err = authorizer.Check(ctx, testID("tenant-a"), "store_view", testID("principal-a"))
	require.NoError(t, err)
	assert.False(t, allowed, "disabled tenant membership must revoke existing role access immediately")
	revision, err = repo.policyRevision(ctx, testID("tenant-a"))
	require.NoError(t, err)
	assert.Equal(t, int64(6), revision)

	auditService := auditmod.NewService(db, newTestIDGenerator())
	integrity, err := auditService.Verify(ctx, testID("tenant-a"))
	require.NoError(t, err)
	assert.True(t, integrity.Valid)
	assert.Equal(t, int64(8), integrity.VerifiedEvents)
	integrity, err = auditService.Verify(ctx, testID("tenant-b"))
	require.NoError(t, err)
	assert.True(t, integrity.Valid)
	assert.Equal(t, int64(1), integrity.VerifiedEvents)
}

func TestServiceRoleAndBindingFlow(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	role, err := svc.CreateRole(ctx, testID("t1"), " Managers ", "desc")
	require.NoError(t, err)
	assert.Equal(t, "Managers", role.Name)
	roles, err := svc.ListRoles(ctx, testID("t1"))
	require.NoError(t, err)
	require.Len(t, roles, 1)
	got, err := svc.GetRole(ctx, testID("t1"), role.ID)
	require.NoError(t, err)
	assert.Equal(t, role.ID, got.ID)

	name := "Operators"
	description := "updated"
	got, err = svc.UpdateRole(ctx, testID("t1"), role.ID, &name, &description)
	require.NoError(t, err)
	assert.Equal(t, name, got.Name)

	changed, err := svc.GrantPermission(ctx, testID("t1"), role.ID, "store_view")
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = svc.GrantPermission(ctx, testID("t1"), role.ID, "store_view")
	require.NoError(t, err)
	assert.False(t, changed)
	codes, err := svc.ListPermissions(ctx, testID("t1"), role.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"store_view"}, codes)
	condition := json.RawMessage(`{"gte":[{"context":"auth.acr"},{"value":2}],"version":1}`)
	grant, changed, err := svc.SetPermissionCondition(ctx, testID("t1"), role.ID, "store_view", condition)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.JSONEq(t, `{"version":1,"gte":[{"context":"auth.acr"},{"value":2}]}`, string(grant.Condition))
	grants, err := svc.ListPermissionGrants(ctx, testID("t1"), role.ID)
	require.NoError(t, err)
	require.Len(t, grants, 1)
	assert.JSONEq(t, string(grant.Condition), string(grants[0].Condition))
	_, changed, err = svc.SetPermissionCondition(ctx, testID("t1"), role.ID, "store_view", condition)
	require.NoError(t, err)
	assert.False(t, changed)

	_, err = svc.PutTenantMember(ctx, testID("t1"), testID("user-sub"), "User", "", 0, MemberActive)
	require.NoError(t, err)
	changed, err = svc.AddMember(ctx, testID("t1"), role.ID, testID("user-sub"))
	require.NoError(t, err)
	assert.True(t, changed)
	members, err := svc.ListMembers(ctx, testID("t1"), role.ID)
	require.NoError(t, err)
	assert.Equal(t, []guid.ID{testID("user-sub")}, members)

	changed, err = svc.RevokePermission(ctx, testID("t1"), role.ID, "store_view")
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = svc.RemoveMember(ctx, testID("t1"), role.ID, testID("user-sub"))
	require.NoError(t, err)
	assert.True(t, changed)
	require.NoError(t, svc.DeleteRole(ctx, testID("t1"), role.ID))
	_, err = svc.GetRole(ctx, testID("t1"), role.ID)
	assert.ErrorIs(t, err, ErrRoleNotFound)
}

func TestServiceValidation(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	role, err := svc.CreateRole(ctx, testID("t1"), "role", "")
	require.NoError(t, err)

	_, err = svc.CreateRole(ctx, 0, "role", "")
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.CreateRole(ctx, testID("t1"), " ", "")
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.CreateRole(ctx, testID("t1"), strings.Repeat("x", 129), "")
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.CreateRole(ctx, testID("t1"), "role2", strings.Repeat("x", 4097))
	assert.ErrorIs(t, err, ErrInvalidArgument)

	_, err = svc.GetRole(ctx, testID("t1"), 0)
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.GetRole(ctx, 0, role.ID)
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.UpdateRole(ctx, testID("t1"), role.ID, nil, nil)
	assert.ErrorIs(t, err, ErrInvalidArgument)
	empty := " "
	_, err = svc.UpdateRole(ctx, testID("t1"), role.ID, &empty, nil)
	assert.ErrorIs(t, err, ErrInvalidArgument)

	_, err = svc.GrantPermission(ctx, testID("t1"), role.ID, "missing_code")
	assert.ErrorIs(t, err, ErrPermissionNotFound)
	_, err = svc.GrantPermission(ctx, testID("t1"), role.ID, "platform_administer")
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, _, err = svc.SetPermissionCondition(ctx, testID("t1"), role.ID, "missing_code", json.RawMessage(`{"version":1,"gte":[]}`))
	assert.ErrorIs(t, err, ErrPermissionNotFound)
	_, _, err = svc.SetPermissionCondition(ctx, testID("t1"), role.ID, "platform_administer", nil)
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, _, err = svc.SetPermissionCondition(ctx, testID("t1"), role.ID, "store_view", json.RawMessage(`{"version":2,"gte":[]}`))
	assert.ErrorIs(t, err, ErrInvalidRolePermissionCondition)
	_, _, err = svc.SetPermissionCondition(ctx, testID("t1"), role.ID, "store_view", nil)
	assert.ErrorIs(t, err, ErrRolePermissionNotGranted)
	for _, action := range svc.PermissionCatalog(ctx) {
		assert.NotEqual(t, "platform", action.Scope)
	}
	_, err = svc.AddMember(ctx, testID("t1"), role.ID, 0)
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.AddMember(ctx, testID("t1"), role.ID, 0)
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = svc.ListPermissions(ctx, testID("t1"), testID("missing"))
	assert.ErrorIs(t, err, ErrRoleNotFound)
	_, err = svc.ListMembers(ctx, testID("t1"), testID("missing"))
	assert.ErrorIs(t, err, ErrRoleNotFound)
	_, err = svc.ListRoles(ctx, 0)
	assert.ErrorIs(t, err, ErrInvalidArgument)
	assert.ErrorIs(t, svc.DeleteRole(ctx, testID("t1"), testID("missing")), ErrRoleNotFound)
	_, err = svc.RevokePermission(ctx, testID("t1"), testID("missing"), "store_view")
	assert.ErrorIs(t, err, ErrRoleNotFound)
	_, err = svc.RemoveMember(ctx, testID("t1"), testID("missing"), testID("u1"))
	assert.ErrorIs(t, err, ErrRoleNotFound)
	longDescription := strings.Repeat("d", 4097)
	_, err = svc.UpdateRole(ctx, testID("t1"), role.ID, nil, &longDescription)
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
