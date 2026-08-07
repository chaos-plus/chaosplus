package iam

import (
	"context"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectoryBindingRepositoryLifecycleAndIsolation(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := t.Context()
	role := createTestRole(t, repo, "tenant-a", "Operators")
	insertDirectory(t, repo, "tenant-a", "group-a", "position-a", "active")
	insertDirectory(t, repo, "tenant-b", "group-b", "position-b", "active")

	changed, err := repo.ChangeDirectoryBinding(ctx, "tenant-a", role.ID, DirectoryAssigneeGroup, "group-a", true)
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = repo.ChangeDirectoryBinding(ctx, "tenant-a", role.ID, DirectoryAssigneeGroup, "group-a", true)
	require.NoError(t, err)
	assert.False(t, changed)
	changed, err = repo.ChangeDirectoryBinding(ctx, "tenant-a", role.ID, DirectoryAssigneePosition, "position-a", true)
	require.NoError(t, err)
	assert.True(t, changed)

	bindings, err := repo.ListDirectoryBindings(ctx, "tenant-a", role.ID)
	require.NoError(t, err)
	require.Len(t, bindings, 2)
	assert.Equal(t, DirectoryAssigneeGroup, bindings[0].AssigneeType)
	assert.Equal(t, "group-a", bindings[0].AssigneeID)
	assert.Equal(t, DirectoryAssigneePosition, bindings[1].AssigneeType)

	_, err = repo.ChangeDirectoryBinding(ctx, "tenant-a", role.ID, DirectoryAssigneeGroup, "group-b", true)
	assert.ErrorIs(t, err, ErrDirectoryAssigneeNotFound)
	_, err = repo.ChangeDirectoryBinding(ctx, "tenant-b", role.ID, DirectoryAssigneeGroup, "group-b", true)
	assert.ErrorIs(t, err, ErrRoleNotFound)
	_, err = repo.ChangeDirectoryBinding(ctx, "tenant-a", role.ID, "team", "group-a", true)
	assert.ErrorIs(t, err, ErrInvalidArgument)

	changed, err = repo.ChangeDirectoryBinding(ctx, "tenant-a", role.ID, DirectoryAssigneeGroup, "group-a", false)
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = repo.ChangeDirectoryBinding(ctx, "tenant-a", role.ID, DirectoryAssigneeGroup, "group-a", false)
	require.NoError(t, err)
	assert.False(t, changed)

	require.NoError(t, repo.DeleteRole(ctx, "tenant-a", role.ID))
	for _, table := range []string{"iam_group_role_bindings", "iam_position_role_bindings"} {
		count, countErr := repo.db.NewSelect().Table(table).Where("tenant_id = ? AND role_id = ?", "tenant-a", role.ID).Count(ctx)
		require.NoError(t, countErr)
		assert.Zero(t, count)
	}
	_, err = repo.ListDirectoryBindings(ctx, "tenant-a", role.ID)
	assert.ErrorIs(t, err, ErrRoleNotFound)
}

func TestDirectoryBindingServiceIdempotencyAndAuditRollback(t *testing.T) {
	repo := newIAMRepository(t)
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	ctx := t.Context()
	role, err := service.CreateRole(ctx, "tenant", "Operators", "")
	require.NoError(t, err)
	insertDirectory(t, repo, "tenant", "group", "position", "active")

	revision, err := repo.policyRevision(ctx, "tenant")
	require.NoError(t, err)
	events := directoryBindingAuditCount(t, repo, "tenant")
	changed, err := service.AddDirectoryBinding(ctx, "tenant", role.ID, DirectoryAssigneeGroup, "group")
	require.NoError(t, err)
	assert.True(t, changed)
	revisionAfterAdd, err := repo.policyRevision(ctx, "tenant")
	require.NoError(t, err)
	assert.Equal(t, revision+1, revisionAfterAdd)
	assert.Equal(t, events+1, directoryBindingAuditCount(t, repo, "tenant"))

	changed, err = service.AddDirectoryBinding(ctx, "tenant", role.ID, DirectoryAssigneeGroup, "group")
	require.NoError(t, err)
	assert.False(t, changed)
	revisionAfterNoop, err := repo.policyRevision(ctx, "tenant")
	require.NoError(t, err)
	assert.Equal(t, revisionAfterAdd, revisionAfterNoop)
	assert.Equal(t, events+1, directoryBindingAuditCount(t, repo, "tenant"))

	_, err = repo.db.ExecContext(ctx, `CREATE TRIGGER reject_directory_binding_audit BEFORE INSERT ON iam_audit_events
		WHEN NEW.event_type = 'role_directory_binding_added' BEGIN SELECT RAISE(ABORT, 'forced audit failure'); END`)
	require.NoError(t, err)
	changed, err = service.AddDirectoryBinding(ctx, "tenant", role.ID, DirectoryAssigneePosition, "position")
	assert.False(t, changed)
	require.Error(t, err)
	count, countErr := repo.db.NewSelect().Table("iam_position_role_bindings").Where("tenant_id = ? AND role_id = ?", "tenant", role.ID).Count(ctx)
	require.NoError(t, countErr)
	assert.Zero(t, count)
	revisionAfterRollback, revisionErr := repo.policyRevision(ctx, "tenant")
	require.NoError(t, revisionErr)
	assert.Equal(t, revisionAfterNoop, revisionAfterRollback)
}

func TestAuthorizerDirectoryBindingsRespectStateAndMembershipWindows(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := t.Context()
	now := repo.now().UTC().UnixMilli()
	putTestMember(t, repo, TenantMember{TenantID: "tenant", Subject: "principal", DisplayName: "Principal", Status: MemberActive})
	var err error
	role := createTestRole(t, repo, "tenant", "Directory viewers")
	_, err = repo.GrantPermission(ctx, "tenant", role.ID, "store_view")
	require.NoError(t, err)
	insertDirectory(t, repo, "tenant", "group", "position", "active")
	_, err = repo.db.ExecContext(ctx, `INSERT INTO iam_group_members
		(tenant_id,group_id,principal_id,starts_at,ends_at,created_at,updated_at) VALUES (?,?,?,?,?,?,?)`,
		"tenant", "group", "principal", now-1000, 0, now, now)
	require.NoError(t, err)
	_, err = repo.db.ExecContext(ctx, `INSERT INTO iam_position_members
		(tenant_id,position_id,principal_id,starts_at,ends_at,created_at,updated_at) VALUES (?,?,?,?,?,?,?)`,
		"tenant", "position", "principal", now-1000, 0, now, now)
	require.NoError(t, err)
	_, err = repo.ChangeDirectoryBinding(ctx, "tenant", role.ID, DirectoryAssigneeGroup, "group", true)
	require.NoError(t, err)

	authorizer := NewAuthorizer(repo.db)
	authorizer.now = repo.now
	allowed, err := authorizer.Check(ctx, "tenant", "store_view", "principal")
	require.NoError(t, err)
	assert.True(t, allowed)

	_, err = repo.db.ExecContext(ctx, "UPDATE iam_groups SET status = 'disabled' WHERE tenant_id = ? AND id = ?", "tenant", "group")
	require.NoError(t, err)
	allowed, err = authorizer.Check(ctx, "tenant", "store_view", "principal")
	require.NoError(t, err)
	assert.False(t, allowed)
	_, err = repo.db.ExecContext(ctx, "UPDATE iam_groups SET status = 'active' WHERE tenant_id = ? AND id = ?", "tenant", "group")
	require.NoError(t, err)
	_, err = repo.db.ExecContext(ctx, "UPDATE iam_group_members SET starts_at = ? WHERE tenant_id = ? AND group_id = ?", now+1, "tenant", "group")
	require.NoError(t, err)
	allowed, err = authorizer.Check(ctx, "tenant", "store_view", "principal")
	require.NoError(t, err)
	assert.False(t, allowed)
	_, err = repo.db.ExecContext(ctx, "UPDATE iam_group_members SET starts_at = 0, ends_at = ? WHERE tenant_id = ? AND group_id = ?", now, "tenant", "group")
	require.NoError(t, err)
	allowed, err = authorizer.Check(ctx, "tenant", "store_view", "principal")
	require.NoError(t, err)
	assert.False(t, allowed)

	_, err = repo.ChangeDirectoryBinding(ctx, "tenant", role.ID, DirectoryAssigneePosition, "position", true)
	require.NoError(t, err)
	allowed, err = authorizer.Check(ctx, "tenant", "store_view", "principal")
	require.NoError(t, err)
	assert.True(t, allowed)
	_, err = repo.SetMemberStatus(ctx, "tenant", "principal", MemberDisabled)
	require.NoError(t, err)
	allowed, err = authorizer.Check(ctx, "tenant", "store_view", "principal")
	require.NoError(t, err)
	assert.False(t, allowed)
}

func TestDirectoryBindingValidationAndInactiveAssignee(t *testing.T) {
	repo := newIAMRepository(t)
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	role := createTestRole(t, repo, "tenant", "Role")
	insertDirectory(t, repo, "tenant", "group", "position", "disabled")
	_, err := service.AddDirectoryBinding(t.Context(), "tenant", role.ID, DirectoryAssigneeGroup, "group")
	assert.ErrorIs(t, err, ErrDirectoryAssigneeInactive)
	_, err = service.AddDirectoryBinding(t.Context(), "tenant", role.ID, "dynamic", "group")
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = service.AddDirectoryBinding(t.Context(), "tenant", role.ID, DirectoryAssigneeGroup, "")
	assert.ErrorIs(t, err, ErrInvalidArgument)
}

func insertDirectory(t *testing.T, repo *Repository, tenantID, groupID, positionID, status string) {
	t.Helper()
	now := repo.now().UTC().UnixMilli()
	_, err := repo.db.ExecContext(context.Background(), `INSERT INTO iam_groups
		(tenant_id,id,name,name_key,group_type,description,status,sort_order,version,created_at,updated_at)
		VALUES (?,?,?,?,?,'',?,0,1,?,?)`, tenantID, groupID, groupID, groupID, "static", status, now, now)
	require.NoError(t, err)
	_, err = repo.db.ExecContext(context.Background(), `INSERT INTO iam_positions
		(tenant_id,id,code,name,status,sort_order,version,created_at,updated_at)
		VALUES (?,?,?,?,?,0,1,?,?)`, tenantID, positionID, positionID, positionID, status, now, now)
	require.NoError(t, err)
}

func directoryBindingAuditCount(t *testing.T, repo *Repository, tenantID string) int {
	t.Helper()
	count, err := repo.db.NewSelect().Table("iam_audit_events").Where("tenant_id = ? AND event_type LIKE 'role_directory_binding_%'", tenantID).Count(t.Context())
	require.NoError(t, err)
	return count
}
