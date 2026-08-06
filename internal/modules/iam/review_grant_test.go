package iam

import (
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	iamdomain "github.com/chaos-plus/chaosplus/internal/modules/iam/domain"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReviewGrantRevocationsSeverDerivedAccess(t *testing.T) {
	repo := newIAMRepository(t)
	require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, "tenant"))
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	_, err := service.PutTenantMember(t.Context(), "tenant", "principal", "Principal", "", "", iamdomain.MemberActive)
	require.NoError(t, err)
	role := createTestRole(t, repo, "tenant", "Review role")
	now := repo.now().UnixMilli()

	_, err = repo.db.ExecContext(t.Context(), `INSERT INTO iam_groups
		(tenant_id,id,name,name_key,group_type,description,status,sort_order,version,created_at,updated_at)
		VALUES ('tenant','group-a','Group A','group a','static','','active',0,1,?,?)`, now, now)
	require.NoError(t, err)
	_, err = repo.db.ExecContext(t.Context(), `INSERT INTO iam_group_members
		(tenant_id,group_id,principal_id,starts_at,ends_at,created_at,updated_at)
		VALUES ('tenant','group-a','principal',0,0,?,?)`, now, now)
	require.NoError(t, err)
	_, err = repo.db.ExecContext(t.Context(), `INSERT INTO iam_group_role_bindings
		(tenant_id,role_id,group_id,created_at) VALUES ('tenant',?, 'group-a', ?)`, role.ID, now)
	require.NoError(t, err)

	_, err = repo.db.ExecContext(t.Context(), `INSERT INTO iam_positions
		(tenant_id,id,code,name,status,sort_order,version,created_at,updated_at)
		VALUES ('tenant','position-a','pa','Position A','active',0,1,?,?)`, now, now)
	require.NoError(t, err)
	_, err = repo.db.ExecContext(t.Context(), `INSERT INTO iam_position_members
		(tenant_id,position_id,principal_id,starts_at,ends_at,created_at,updated_at)
		VALUES ('tenant','position-a','principal',0,0,?,?)`, now, now)
	require.NoError(t, err)
	_, err = repo.db.ExecContext(t.Context(), `INSERT INTO iam_position_role_bindings
		(tenant_id,role_id,position_id,created_at) VALUES ('tenant',?, 'position-a', ?)`, role.ID, now)
	require.NoError(t, err)

	entity, err := service.CreateEntity(t.Context(), iamdomain.Entity{TenantID: "tenant", Type: "store", Name: "Store"})
	require.NoError(t, err)
	_, err = repo.db.ExecContext(t.Context(), `INSERT INTO iam_role_bindings
		(tenant_id,role_id,principal_id,scope_type,scope_id,effect,expires_at,created_at)
		VALUES ('tenant',?, 'principal', 'entity', ?, 'allow', 0, ?)`, role.ID, entity.ID, now)
	require.NoError(t, err)

	t.Run("group membership", func(t *testing.T) {
		changed, err := RemoveGroupMembership(t.Context(), repo.db, "tenant", "group-a", "principal")
		require.NoError(t, err)
		assert.True(t, changed)
		changed, err = RemoveGroupMembership(t.Context(), repo.db, "tenant", "group-a", "principal")
		require.NoError(t, err)
		assert.False(t, changed)
		count := countRows(t, repo, `SELECT COUNT(*) FROM iam_group_members WHERE tenant_id = 'tenant' AND group_id = 'group-a' AND principal_id = 'principal'`)
		assert.Zero(t, count)
	})

	t.Run("position membership", func(t *testing.T) {
		changed, err := RemovePositionMembership(t.Context(), repo.db, "tenant", "position-a", "principal")
		require.NoError(t, err)
		assert.True(t, changed)
		changed, err = RemovePositionMembership(t.Context(), repo.db, "tenant", "position-a", "principal")
		require.NoError(t, err)
		assert.False(t, changed)
		count := countRows(t, repo, `SELECT COUNT(*) FROM iam_position_members WHERE tenant_id = 'tenant' AND position_id = 'position-a' AND principal_id = 'principal'`)
		assert.Zero(t, count)
	})

	t.Run("entity role binding", func(t *testing.T) {
		changed, err := RemoveEntityRoleBinding(t.Context(), repo.db, "tenant", entity.ID, role.ID, "principal")
		require.NoError(t, err)
		assert.True(t, changed)
		changed, err = RemoveEntityRoleBinding(t.Context(), repo.db, "tenant", entity.ID, role.ID, "principal")
		require.NoError(t, err)
		assert.False(t, changed)
		count := countRows(t, repo, `SELECT COUNT(*) FROM iam_role_bindings WHERE tenant_id = 'tenant' AND scope_type = 'entity' AND scope_id = '`+entity.ID+`'`)
		assert.Zero(t, count)
	})

	t.Run("tenant isolation", func(t *testing.T) {
		changed, err := RemoveGroupMembership(t.Context(), repo.db, "other-tenant", "group-a", "principal")
		require.NoError(t, err)
		assert.False(t, changed)
		changed, err = RemovePositionMembership(t.Context(), repo.db, "tenant", "position-a", "someone-else")
		require.NoError(t, err)
		assert.False(t, changed)
	})
}

func TestReviewGrantRemovalsValidateInputs(t *testing.T) {
	_, err := RemoveGroupMembership(t.Context(), nil, "tenant", "group-a", "principal")
	assert.Error(t, err)
	_, err = RemovePositionMembership(t.Context(), nil, "tenant", "position-a", "principal")
	assert.Error(t, err)
	_, err = RemoveEntityRoleBinding(t.Context(), nil, "tenant", "entity-a", "role-a", "principal")
	assert.Error(t, err)
}

func countRows(t *testing.T, repo *Repository, query string) int {
	t.Helper()
	var count int
	require.NoError(t, repo.db.QueryRowContext(t.Context(), query).Scan(&count))
	return count
}

func TestReviewGrantEntityBindingWindowRespected(t *testing.T) {
	repo := newIAMRepository(t)
	require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, "tenant"))
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	_, err := service.PutTenantMember(t.Context(), "tenant", "principal", "Principal", "", "", iamdomain.MemberActive)
	require.NoError(t, err)
	role := createTestRole(t, repo, "tenant", "Review role")
	entity, err := service.CreateEntity(t.Context(), iamdomain.Entity{TenantID: "tenant", Type: "store", Name: "Store"})
	require.NoError(t, err)
	now := repo.now().UnixMilli()
	_, err = repo.db.ExecContext(t.Context(), `INSERT INTO iam_role_bindings
		(tenant_id,role_id,principal_id,scope_type,scope_id,effect,expires_at,created_at)
		VALUES ('tenant',?, 'principal', 'entity', ?, 'allow', ?, ?)`, role.ID, entity.ID, now+time.Hour.Milliseconds(), now)
	require.NoError(t, err)
	changed, err := RemoveEntityRoleBinding(t.Context(), repo.db, "tenant", entity.ID, role.ID, "principal")
	require.NoError(t, err)
	assert.True(t, changed, "expiring bindings are still revocable by review")
}
