package iam

import (
	"context"
	"testing"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoleDataScopesCompileDirectAndDirectoryGrants(t *testing.T) {
	repo := newIAMRepository(t)
	require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, testID("tenant")))
	seedDataScopeDepartments(t, repo)
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	makeTenantAdministrator(t, repo, testID("tenant"), testID("operator"))
	ctx := authnext.WithClaims(t.Context(), &authnext.Claims{Subject: "operator", PrincipalID: testID("operator")})

	selfRole := createDataScopeRole(ctx, t, service, "Self", testID("self-principal"), 0, DataScopeSelf, nil)
	selfConstraint, err := service.AuthorizationConstraint(ctx, testID("tenant"), "store_view", testID("self-principal"))
	require.NoError(t, err)
	assert.False(t, selfConstraint.AllowAll)
	assert.Equal(t, []string{testID("self-principal").String()}, selfConstraint.OwnerIDs)

	departmentRole := createDataScopeRole(ctx, t, service, "Department", testID("department-principal"), testID("root"), DataScopeDepartment, nil)
	departmentConstraint, err := service.AuthorizationConstraint(ctx, testID("tenant"), "store_view", testID("department-principal"))
	require.NoError(t, err)
	assert.Equal(t, []string{testID("root").String()}, departmentConstraint.DepartmentIDs)

	groupRole := createDataScopeRole(ctx, t, service, "Descendants", testID("group-principal"), testID("root"), DataScopeDepartmentAndDescendants, nil)
	positionRole := createDataScopeRole(ctx, t, service, "Selected", testID("position-principal"), 0, DataScopeSelectedDepartments, []guid.ID{testID("other"), testID("child")})
	insertDirectory(t, repo, testID("tenant"), testID("group"), testID("position"), "active")
	now := repo.now().UTC().UnixMilli()
	_, err = repo.db.ExecContext(ctx, `INSERT INTO iam_group_members
		(tenant_id,group_id,principal_id,starts_at,ends_at,created_at,updated_at) VALUES (?,?,?,0,0,?,?)`, testID("tenant"), testID("group"), testID("group-principal"), now, now)
	require.NoError(t, err)
	_, err = repo.db.ExecContext(ctx, `INSERT INTO iam_position_members
		(tenant_id,position_id,principal_id,starts_at,ends_at,created_at,updated_at) VALUES (?,?,?,0,0,?,?)`, testID("tenant"), testID("position"), testID("position-principal"), now, now)
	require.NoError(t, err)
	_, err = service.AddDirectoryBinding(ctx, testID("tenant"), groupRole.ID, DirectoryAssigneeGroup, testID("group"))
	require.NoError(t, err)
	_, err = service.AddDirectoryBinding(ctx, testID("tenant"), positionRole.ID, DirectoryAssigneePosition, testID("position"))
	require.NoError(t, err)
	_, err = service.RemoveMember(ctx, testID("tenant"), groupRole.ID, testID("group-principal"))
	require.NoError(t, err)
	_, err = service.RemoveMember(ctx, testID("tenant"), positionRole.ID, testID("position-principal"))
	require.NoError(t, err)

	groupConstraint, err := service.AuthorizationConstraint(ctx, testID("tenant"), "store_view", testID("group-principal"))
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{testID("child").String(), testID("root").String()}, groupConstraint.DepartmentIDs)
	positionConstraint, err := service.AuthorizationConstraint(ctx, testID("tenant"), "store_view", testID("position-principal"))
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{testID("child").String(), testID("other").String()}, positionConstraint.DepartmentIDs)

	defaultRole := createDataScopeRole(ctx, t, service, "Default", testID("default-principal"), 0, DataScopeAll, nil)
	defaultScope, err := service.GetRoleDataScope(ctx, testID("tenant"), defaultRole.ID)
	require.NoError(t, err)
	assert.Equal(t, DataScopeAll, defaultScope.Scope)
	assert.NotNil(t, defaultScope.DepartmentIDs)
	defaultConstraint, err := service.AuthorizationConstraint(ctx, testID("tenant"), "store_view", testID("default-principal"))
	require.NoError(t, err)
	assert.True(t, defaultConstraint.AllowAll)

	adminRole := createDataScopeRole(ctx, t, service, "Administrator", testID("admin-principal"), 0, DataScopeSelf, nil)
	_, err = service.GrantPermission(ctx, testID("tenant"), adminRole.ID, "tenant_administer")
	require.NoError(t, err)
	adminConstraint, err := service.AuthorizationConstraint(ctx, testID("tenant"), "store_view", testID("admin-principal"))
	require.NoError(t, err)
	assert.True(t, adminConstraint.AllowAll)

	for _, role := range []Role{selfRole, departmentRole, groupRole, positionRole, defaultRole, adminRole} {
		allowed, checkErr := NewAuthorizer(repo.db).Check(ctx, testID("tenant"), "store_view", rolePrincipal(role.Name))
		require.NoError(t, checkErr)
		assert.True(t, allowed, role.Name)
	}
}

func TestRoleDataScopeValidationIdempotencyAndMemberAtomicity(t *testing.T) {
	repo := newIAMRepository(t)
	require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, testID("tenant")))
	seedDataScopeDepartments(t, repo)
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	makeTenantAdministrator(t, repo, testID("tenant"), testID("operator"))
	ctx := authnext.WithClaims(t.Context(), &authnext.Claims{Subject: "operator", PrincipalID: testID("operator")})
	role, err := service.CreateRole(ctx, testID("tenant"), "Scoped", "")
	require.NoError(t, err)

	_, _, err = service.SetRoleDataScope(ctx, testID("tenant"), role.ID, DataScopeSelectedDepartments, nil)
	assert.ErrorIs(t, err, ErrRoleScopeDepartmentNeeded)
	_, _, err = service.SetRoleDataScope(ctx, testID("tenant"), role.ID, DataScopeAll, []guid.ID{testID("root")})
	assert.ErrorIs(t, err, ErrRoleScopeDepartmentsExtra)
	_, _, err = service.SetRoleDataScope(ctx, testID("tenant"), role.ID, DataScope("unknown"), nil)
	assert.ErrorIs(t, err, ErrInvalidRoleDataScope)
	_, _, err = service.SetRoleDataScope(ctx, testID("tenant"), role.ID, DataScopeSelectedDepartments, []guid.ID{testID("missing")})
	assert.ErrorIs(t, err, ErrRoleScopeDepartmentMissing)
	_, _, err = service.SetRoleDataScope(ctx, testID("tenant"), role.ID, DataScopeSelectedDepartments, []guid.ID{testID("disabled")})
	assert.ErrorIs(t, err, ErrRoleScopeDepartmentInactive)

	scope, changed, err := service.SetRoleDataScope(ctx, testID("tenant"), role.ID, DataScopeSelectedDepartments, []guid.ID{testID("other"), testID("root")})
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, []guid.ID{testID("other"), testID("root")}, scope.DepartmentIDs)
	revision, err := repo.policyRevision(ctx, testID("tenant"))
	require.NoError(t, err)
	auditCount, err := repo.db.NewSelect().Table("iam_audit_events").Where("tenant_id = ? AND event_type = ? AND target_id = ?", testID("tenant"), "role_data_scope_updated", role.ID).Count(ctx)
	require.NoError(t, err)

	_, changed, err = service.SetRoleDataScope(ctx, testID("tenant"), role.ID, DataScopeSelectedDepartments, []guid.ID{testID("root"), testID("other"), testID("root")})
	require.NoError(t, err)
	assert.False(t, changed)
	sameRevision, err := repo.policyRevision(ctx, testID("tenant"))
	require.NoError(t, err)
	assert.Equal(t, revision, sameRevision)
	sameAuditCount, err := repo.db.NewSelect().Table("iam_audit_events").Where("tenant_id = ? AND event_type = ? AND target_id = ?", testID("tenant"), "role_data_scope_updated", role.ID).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, auditCount, sameAuditCount)

	member, err := service.PutTenantMember(ctx, testID("tenant"), testID("member"), "Member", "", testID("root"), MemberActive)
	require.NoError(t, err)
	assert.Equal(t, testID("root"), member.DepartmentID)
	_, err = service.PutTenantMember(ctx, testID("tenant"), testID("member"), "Changed", "", testID("missing"), MemberActive)
	assert.ErrorIs(t, err, ErrMemberDepartmentMissing)
	stored, err := service.GetTenantMember(ctx, testID("tenant"), testID("member"))
	require.NoError(t, err)
	assert.Equal(t, "Member", stored.DisplayName)
	assert.Equal(t, testID("root"), stored.DepartmentID)
	_, err = service.PutTenantMember(ctx, testID("tenant"), testID("member"), "Changed", "", testID("disabled"), MemberActive)
	assert.ErrorIs(t, err, ErrMemberDepartmentInactive)

	member, err = service.PutTenantMember(ctx, testID("tenant"), testID("member"), "Member", "", 0, MemberActive)
	require.NoError(t, err)
	assert.Empty(t, member.DepartmentID)

	scope, changed, err = service.SetRoleDataScope(ctx, testID("tenant"), role.ID, DataScopeAll, nil)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, DataScopeAll, scope.Scope)
	assert.Empty(t, scope.DepartmentIDs)

	_, changed, err = service.SetRoleDataScope(ctx, testID("tenant"), role.ID, DataScopeSelectedDepartments, []guid.ID{testID("other")})
	require.NoError(t, err)
	assert.True(t, changed)
	_, err = repo.db.ExecContext(ctx, "UPDATE iam_departments SET status = 'disabled' WHERE tenant_id = ? AND id = ?", testID("tenant"), testID("other"))
	require.NoError(t, err)
	_, err = service.GrantPermission(ctx, testID("tenant"), role.ID, "store_view")
	require.NoError(t, err)
	_, err = service.AddMember(ctx, testID("tenant"), role.ID, testID("member"))
	require.NoError(t, err)
	constraint, err := service.AuthorizationConstraint(ctx, testID("tenant"), "store_view", testID("member"))
	require.NoError(t, err)
	assert.Empty(t, constraint.DepartmentIDs)

	_, err = repo.db.ExecContext(ctx, `CREATE TRIGGER reject_role_data_scope_audit BEFORE INSERT ON iam_audit_events
		WHEN NEW.event_type = 'role_data_scope_updated' BEGIN SELECT RAISE(ABORT, 'audit denied'); END`)
	require.NoError(t, err)
	_, _, err = service.SetRoleDataScope(ctx, testID("tenant"), role.ID, DataScopeSelf, nil)
	assert.Error(t, err)
	storedScope, err := service.GetRoleDataScope(ctx, testID("tenant"), role.ID)
	require.NoError(t, err)
	assert.Equal(t, DataScopeSelectedDepartments, storedScope.Scope)
	assert.Equal(t, []guid.ID{testID("other")}, storedScope.DepartmentIDs)
}

func createDataScopeRole(ctx context.Context, t *testing.T, service *Service, name string, principal, departmentID guid.ID, scope DataScope, departmentIDs []guid.ID) Role {
	t.Helper()
	_, err := service.PutTenantMember(ctx, testID("tenant"), principal, principal.String(), "", departmentID, MemberActive)
	require.NoError(t, err)
	role, err := service.CreateRole(ctx, testID("tenant"), name, "")
	require.NoError(t, err)
	_, err = service.GrantPermission(ctx, testID("tenant"), role.ID, "store_view")
	require.NoError(t, err)
	_, err = service.AddMember(ctx, testID("tenant"), role.ID, principal)
	require.NoError(t, err)
	if scope != DataScopeAll {
		_, changed, err := service.SetRoleDataScope(ctx, testID("tenant"), role.ID, scope, departmentIDs)
		require.NoError(t, err)
		assert.True(t, changed)
	}
	return role
}

func seedDataScopeDepartments(t *testing.T, repo *Repository) {
	t.Helper()
	now := repo.now().UTC().UnixMilli()
	for _, row := range []struct {
		id, parent guid.ID
		status     string
	}{
		{testID("root"), 0, "active"}, {testID("child"), testID("root"), "active"}, {testID("other"), 0, "active"}, {testID("disabled"), 0, "disabled"},
	} {
		_, err := repo.db.ExecContext(t.Context(), `INSERT INTO iam_departments
			(tenant_id,id,parent_id,name,name_key,status,sort_order,version,created_at,updated_at)
			VALUES (?,?,?,?,?,?,0,1,?,?)`, testID("tenant"), row.id, row.parent, row.id.String(), row.id.String(), row.status, now, now)
		require.NoError(t, err)
		_, err = repo.db.ExecContext(t.Context(), `INSERT INTO iam_department_closure
			(tenant_id,ancestor_id,descendant_id,depth) VALUES (?,?,?,0)`, testID("tenant"), row.id, row.id)
		require.NoError(t, err)
	}
	_, err := repo.db.ExecContext(t.Context(), `INSERT INTO iam_department_closure
		(tenant_id,ancestor_id,descendant_id,depth) VALUES (?,?,?,1)`, testID("tenant"), testID("root"), testID("child"))
	require.NoError(t, err)
}

func rolePrincipal(name string) guid.ID {
	switch name {
	case "Self":
		return testID("self-principal")
	case "Department":
		return testID("department-principal")
	case "Descendants":
		return testID("group-principal")
	case "Selected":
		return testID("position-principal")
	case "Default":
		return testID("default-principal")
	default:
		return testID("admin-principal")
	}
}
