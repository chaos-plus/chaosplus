package iam

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	iamdomain "github.com/chaos-plus/chaosplus/internal/modules/iam/domain"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
)

// makeTenantAdministrator seeds an actor that genuinely holds tenant_administer,
// which is what every authenticated administration request looks like in
// production. Repository writes are used so the seeding itself is not subject
// to the escalation guard under test.
func makeTenantAdministrator(t *testing.T, repo *Repository, tenantID, subject string) Role {
	t.Helper()
	require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, tenantID))
	_, err := repo.PutMember(t.Context(), TenantMember{TenantID: tenantID, Subject: subject, DisplayName: subject, Status: MemberActive})
	require.NoError(t, err)
	role, err := repo.CreateRole(t.Context(), tenantID, "Administrators "+subject, "")
	require.NoError(t, err)
	_, err = repo.GrantPermission(t.Context(), tenantID, role.ID, "tenant_administer")
	require.NoError(t, err)
	_, err = repo.AddMember(t.Context(), tenantID, role.ID, subject)
	require.NoError(t, err)
	return role
}

// grantViaRepository attaches a permission without going through the service,
// so a test can build an actor that holds strictly less than it tries to grant.
func grantViaRepository(t *testing.T, repo *Repository, tenantID, subject, code string) Role {
	t.Helper()
	require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, tenantID))
	_, err := repo.PutMember(t.Context(), TenantMember{TenantID: tenantID, Subject: subject, DisplayName: subject, Status: MemberActive})
	require.NoError(t, err)
	role, err := repo.CreateRole(t.Context(), tenantID, "Limited "+subject+" "+code, "")
	require.NoError(t, err)
	_, err = repo.GrantPermission(t.Context(), tenantID, role.ID, code)
	require.NoError(t, err)
	_, err = repo.AddMember(t.Context(), tenantID, role.ID, subject)
	require.NoError(t, err)
	return role
}

func TestGrantPermissionRefusesEscalationBeyondActorGrants(t *testing.T) {
	repo := newIAMRepository(t)
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	// The actor may manage roles but holds only store_view itself.
	grantViaRepository(t, repo, "tenant", "limited", "role_grant_permission")
	limited := grantViaRepository(t, repo, "tenant", "limited", "store_view")
	ctx := authnext.WithClaims(t.Context(), &authnext.Claims{Subject: "limited"})

	// Handing itself tenant_administer is the classic takeover path.
	_, err := service.GrantPermission(ctx, "tenant", limited.ID, "tenant_administer")
	assert.ErrorIs(t, err, ErrPrivilegeEscalation)
	_, err = service.GrantPermission(ctx, "tenant", limited.ID, "user_delete")
	assert.ErrorIs(t, err, ErrPrivilegeEscalation)

	// A permission the actor does hold stays grantable.
	changed, err := service.GrantPermission(ctx, "tenant", limited.ID, "store_view")
	require.NoError(t, err)
	assert.False(t, changed, "store_view is already granted to this role")

	// Revocation narrows access and is never blocked by the guard.
	_, err = service.RevokePermission(ctx, "tenant", limited.ID, "store_view")
	require.NoError(t, err)
}

func TestGrantPermissionAllowsTenantAdministrator(t *testing.T) {
	repo := newIAMRepository(t)
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	makeTenantAdministrator(t, repo, "tenant", "root")
	target, err := repo.CreateRole(t.Context(), "tenant", "Target", "")
	require.NoError(t, err)
	ctx := authnext.WithClaims(t.Context(), &authnext.Claims{Subject: "root"})

	for _, code := range []string{"user_delete", "store_view", "tenant_administer"} {
		changed, err := service.GrantPermission(ctx, "tenant", target.ID, code)
		require.NoError(t, err, code)
		assert.True(t, changed, code)
	}
}

func TestAddMemberRefusesEscalationThroughRolePermissions(t *testing.T) {
	repo := newIAMRepository(t)
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	grantViaRepository(t, repo, "tenant", "limited", "role_manage_member")
	privileged, err := repo.CreateRole(t.Context(), "tenant", "Privileged", "")
	require.NoError(t, err)
	_, err = repo.GrantPermission(t.Context(), "tenant", privileged.ID, "tenant_administer")
	require.NoError(t, err)
	ctx := authnext.WithClaims(t.Context(), &authnext.Claims{Subject: "limited"})

	// Adding itself to an administrator role is the same takeover by another door.
	_, err = service.AddMember(ctx, "tenant", privileged.ID, "limited")
	assert.ErrorIs(t, err, ErrPrivilegeEscalation)

	// Removal is unrestricted.
	_, err = service.RemoveMember(ctx, "tenant", privileged.ID, "limited")
	require.NoError(t, err)
}

func TestDirectoryBindingAndEntityBindingRefuseEscalation(t *testing.T) {
	repo := newIAMRepository(t)
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	grantViaRepository(t, repo, "tenant", "limited", "role_manage_assignee")
	grantViaRepository(t, repo, "tenant", "limited", "entity_manage_binding")
	privileged, err := repo.CreateRole(t.Context(), "tenant", "Privileged", "")
	require.NoError(t, err)
	_, err = repo.GrantPermission(t.Context(), "tenant", privileged.ID, "user_delete")
	require.NoError(t, err)
	entity, err := repo.CreateEntity(t.Context(), iamdomain.Entity{TenantID: "tenant", Type: "company", Name: "HQ", Status: iamdomain.EntityActive})
	require.NoError(t, err)
	ctx := authnext.WithClaims(t.Context(), &authnext.Claims{Subject: "limited"})

	_, err = service.AddDirectoryBinding(ctx, "tenant", privileged.ID, DirectoryAssigneeGroup, "group-1")
	assert.ErrorIs(t, err, ErrPrivilegeEscalation)
	_, _, err = service.PutEntityRoleBinding(ctx, "tenant", entity.ID, privileged.ID, "limited", iamdomain.BindingAllow, time.Time{})
	assert.ErrorIs(t, err, ErrPrivilegeEscalation)

	// A deny binding only narrows access, so it must get past the guard and be
	// judged on its own merits instead.
	_, _, err = service.PutEntityRoleBinding(ctx, "tenant", entity.ID, privileged.ID, "limited", iamdomain.BindingDeny, time.Time{})
	assert.NotErrorIs(t, err, ErrPrivilegeEscalation)
}

func TestSetPermissionConditionRefusesEscalation(t *testing.T) {
	repo := newIAMRepository(t)
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	grantViaRepository(t, repo, "tenant", "limited", "role_grant_permission")
	privileged, err := repo.CreateRole(t.Context(), "tenant", "Privileged", "")
	require.NoError(t, err)
	_, err = repo.GrantPermission(t.Context(), "tenant", privileged.ID, "user_delete")
	require.NoError(t, err)
	ctx := authnext.WithClaims(t.Context(), &authnext.Claims{Subject: "limited"})

	_, _, err = service.SetPermissionCondition(ctx, "tenant", privileged.ID, "user_delete", nil)
	assert.ErrorIs(t, err, ErrPrivilegeEscalation)
}

func TestEscalationGuardExemptsSystemFlows(t *testing.T) {
	repo := newIAMRepository(t)
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, "tenant"))
	role, err := repo.CreateRole(t.Context(), "tenant", "Bootstrap", "")
	require.NoError(t, err)

	// Deployment bootstrap runs before any principal exists and carries no
	// claims, so it must remain able to seed the first administrator role.
	changed, err := service.GrantPermission(t.Context(), "tenant", role.ID, "tenant_administer")
	require.NoError(t, err)
	assert.True(t, changed)
	require.NoError(t, service.requireGrantablePermissions(t.Context(), "tenant", "user_delete"))
	require.NoError(t, service.requireGrantableRole(t.Context(), "tenant", role.ID))
}

func TestRequireGrantablePermissionsIgnoresEmptyCodeSets(t *testing.T) {
	repo := newIAMRepository(t)
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	makeTenantAdministrator(t, repo, "tenant", "root")
	ctx := authnext.WithClaims(t.Context(), &authnext.Claims{Subject: "root"})

	require.NoError(t, service.requireGrantablePermissions(ctx, "tenant"))
	require.NoError(t, service.requireGrantablePermissions(ctx, "tenant", "", ""))
	// A role with no permissions confers nothing, so it is always assignable.
	empty, err := repo.CreateRole(t.Context(), "tenant", "Empty", "")
	require.NoError(t, err)
	require.NoError(t, service.requireGrantableRole(ctx, "tenant", empty.ID))
}
