package iam

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/core/extension/spicedbx"
)

func newIAMRepository(t *testing.T) *Repository {
	t.Helper()
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, Migrate(context.Background(), db))
	var id atomic.Int64
	repo := NewRepository(db, func() (string, error) { return fmt.Sprint(id.Add(1)), nil })
	repo.now = func() time.Time { return time.UnixMilli(1_700_000_000_000).UTC() }
	return repo
}

func createTestRole(t *testing.T, repo *Repository, tenant, name string) Role {
	t.Helper()
	role, err := repo.CreateRole(context.Background(), tenant, name, "description")
	require.NoError(t, err)
	return role
}

func TestRepositoryRoleCRUDAndTenantIsolation(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := context.Background()

	role := createTestRole(t, repo, "tenant-a", "Managers")
	assert.Equal(t, "tenant-a", role.TenantID)
	assert.Equal(t, "Managers", role.Name)
	assert.Equal(t, time.UnixMilli(1_700_000_000_000).UTC(), role.CreatedAt)

	_, err := repo.CreateRole(ctx, "tenant-a", "Managers", "duplicate")
	assert.ErrorIs(t, err, ErrRoleNameConflict)
	other := createTestRole(t, repo, "tenant-b", "Managers")
	assert.NotEqual(t, role.ID, other.ID)

	roles, err := repo.ListRoles(ctx, "tenant-a")
	require.NoError(t, err)
	require.Len(t, roles, 1)
	assert.Equal(t, role.ID, roles[0].ID)

	_, err = repo.GetRole(ctx, "tenant-b", role.ID)
	assert.ErrorIs(t, err, ErrRoleNotFound)
	updated, err := repo.UpdateRole(ctx, "tenant-a", role.ID, "Operators", "updated")
	require.NoError(t, err)
	assert.Equal(t, "Operators", updated.Name)
	assert.Equal(t, "updated", updated.Description)
	_, err = repo.UpdateRole(ctx, "tenant-a", "missing", "x", "")
	assert.ErrorIs(t, err, ErrRoleNotFound)
}

func TestRepositoryPermissionAndMemberBindings(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := context.Background()
	role := createTestRole(t, repo, "t1", "role")

	changed, err := repo.GrantPermission(ctx, "t1", role.ID, "store_view")
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = repo.GrantPermission(ctx, "t1", role.ID, "store_view")
	require.NoError(t, err)
	assert.False(t, changed)
	codes, err := repo.ListPermissions(ctx, "t1", role.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"store_view"}, codes)

	changed, err = repo.AddMember(ctx, "t1", role.ID, "zitadel-user")
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = repo.AddMember(ctx, "t1", role.ID, "zitadel-user")
	require.NoError(t, err)
	assert.False(t, changed)
	members, err := repo.ListMembers(ctx, "t1", role.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"zitadel-user"}, members)

	_, err = repo.GrantPermission(ctx, "wrong", role.ID, "store_view")
	assert.ErrorIs(t, err, ErrRoleNotFound)
	_, err = repo.AddMember(ctx, "wrong", role.ID, "u")
	assert.ErrorIs(t, err, ErrRoleNotFound)

	changed, err = repo.RevokePermission(ctx, "t1", role.ID, "store_view")
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = repo.RemoveMember(ctx, "t1", role.ID, "zitadel-user")
	require.NoError(t, err)
	assert.True(t, changed)
	codes, err = repo.ListPermissions(ctx, "t1", role.ID)
	require.NoError(t, err)
	assert.Empty(t, codes)
	members, err = repo.ListMembers(ctx, "t1", role.ID)
	require.NoError(t, err)
	assert.Empty(t, members)
}

func TestRepositoryDeleteRoleEnqueuesRevocations(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := context.Background()
	role := createTestRole(t, repo, "t1", "role")
	_, err := repo.GrantPermission(ctx, "t1", role.ID, "store_view")
	require.NoError(t, err)
	_, err = repo.AddMember(ctx, "t1", role.ID, "u1")
	require.NoError(t, err)

	require.NoError(t, repo.DeleteRole(ctx, "t1", role.ID))
	_, err = repo.GetRole(ctx, "t1", role.ID)
	assert.ErrorIs(t, err, ErrRoleNotFound)

	messages, err := repo.ClaimOutbox(ctx, "worker", OutboxConfig{})
	require.NoError(t, err)
	require.Len(t, messages, 2)
	for _, message := range messages {
		assert.Equal(t, spicedbx.RelationshipDelete, message.Operation)
	}
	assert.ErrorIs(t, repo.DeleteRole(ctx, "t1", role.ID), ErrRoleNotFound)
}

func TestRepositoryIDAndDialectErrors(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := NewRepository(db, func() (string, error) { return "", errors.New("id failed") })
	_, err = repo.CreateRole(context.Background(), "t", "r", "")
	assert.ErrorContains(t, err, "id failed")

	assert.Empty(t, outboxUpsertSQL("oracle"))
	assert.NotEmpty(t, outboxUpsertSQL("mysql"))
	assert.NotEmpty(t, outboxUpsertSQL("postgres"))
	assert.NotEmpty(t, outboxUpsertSQL("sqlite"))
	assert.Panics(t, func() { NewRepository(nil, func() (string, error) { return "1", nil }) })
	assert.Panics(t, func() { NewRepository(db, nil) })
}

func TestRepositoryConflictAndOutboxErrors(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := context.Background()
	first := createTestRole(t, repo, "t1", "first")
	second := createTestRole(t, repo, "t1", "second")
	_, err := repo.UpdateRole(ctx, "t1", second.ID, first.Name, "")
	assert.ErrorIs(t, err, ErrRoleNameConflict)

	repo.nextID = func() (string, error) { return "", errors.New("outbox id failed") }
	_, err = repo.GrantPermission(ctx, "t1", first.ID, "store_view")
	assert.ErrorContains(t, err, "outbox id failed")
	codes, listErr := repo.ListPermissions(ctx, "t1", first.ID)
	require.NoError(t, listErr)
	assert.Empty(t, codes, "business mutation rolls back when outbox enqueue fails")

	repo.nextID = func() (string, error) { return "99", nil }
	repo.dialect = "oracle"
	_, err = repo.AddMember(ctx, "t1", first.ID, "u1")
	assert.ErrorContains(t, err, "unsupported iam database dialect")
	_, _, err = repo.OutboxStatus(ctx, "t1", memberRelationship(first.ID, "missing"))
	assert.Error(t, err)
}

func TestRepositoryReportsClosedDatabaseErrors(t *testing.T) {
	repo := newIAMRepository(t)
	require.NoError(t, repo.db.Close())
	ctx := context.Background()
	_, err := repo.ListRoles(ctx, "t1")
	assert.Error(t, err)
	_, err = repo.UpdateRole(ctx, "t1", "r1", "name", "")
	assert.Error(t, err)
	_, err = repo.ListPermissions(ctx, "t1", "r1")
	assert.Error(t, err)
	_, err = repo.ListMembers(ctx, "t1", "r1")
	assert.Error(t, err)
	_, _, err = repo.OutboxStatus(ctx, "t1", memberRelationship("r1", "u1"))
	assert.Error(t, err)
	message := OutboxMessage{ID: "1", Version: 1}
	assert.Error(t, repo.CompleteOutbox(ctx, message, "worker", "token"))
	assert.Error(t, repo.FailOutbox(ctx, message, "worker", errors.New("fail"), OutboxConfig{}))
}

func TestRelationshipMappingAndKey(t *testing.T) {
	permission := permissionRelationship("t1", "r1", "store_view")
	assert.Equal(t, "tenant:t1#store_view_role@role:r1#member", permission.String())
	member := memberRelationship("r1", "u1")
	assert.Equal(t, "role:r1#member@user:u1", member.String())
	assert.Len(t, relationshipKey("t1", member), 64)
	assert.NotEqual(t, relationshipKey("t1", member), relationshipKey("t2", member))
}
