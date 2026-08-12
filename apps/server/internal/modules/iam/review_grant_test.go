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
	require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, testID("tenant")))
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	_, err := service.PutTenantMember(t.Context(), testID("tenant"), testID("principal"), "Principal", "", 0, iamdomain.MemberActive)
	require.NoError(t, err)
	role := createTestRole(t, repo, testID("tenant"), "Review role")
	now := repo.now().UnixMilli()

	_, err = repo.db.ExecContext(t.Context(), `INSERT INTO iam_groups
		(tenant_id,id,name,name_key,group_type,description,status,sort_order,version,created_at,updated_at)
		VALUES (?,?,?,'group a','static','','active',0,1,?,?)`, testID("tenant"), testID("group-a"), "Group A", now, now)
	require.NoError(t, err)
	_, err = repo.db.ExecContext(t.Context(), `INSERT INTO iam_group_members
		(tenant_id,group_id,principal_id,starts_at,ends_at,created_at,updated_at)
		VALUES (?,?,?,0,0,?,?)`, testID("tenant"), testID("group-a"), testID("principal"), now, now)
	require.NoError(t, err)
	_, err = repo.db.ExecContext(t.Context(), `INSERT INTO iam_group_role_bindings
		(tenant_id,role_id,group_id,created_at) VALUES (?,?,?,?)`, testID("tenant"), role.ID, testID("group-a"), now)
	require.NoError(t, err)

	_, err = repo.db.ExecContext(t.Context(), `INSERT INTO iam_positions
		(tenant_id,id,code,name,status,sort_order,version,created_at,updated_at)
		VALUES (?,?,?,'Position A','active',0,1,?,?)`, testID("tenant"), testID("position-a"), "pa", now, now)
	require.NoError(t, err)
	_, err = repo.db.ExecContext(t.Context(), `INSERT INTO iam_position_members
		(tenant_id,position_id,principal_id,starts_at,ends_at,created_at,updated_at)
		VALUES (?,?,?,0,0,?,?)`, testID("tenant"), testID("position-a"), testID("principal"), now, now)
	require.NoError(t, err)
	_, err = repo.db.ExecContext(t.Context(), `INSERT INTO iam_position_role_bindings
		(tenant_id,role_id,position_id,created_at) VALUES (?,?,?,?)`, testID("tenant"), role.ID, testID("position-a"), now)
	require.NoError(t, err)

	entity, err := service.CreateEntity(t.Context(), iamdomain.Entity{TenantID: testID("tenant"), Type: "store", Name: "Store"})
	require.NoError(t, err)
	_, err = repo.db.ExecContext(t.Context(), `INSERT INTO iam_role_bindings
		(tenant_id,role_id,principal_id,scope_type,scope_id,effect,expires_at,created_at)
		VALUES (?,?,?, 'entity', ?, 'allow', 0, ?)`, testID("tenant"), role.ID, testID("principal"), entity.ID, now)
	require.NoError(t, err)

	t.Run("group membership", func(t *testing.T) {
		changed, err := RemoveGroupMembership(t.Context(), repo.db, testID("tenant"), testID("group-a"), testID("principal"))
		require.NoError(t, err)
		assert.True(t, changed)
		changed, err = RemoveGroupMembership(t.Context(), repo.db, testID("tenant"), testID("group-a"), testID("principal"))
		require.NoError(t, err)
		assert.False(t, changed)
		count := countRows(t, repo, `SELECT COUNT(*) FROM iam_group_members WHERE tenant_id = 'testID("tenant").String()' AND group_id = 'testID("group-a").String()' AND principal_id = 'testID("principal").String()'`)
		assert.Zero(t, count)
	})

	t.Run("position membership", func(t *testing.T) {
		changed, err := RemovePositionMembership(t.Context(), repo.db, testID("tenant"), testID("position-a"), testID("principal"))
		require.NoError(t, err)
		assert.True(t, changed)
		changed, err = RemovePositionMembership(t.Context(), repo.db, testID("tenant"), testID("position-a"), testID("principal"))
		require.NoError(t, err)
		assert.False(t, changed)
		count := countRows(t, repo, `SELECT COUNT(*) FROM iam_position_members WHERE tenant_id = 'testID("tenant").String()' AND position_id = 'testID("position-a").String()' AND principal_id = 'testID("principal").String()'`)
		assert.Zero(t, count)
	})

	t.Run("entity role binding", func(t *testing.T) {
		changed, err := RemoveEntityRoleBinding(t.Context(), repo.db, testID("tenant"), entity.ID, role.ID, testID("principal"))
		require.NoError(t, err)
		assert.True(t, changed)
		changed, err = RemoveEntityRoleBinding(t.Context(), repo.db, testID("tenant"), entity.ID, role.ID, testID("principal"))
		require.NoError(t, err)
		assert.False(t, changed)
		count := countRows(t, repo, `SELECT COUNT(*) FROM iam_role_bindings WHERE tenant_id = 'testID("tenant").String()' AND scope_type = 'entity' AND scope_id = '`+entity.ID.String()+`'`)
		assert.Zero(t, count)
	})

	t.Run("tenant isolation", func(t *testing.T) {
		changed, err := RemoveGroupMembership(t.Context(), repo.db, testID("other-tenant"), testID("group-a"), testID("principal"))
		require.NoError(t, err)
		assert.False(t, changed)
		changed, err = RemovePositionMembership(t.Context(), repo.db, testID("tenant"), testID("position-a"), testID("someone-else"))
		require.NoError(t, err)
		assert.False(t, changed)
	})
}

func TestReviewGrantRemovalsValidateInputs(t *testing.T) {
	_, err := RemoveGroupMembership(t.Context(), nil, testID("tenant"), testID("group-a"), testID("principal"))
	assert.Error(t, err)
	_, err = RemovePositionMembership(t.Context(), nil, testID("tenant"), testID("position-a"), testID("principal"))
	assert.Error(t, err)
	_, err = RemoveEntityRoleBinding(t.Context(), nil, testID("tenant"), testID("entity-a"), testID("role-a"), testID("principal"))
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
	require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, testID("tenant")))
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	_, err := service.PutTenantMember(t.Context(), testID("tenant"), testID("principal"), "Principal", "", 0, iamdomain.MemberActive)
	require.NoError(t, err)
	role := createTestRole(t, repo, testID("tenant"), "Review role")
	entity, err := service.CreateEntity(t.Context(), iamdomain.Entity{TenantID: testID("tenant"), Type: "store", Name: "Store"})
	require.NoError(t, err)
	now := repo.now().UnixMilli()
	_, err = repo.db.ExecContext(t.Context(), `INSERT INTO iam_role_bindings
		(tenant_id,role_id,principal_id,scope_type,scope_id,effect,expires_at,created_at)
		VALUES (?,?,?, 'entity', ?, 'allow', ?, ?)`, testID("tenant"), role.ID, testID("principal"), entity.ID, now+time.Hour.Milliseconds(), now)
	require.NoError(t, err)
	changed, err := RemoveEntityRoleBinding(t.Context(), repo.db, testID("tenant"), entity.ID, role.ID, testID("principal"))
	require.NoError(t, err)
	assert.True(t, changed, "expiring bindings are still revocable by review")
}
