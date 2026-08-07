package iam

import (
	"context"
	"testing"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoleDataScopesCompileDirectAndDirectoryGrants(t *testing.T) {
	repo := newIAMRepository(t)
	require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, "tenant"))
	seedDataScopeDepartments(t, repo)
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	makeTenantAdministrator(t, repo, "tenant", "operator")
	ctx := authnext.WithClaims(t.Context(), &authnext.Claims{Subject: "operator"})

	selfRole := createDataScopeRole(ctx, t, service, "Self", "self-principal", "", DataScopeSelf, nil)
	selfConstraint, err := service.AuthorizationConstraint(ctx, "tenant", "store_view", "self-principal")
	require.NoError(t, err)
	assert.False(t, selfConstraint.AllowAll)
	assert.Equal(t, []string{"self-principal"}, selfConstraint.OwnerIDs)

	departmentRole := createDataScopeRole(ctx, t, service, "Department", "department-principal", "root", DataScopeDepartment, nil)
	departmentConstraint, err := service.AuthorizationConstraint(ctx, "tenant", "store_view", "department-principal")
	require.NoError(t, err)
	assert.Equal(t, []string{"root"}, departmentConstraint.DepartmentIDs)

	groupRole := createDataScopeRole(ctx, t, service, "Descendants", "group-principal", "root", DataScopeDepartmentAndDescendants, nil)
	positionRole := createDataScopeRole(ctx, t, service, "Selected", "position-principal", "", DataScopeSelectedDepartments, []string{"other", "child"})
	insertDirectory(t, repo, "tenant", "group", "position", "active")
	now := repo.now().UTC().UnixMilli()
	_, err = repo.db.ExecContext(ctx, `INSERT INTO iam_group_members
		(tenant_id,group_id,principal_id,starts_at,ends_at,created_at,updated_at) VALUES ('tenant','group','group-principal',0,0,?,?)`, now, now)
	require.NoError(t, err)
	_, err = repo.db.ExecContext(ctx, `INSERT INTO iam_position_members
		(tenant_id,position_id,principal_id,starts_at,ends_at,created_at,updated_at) VALUES ('tenant','position','position-principal',0,0,?,?)`, now, now)
	require.NoError(t, err)
	_, err = service.AddDirectoryBinding(ctx, "tenant", groupRole.ID, DirectoryAssigneeGroup, "group")
	require.NoError(t, err)
	_, err = service.AddDirectoryBinding(ctx, "tenant", positionRole.ID, DirectoryAssigneePosition, "position")
	require.NoError(t, err)
	_, err = service.RemoveMember(ctx, "tenant", groupRole.ID, "group-principal")
	require.NoError(t, err)
	_, err = service.RemoveMember(ctx, "tenant", positionRole.ID, "position-principal")
	require.NoError(t, err)

	groupConstraint, err := service.AuthorizationConstraint(ctx, "tenant", "store_view", "group-principal")
	require.NoError(t, err)
	assert.Equal(t, []string{"child", "root"}, groupConstraint.DepartmentIDs)
	positionConstraint, err := service.AuthorizationConstraint(ctx, "tenant", "store_view", "position-principal")
	require.NoError(t, err)
	assert.Equal(t, []string{"child", "other"}, positionConstraint.DepartmentIDs)

	defaultRole := createDataScopeRole(ctx, t, service, "Default", "default-principal", "", DataScopeAll, nil)
	defaultScope, err := service.GetRoleDataScope(ctx, "tenant", defaultRole.ID)
	require.NoError(t, err)
	assert.Equal(t, DataScopeAll, defaultScope.Scope)
	assert.NotNil(t, defaultScope.DepartmentIDs)
	defaultConstraint, err := service.AuthorizationConstraint(ctx, "tenant", "store_view", "default-principal")
	require.NoError(t, err)
	assert.True(t, defaultConstraint.AllowAll)

	adminRole := createDataScopeRole(ctx, t, service, "Administrator", "admin-principal", "", DataScopeSelf, nil)
	_, err = service.GrantPermission(ctx, "tenant", adminRole.ID, "tenant_administer")
	require.NoError(t, err)
	adminConstraint, err := service.AuthorizationConstraint(ctx, "tenant", "store_view", "admin-principal")
	require.NoError(t, err)
	assert.True(t, adminConstraint.AllowAll)

	for _, role := range []Role{selfRole, departmentRole, groupRole, positionRole, defaultRole, adminRole} {
		allowed, checkErr := NewAuthorizer(repo.db).Check(ctx, "tenant", "store_view", rolePrincipal(role.Name))
		require.NoError(t, checkErr)
		assert.True(t, allowed, role.Name)
	}
}

func TestRoleDataScopeValidationIdempotencyAndMemberAtomicity(t *testing.T) {
	repo := newIAMRepository(t)
	require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, "tenant"))
	seedDataScopeDepartments(t, repo)
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	makeTenantAdministrator(t, repo, "tenant", "operator")
	ctx := authnext.WithClaims(t.Context(), &authnext.Claims{Subject: "operator"})
	role, err := service.CreateRole(ctx, "tenant", "Scoped", "")
	require.NoError(t, err)

	_, _, err = service.SetRoleDataScope(ctx, "tenant", role.ID, DataScopeSelectedDepartments, nil)
	assert.ErrorIs(t, err, ErrRoleScopeDepartmentNeeded)
	_, _, err = service.SetRoleDataScope(ctx, "tenant", role.ID, DataScopeAll, []string{"root"})
	assert.ErrorIs(t, err, ErrRoleScopeDepartmentsExtra)
	_, _, err = service.SetRoleDataScope(ctx, "tenant", role.ID, DataScope("unknown"), nil)
	assert.ErrorIs(t, err, ErrInvalidRoleDataScope)
	_, _, err = service.SetRoleDataScope(ctx, "tenant", role.ID, DataScopeSelectedDepartments, []string{"missing"})
	assert.ErrorIs(t, err, ErrRoleScopeDepartmentMissing)
	_, _, err = service.SetRoleDataScope(ctx, "tenant", role.ID, DataScopeSelectedDepartments, []string{"disabled"})
	assert.ErrorIs(t, err, ErrRoleScopeDepartmentInactive)

	scope, changed, err := service.SetRoleDataScope(ctx, "tenant", role.ID, DataScopeSelectedDepartments, []string{"other", "root"})
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, []string{"other", "root"}, scope.DepartmentIDs)
	revision, err := repo.policyRevision(ctx, "tenant")
	require.NoError(t, err)
	auditCount, err := repo.db.NewSelect().Table("iam_audit_events").Where("tenant_id = ? AND event_type = ? AND target_id = ?", "tenant", "role_data_scope_updated", role.ID).Count(ctx)
	require.NoError(t, err)

	_, changed, err = service.SetRoleDataScope(ctx, "tenant", role.ID, DataScopeSelectedDepartments, []string{"root", "other", "root"})
	require.NoError(t, err)
	assert.False(t, changed)
	sameRevision, err := repo.policyRevision(ctx, "tenant")
	require.NoError(t, err)
	assert.Equal(t, revision, sameRevision)
	sameAuditCount, err := repo.db.NewSelect().Table("iam_audit_events").Where("tenant_id = ? AND event_type = ? AND target_id = ?", "tenant", "role_data_scope_updated", role.ID).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, auditCount, sameAuditCount)

	member, err := service.PutTenantMember(ctx, "tenant", "member", "Member", "", "root", MemberActive)
	require.NoError(t, err)
	assert.Equal(t, "root", member.DepartmentID)
	_, err = service.PutTenantMember(ctx, "tenant", "member", "Changed", "", "missing", MemberActive)
	assert.ErrorIs(t, err, ErrMemberDepartmentMissing)
	stored, err := service.GetTenantMember(ctx, "tenant", "member")
	require.NoError(t, err)
	assert.Equal(t, "Member", stored.DisplayName)
	assert.Equal(t, "root", stored.DepartmentID)
	_, err = service.PutTenantMember(ctx, "tenant", "member", "Changed", "", "disabled", MemberActive)
	assert.ErrorIs(t, err, ErrMemberDepartmentInactive)

	member, err = service.PutTenantMember(ctx, "tenant", "member", "Member", "", "", MemberActive)
	require.NoError(t, err)
	assert.Empty(t, member.DepartmentID)

	scope, changed, err = service.SetRoleDataScope(ctx, "tenant", role.ID, DataScopeAll, nil)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, DataScopeAll, scope.Scope)
	assert.Empty(t, scope.DepartmentIDs)

	_, changed, err = service.SetRoleDataScope(ctx, "tenant", role.ID, DataScopeSelectedDepartments, []string{"other"})
	require.NoError(t, err)
	assert.True(t, changed)
	_, err = repo.db.ExecContext(ctx, "UPDATE iam_departments SET status = 'disabled' WHERE tenant_id = 'tenant' AND id = 'other'")
	require.NoError(t, err)
	_, err = service.GrantPermission(ctx, "tenant", role.ID, "store_view")
	require.NoError(t, err)
	_, err = service.AddMember(ctx, "tenant", role.ID, "member")
	require.NoError(t, err)
	constraint, err := service.AuthorizationConstraint(ctx, "tenant", "store_view", "member")
	require.NoError(t, err)
	assert.Empty(t, constraint.DepartmentIDs)

	_, err = repo.db.ExecContext(ctx, `CREATE TRIGGER reject_role_data_scope_audit BEFORE INSERT ON iam_audit_events
		WHEN NEW.event_type = 'role_data_scope_updated' BEGIN SELECT RAISE(ABORT, 'audit denied'); END`)
	require.NoError(t, err)
	_, _, err = service.SetRoleDataScope(ctx, "tenant", role.ID, DataScopeSelf, nil)
	assert.Error(t, err)
	storedScope, err := service.GetRoleDataScope(ctx, "tenant", role.ID)
	require.NoError(t, err)
	assert.Equal(t, DataScopeSelectedDepartments, storedScope.Scope)
	assert.Equal(t, []string{"other"}, storedScope.DepartmentIDs)
}

func createDataScopeRole(ctx context.Context, t *testing.T, service *Service, name, principal, departmentID string, scope DataScope, departmentIDs []string) Role {
	t.Helper()
	_, err := service.PutTenantMember(ctx, "tenant", principal, principal, "", departmentID, MemberActive)
	require.NoError(t, err)
	role, err := service.CreateRole(ctx, "tenant", name, "")
	require.NoError(t, err)
	_, err = service.GrantPermission(ctx, "tenant", role.ID, "store_view")
	require.NoError(t, err)
	_, err = service.AddMember(ctx, "tenant", role.ID, principal)
	require.NoError(t, err)
	if scope != DataScopeAll {
		_, changed, err := service.SetRoleDataScope(ctx, "tenant", role.ID, scope, departmentIDs)
		require.NoError(t, err)
		assert.True(t, changed)
	}
	return role
}

func seedDataScopeDepartments(t *testing.T, repo *Repository) {
	t.Helper()
	now := repo.now().UTC().UnixMilli()
	for _, row := range []struct{ id, parent, status string }{
		{"root", "", "active"}, {"child", "root", "active"}, {"other", "", "active"}, {"disabled", "", "disabled"},
	} {
		_, err := repo.db.ExecContext(t.Context(), `INSERT INTO iam_departments
			(tenant_id,id,parent_id,name,name_key,status,sort_order,version,created_at,updated_at)
			VALUES ('tenant',?,?,?,?,?,0,1,?,?)`, row.id, row.parent, row.id, row.id, row.status, now, now)
		require.NoError(t, err)
		_, err = repo.db.ExecContext(t.Context(), `INSERT INTO iam_department_closure
			(tenant_id,ancestor_id,descendant_id,depth) VALUES ('tenant',?,?,0)`, row.id, row.id)
		require.NoError(t, err)
	}
	_, err := repo.db.ExecContext(t.Context(), `INSERT INTO iam_department_closure
		(tenant_id,ancestor_id,descendant_id,depth) VALUES ('tenant','root','child',1)`)
	require.NoError(t, err)
}

func rolePrincipal(name string) string {
	switch name {
	case "Self":
		return "self-principal"
	case "Department":
		return "department-principal"
	case "Descendants":
		return "group-principal"
	case "Selected":
		return "position-principal"
	case "Default":
		return "default-principal"
	default:
		return "admin-principal"
	}
}
