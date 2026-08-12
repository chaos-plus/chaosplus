package iam

import (
	"context"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectoryBindingRepositoryLifecycleAndIsolation(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := t.Context()
	role := createTestRole(t, repo, testID("tenant-a"), "Operators")
	insertDirectory(t, repo, testID("tenant-a"), testID("group-a"), testID("position-a"), "active")
	insertDirectory(t, repo, testID("tenant-b"), testID("group-b"), testID("position-b"), "active")

	changed, err := repo.ChangeDirectoryBinding(ctx, testID("tenant-a"), role.ID, DirectoryAssigneeGroup, testID("group-a"), true)
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = repo.ChangeDirectoryBinding(ctx, testID("tenant-a"), role.ID, DirectoryAssigneeGroup, testID("group-a"), true)
	require.NoError(t, err)
	assert.False(t, changed)
	changed, err = repo.ChangeDirectoryBinding(ctx, testID("tenant-a"), role.ID, DirectoryAssigneePosition, testID("position-a"), true)
	require.NoError(t, err)
	assert.True(t, changed)

	bindings, err := repo.ListDirectoryBindings(ctx, testID("tenant-a"), role.ID)
	require.NoError(t, err)
	require.Len(t, bindings, 2)
	assert.Equal(t, DirectoryAssigneeGroup, bindings[0].AssigneeType)
	assert.Equal(t, testID("group-a"), bindings[0].AssigneeID)
	assert.Equal(t, DirectoryAssigneePosition, bindings[1].AssigneeType)

	_, err = repo.ChangeDirectoryBinding(ctx, testID("tenant-a"), role.ID, DirectoryAssigneeGroup, testID("group-b"), true)
	assert.ErrorIs(t, err, ErrDirectoryAssigneeNotFound)
	_, err = repo.ChangeDirectoryBinding(ctx, testID("tenant-b"), role.ID, DirectoryAssigneeGroup, testID("group-b"), true)
	assert.ErrorIs(t, err, ErrRoleNotFound)
	_, err = repo.ChangeDirectoryBinding(ctx, testID("tenant-a"), role.ID, "team", testID("group-a"), true)
	assert.ErrorIs(t, err, ErrInvalidArgument)

	changed, err = repo.ChangeDirectoryBinding(ctx, testID("tenant-a"), role.ID, DirectoryAssigneeGroup, testID("group-a"), false)
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = repo.ChangeDirectoryBinding(ctx, testID("tenant-a"), role.ID, DirectoryAssigneeGroup, testID("group-a"), false)
	require.NoError(t, err)
	assert.False(t, changed)

	require.NoError(t, repo.DeleteRole(ctx, testID("tenant-a"), role.ID))
	for _, table := range []string{"iam_group_role_bindings", "iam_position_role_bindings"} {
		count, countErr := repo.db.NewSelect().Table(table).Where("tenant_id = ? AND role_id = ?", testID("tenant-a"), role.ID).Count(ctx)
		require.NoError(t, countErr)
		assert.Zero(t, count)
	}
	_, err = repo.ListDirectoryBindings(ctx, testID("tenant-a"), role.ID)
	assert.ErrorIs(t, err, ErrRoleNotFound)
}

func TestDirectoryBindingServiceIdempotencyAndAuditRollback(t *testing.T) {
	repo := newIAMRepository(t)
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	ctx := t.Context()
	role, err := service.CreateRole(ctx, testID("tenant"), "Operators", "")
	require.NoError(t, err)
	insertDirectory(t, repo, testID("tenant"), testID("group"), testID("position"), "active")

	revision, err := repo.policyRevision(ctx, testID("tenant"))
	require.NoError(t, err)
	events := directoryBindingAuditCount(t, repo, testID("tenant"))
	changed, err := service.AddDirectoryBinding(ctx, testID("tenant"), role.ID, DirectoryAssigneeGroup, testID("group"))
	require.NoError(t, err)
	assert.True(t, changed)
	revisionAfterAdd, err := repo.policyRevision(ctx, testID("tenant"))
	require.NoError(t, err)
	assert.Equal(t, revision+1, revisionAfterAdd)
	assert.Equal(t, events+1, directoryBindingAuditCount(t, repo, testID("tenant")))

	changed, err = service.AddDirectoryBinding(ctx, testID("tenant"), role.ID, DirectoryAssigneeGroup, testID("group"))
	require.NoError(t, err)
	assert.False(t, changed)
	revisionAfterNoop, err := repo.policyRevision(ctx, testID("tenant"))
	require.NoError(t, err)
	assert.Equal(t, revisionAfterAdd, revisionAfterNoop)
	assert.Equal(t, events+1, directoryBindingAuditCount(t, repo, testID("tenant")))

	_, err = repo.db.ExecContext(ctx, `CREATE TRIGGER reject_directory_binding_audit BEFORE INSERT ON iam_audit_events
		WHEN NEW.event_type = 'role_directory_binding_added' BEGIN SELECT RAISE(ABORT, 'forced audit failure'); END`)
	require.NoError(t, err)
	changed, err = service.AddDirectoryBinding(ctx, testID("tenant"), role.ID, DirectoryAssigneePosition, testID("position"))
	assert.False(t, changed)
	require.Error(t, err)
	count, countErr := repo.db.NewSelect().Table("iam_position_role_bindings").Where("tenant_id = ? AND role_id = ?", testID("tenant"), role.ID).Count(ctx)
	require.NoError(t, countErr)
	assert.Zero(t, count)
	revisionAfterRollback, revisionErr := repo.policyRevision(ctx, testID("tenant"))
	require.NoError(t, revisionErr)
	assert.Equal(t, revisionAfterNoop, revisionAfterRollback)
}

func TestAuthorizerDirectoryBindingsRespectStateAndMembershipWindows(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := t.Context()
	now := repo.now().UTC().UnixMilli()
	putTestMember(t, repo, TenantMember{TenantID: testID("tenant"), PrincipalID: testID("principal"), DisplayName: "Principal", Status: MemberActive})
	var err error
	role := createTestRole(t, repo, testID("tenant"), "Directory viewers")
	_, err = repo.GrantPermission(ctx, testID("tenant"), role.ID, "store_view")
	require.NoError(t, err)
	insertDirectory(t, repo, testID("tenant"), testID("group"), testID("position"), "active")
	_, err = repo.db.ExecContext(ctx, `INSERT INTO iam_group_members
		(tenant_id,group_id,principal_id,starts_at,ends_at,created_at,updated_at) VALUES (?,?,?,?,?,?,?)`,
		testID("tenant"), testID("group"), testID("principal"), now-1000, 0, now, now)
	require.NoError(t, err)
	_, err = repo.db.ExecContext(ctx, `INSERT INTO iam_position_members
		(tenant_id,position_id,principal_id,starts_at,ends_at,created_at,updated_at) VALUES (?,?,?,?,?,?,?)`,
		testID("tenant"), testID("position"), testID("principal"), now-1000, 0, now, now)
	require.NoError(t, err)
	_, err = repo.ChangeDirectoryBinding(ctx, testID("tenant"), role.ID, DirectoryAssigneeGroup, testID("group"), true)
	require.NoError(t, err)

	authorizer := NewAuthorizer(repo.db)
	authorizer.now = repo.now
	allowed, err := authorizer.Check(ctx, testID("tenant"), "store_view", testID("principal"))
	require.NoError(t, err)
	assert.True(t, allowed)

	_, err = repo.db.ExecContext(ctx, "UPDATE iam_groups SET status = 'disabled' WHERE tenant_id = ? AND id = ?", testID("tenant"), testID("group"))
	require.NoError(t, err)
	allowed, err = authorizer.Check(ctx, testID("tenant"), "store_view", testID("principal"))
	require.NoError(t, err)
	assert.False(t, allowed)
	_, err = repo.db.ExecContext(ctx, "UPDATE iam_groups SET status = 'active' WHERE tenant_id = ? AND id = ?", testID("tenant"), testID("group"))
	require.NoError(t, err)
	_, err = repo.db.ExecContext(ctx, "UPDATE iam_group_members SET starts_at = ? WHERE tenant_id = ? AND group_id = ?", now+1, testID("tenant"), testID("group"))
	require.NoError(t, err)
	allowed, err = authorizer.Check(ctx, testID("tenant"), "store_view", testID("principal"))
	require.NoError(t, err)
	assert.False(t, allowed)
	_, err = repo.db.ExecContext(ctx, "UPDATE iam_group_members SET starts_at = 0, ends_at = ? WHERE tenant_id = ? AND group_id = ?", now, testID("tenant"), testID("group"))
	require.NoError(t, err)
	allowed, err = authorizer.Check(ctx, testID("tenant"), "store_view", testID("principal"))
	require.NoError(t, err)
	assert.False(t, allowed)

	_, err = repo.ChangeDirectoryBinding(ctx, testID("tenant"), role.ID, DirectoryAssigneePosition, testID("position"), true)
	require.NoError(t, err)
	allowed, err = authorizer.Check(ctx, testID("tenant"), "store_view", testID("principal"))
	require.NoError(t, err)
	assert.True(t, allowed)
	_, err = repo.SetMemberStatus(ctx, testID("tenant"), testID("principal"), MemberDisabled)
	require.NoError(t, err)
	allowed, err = authorizer.Check(ctx, testID("tenant"), "store_view", testID("principal"))
	require.NoError(t, err)
	assert.False(t, allowed)
}

func TestDirectoryBindingValidationAndInactiveAssignee(t *testing.T) {
	repo := newIAMRepository(t)
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	role := createTestRole(t, repo, testID("tenant"), "Role")
	insertDirectory(t, repo, testID("tenant"), testID("group"), testID("position"), "disabled")
	_, err := service.AddDirectoryBinding(t.Context(), testID("tenant"), role.ID, DirectoryAssigneeGroup, testID("group"))
	assert.ErrorIs(t, err, ErrDirectoryAssigneeInactive)
	_, err = service.AddDirectoryBinding(t.Context(), testID("tenant"), role.ID, "dynamic", testID("group"))
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = service.AddDirectoryBinding(t.Context(), testID("tenant"), role.ID, DirectoryAssigneeGroup, 0)
	assert.ErrorIs(t, err, ErrInvalidArgument)
}

func insertDirectory(t *testing.T, repo *Repository, tenantID, groupID, positionID guid.ID, status string) {
	t.Helper()
	now := repo.now().UTC().UnixMilli()
	_, err := repo.db.ExecContext(context.Background(), `INSERT INTO iam_groups
		(tenant_id,id,name,name_key,group_type,description,status,sort_order,version,created_at,updated_at)
		VALUES (?,?,?,?,?,'',?,0,1,?,?)`, tenantID, groupID, groupID.String(), groupID.String(), "static", status, now, now)
	require.NoError(t, err)
	_, err = repo.db.ExecContext(context.Background(), `INSERT INTO iam_positions
		(tenant_id,id,code,name,status,sort_order,version,created_at,updated_at)
		VALUES (?,?,?,?,?,0,1,?,?)`, tenantID, positionID, positionID.String(), positionID.String(), status, now, now)
	require.NoError(t, err)
}

func directoryBindingAuditCount(t *testing.T, repo *Repository, tenantID guid.ID) int {
	t.Helper()
	count, err := repo.db.NewSelect().Table("iam_audit_events").Where("tenant_id = ? AND event_type LIKE 'role_directory_binding_%'", tenantID).Count(t.Context())
	require.NoError(t, err)
	return count
}
