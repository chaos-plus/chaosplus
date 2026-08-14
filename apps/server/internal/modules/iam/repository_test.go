package iam

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	iamdomain "github.com/chaos-plus/chaosplus/internal/modules/iam/domain"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
)

func TestNewRepositoryNormalizesPostgresDialect(t *testing.T) {
	db, err := (&bunx.Datasource{Type: "postgres", Dsn: "postgres://unused"}).Open()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := NewRepository(db, newTestIDGenerator())
	assert.Equal(t, "postgres", repo.dialect)
}

func newIAMRepository(t *testing.T) *Repository {
	t.Helper()
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, Migrate(context.Background(), db))
	require.NoError(t, organization.Migrate(context.Background(), db))
	repo := NewRepository(db, newTestIDGenerator())
	repo.now = func() time.Time { return time.UnixMilli(1_700_000_000_000).UTC() }
	return repo
}

func putTestMember(t *testing.T, repo *Repository, member TenantMember) TenantMember {
	t.Helper()
	require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, member.TenantID))
	stored, err := repo.PutMember(t.Context(), member)
	require.NoError(t, err)
	return stored
}

func createTestRole(t *testing.T, repo *Repository, tenant guid.ID, name string) Role {
	t.Helper()
	role, err := repo.CreateRole(context.Background(), tenant, name, "description")
	require.NoError(t, err)
	return role
}

// TestRepositoryPropagatesDatabaseFailures drops real tables to produce real
// driver errors, so the error branches are exercised against the actual engine
// rather than a substituted failure. These paths are otherwise unreached.
func TestRepositoryPropagatesDatabaseFailures(t *testing.T) {
	dropAndAssert := func(t *testing.T, tables []string, call func(repo *Repository) error) {
		t.Helper()
		repo := newIAMRepository(t)
		require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, testID("tenant")))
		for _, table := range tables {
			_, err := repo.db.ExecContext(t.Context(), "DROP TABLE "+table)
			require.NoError(t, err)
		}
		assert.Error(t, call(repo))
	}

	t.Run("roles", func(t *testing.T) {
		dropAndAssert(t, []string{"iam_roles"}, func(repo *Repository) error {
			_, err := repo.ListRoles(t.Context(), testID("tenant"))
			return err
		})
		dropAndAssert(t, []string{"iam_roles"}, func(repo *Repository) error {
			_, _, err := repo.ListRolesPage(t.Context(), testID("tenant"), 0, 10)
			return err
		})
		dropAndAssert(t, []string{"iam_roles"}, func(repo *Repository) error {
			_, err := repo.CreateRole(t.Context(), testID("tenant"), "Broken", "")
			return err
		})
		dropAndAssert(t, []string{"iam_roles"}, func(repo *Repository) error {
			_, err := repo.GetRole(t.Context(), testID("tenant"), testID("missing"))
			return err
		})
	})

	t.Run("entities", func(t *testing.T) {
		dropAndAssert(t, []string{"iam_entities"}, func(repo *Repository) error {
			_, err := repo.ListEntities(t.Context(), testID("tenant"))
			return err
		})
		dropAndAssert(t, []string{"iam_entities"}, func(repo *Repository) error {
			_, _, err := repo.ListEntitiesPage(t.Context(), testID("tenant"), 0, 10)
			return err
		})
		dropAndAssert(t, []string{"iam_entities"}, func(repo *Repository) error {
			_, err := repo.CreateEntity(t.Context(), iamdomain.Entity{TenantID: testID("tenant"), Type: "store", Name: "Broken", Status: iamdomain.EntityActive})
			return err
		})
	})

	t.Run("platform authorization", func(t *testing.T) {
		dropAndAssert(t, []string{"iam_platform_administrators"}, func(repo *Repository) error {
			_, err := repo.ListPlatformAdministrators(t.Context())
			return err
		})
		dropAndAssert(t, []string{"iam_platform_grants"}, func(repo *Repository) error {
			_, err := repo.ListPlatformAdministrators(t.Context())
			return err
		})
		dropAndAssert(t, []string{"iam_platform_grants"}, func(repo *Repository) error {
			_, err := repo.SetPlatformAdministrator(t.Context(), testID("someone"), true, nil)
			return err
		})
		dropAndAssert(t, []string{"iam_platform_administrators"}, func(repo *Repository) error {
			_, err := repo.SetPlatformAdministrator(t.Context(), testID("someone"), false, []string{"tenant_view"})
			return err
		})
		dropAndAssert(t, []string{"iam_platform_administrators"}, func(repo *Repository) error {
			_, err := repo.DeletePlatformAdministrator(t.Context(), testID("someone"))
			return err
		})
		dropAndAssert(t, []string{"iam_platform_grants"}, func(repo *Repository) error {
			_, err := repo.DeletePlatformAdministrator(t.Context(), testID("someone"))
			return err
		})
		dropAndAssert(t, []string{"iam_platform_administrators"}, func(repo *Repository) error {
			_, err := repo.CountFullPlatformAdministrators(t.Context())
			return err
		})
		dropAndAssert(t, []string{"iam_platform_administrators"}, func(repo *Repository) error {
			return repo.assertPlatformAdministratorRemains(t.Context())
		})
	})

	t.Run("permissions and members", func(t *testing.T) {
		dropAndAssert(t, []string{"iam_role_permissions"}, func(repo *Repository) error {
			_, err := repo.ListPermissionGrants(t.Context(), testID("tenant"), testID("role"))
			return err
		})
		dropAndAssert(t, []string{"iam_role_members"}, func(repo *Repository) error {
			_, err := repo.ListMembers(t.Context(), testID("tenant"), testID("role"))
			return err
		})
		dropAndAssert(t, []string{"iam_tenant_members"}, func(repo *Repository) error {
			_, err := repo.IsMemberActive(t.Context(), testID("tenant"), testID("root"))
			return err
		})
		dropAndAssert(t, []string{"iam_role_members", "iam_temporary_role_grants"}, func(repo *Repository) error {
			_, err := repo.ListMemberRoleIDs(t.Context(), testID("tenant"), testID("root"))
			return err
		})
	})

	t.Run("authorizer", func(t *testing.T) {
		dropAndAssert(t, []string{"iam_entities"}, func(repo *Repository) error {
			_, err := NewAuthorizer(repo.db).Constraint(t.Context(), testID("tenant"), "store_view", testID("root"))
			return err
		})
		dropAndAssert(t, []string{"iam_principals"}, func(repo *Repository) error {
			_, err := NewAuthorizer(repo.db).CheckPlatform(t.Context(), "tenant_view", testID("root"))
			return err
		})
		dropAndAssert(t, []string{"iam_tenant_members"}, func(repo *Repository) error {
			_, err := NewAuthorizer(repo.db).Check(t.Context(), testID("tenant"), "store_view", testID("root"))
			return err
		})
		dropAndAssert(t, []string{"iam_groups"}, func(repo *Repository) error {
			_, err := matchingDynamicGroupIDs(t.Context(), repo.db, testID("tenant"), testID("root"))
			return err
		})
	})
}

func TestRepositoryRoleCRUDAndTenantIsolation(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := context.Background()

	role := createTestRole(t, repo, testID("tenant-a"), "Managers")
	assert.Equal(t, testID("tenant-a"), role.TenantID)
	assert.Equal(t, "Managers", role.Name)
	assert.Equal(t, time.UnixMilli(1_700_000_000_000).UTC(), role.CreatedAt)

	_, err := repo.CreateRole(ctx, testID("tenant-a"), "Managers", "duplicate")
	assert.ErrorIs(t, err, ErrRoleNameConflict)
	other := createTestRole(t, repo, testID("tenant-b"), "Managers")
	assert.NotEqual(t, role.ID, other.ID)

	roles, err := repo.ListRoles(ctx, testID("tenant-a"))
	require.NoError(t, err)
	require.Len(t, roles, 1)
	assert.Equal(t, role.ID, roles[0].ID)

	_, err = repo.GetRole(ctx, testID("tenant-b"), role.ID)
	assert.ErrorIs(t, err, ErrRoleNotFound)
	updated, err := repo.UpdateRole(ctx, testID("tenant-a"), role.ID, "Operators", "updated")
	require.NoError(t, err)
	assert.Equal(t, "Operators", updated.Name)
	assert.Equal(t, "updated", updated.Description)
	_, err = repo.UpdateRole(ctx, testID("tenant-a"), testID("missing"), "x", "")
	assert.ErrorIs(t, err, ErrRoleNotFound)
}

func TestRepositoryPermissionAndMemberBindings(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := context.Background()
	role := createTestRole(t, repo, testID("t1"), "role")

	changed, err := repo.GrantPermission(ctx, testID("t1"), role.ID, "store_view")
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = repo.GrantPermission(ctx, testID("t1"), role.ID, "store_view")
	require.NoError(t, err)
	assert.False(t, changed)
	codes, err := repo.ListPermissions(ctx, testID("t1"), role.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"store_view"}, codes)
	condition := json.RawMessage(`{"version":1,"gte":[{"context":"auth.acr"},{"value":2}]}`)
	grant, changed, err := repo.SetPermissionCondition(ctx, testID("t1"), role.ID, "store_view", condition)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.JSONEq(t, string(condition), string(grant.Condition))
	grants, err := repo.ListPermissionGrants(ctx, testID("t1"), role.ID)
	require.NoError(t, err)
	require.Len(t, grants, 1)
	assert.JSONEq(t, string(condition), string(grants[0].Condition))
	_, changed, err = repo.SetPermissionCondition(ctx, testID("t1"), role.ID, "store_view", condition)
	require.NoError(t, err)
	assert.False(t, changed)
	_, _, err = repo.SetPermissionCondition(ctx, testID("t1"), role.ID, "missing", condition)
	assert.ErrorIs(t, err, ErrRolePermissionNotGranted)
	grant, changed, err = repo.SetPermissionCondition(ctx, testID("t1"), role.ID, "store_view", nil)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Empty(t, grant.Condition)

	changed, err = repo.AddMember(ctx, testID("t1"), role.ID, testID("local-principal"))
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = repo.AddMember(ctx, testID("t1"), role.ID, testID("local-principal"))
	require.NoError(t, err)
	assert.False(t, changed)
	members, err := repo.ListMembers(ctx, testID("t1"), role.ID)
	require.NoError(t, err)
	assert.Equal(t, []guid.ID{testID("local-principal")}, members)

	_, err = repo.GrantPermission(ctx, testID("wrong"), role.ID, "store_view")
	assert.ErrorIs(t, err, ErrRoleNotFound)
	_, err = repo.AddMember(ctx, testID("wrong"), role.ID, testID("u"))
	assert.ErrorIs(t, err, ErrRoleNotFound)

	changed, err = repo.RevokePermission(ctx, testID("t1"), role.ID, "store_view")
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = repo.RemoveMember(ctx, testID("t1"), role.ID, testID("local-principal"))
	require.NoError(t, err)
	assert.True(t, changed)
	codes, err = repo.ListPermissions(ctx, testID("t1"), role.ID)
	require.NoError(t, err)
	assert.Empty(t, codes)
	members, err = repo.ListMembers(ctx, testID("t1"), role.ID)
	require.NoError(t, err)
	assert.Empty(t, members)
}

func TestRepositoryDeleteRoleRemovesBindings(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := context.Background()
	role := createTestRole(t, repo, testID("t1"), "role")
	_, err := repo.GrantPermission(ctx, testID("t1"), role.ID, "store_view")
	require.NoError(t, err)
	_, err = repo.AddMember(ctx, testID("t1"), role.ID, testID("u1"))
	require.NoError(t, err)

	require.NoError(t, repo.DeleteRole(ctx, testID("t1"), role.ID))
	_, err = repo.GetRole(ctx, testID("t1"), role.ID)
	assert.ErrorIs(t, err, ErrRoleNotFound)

	codes, err := repo.ListPermissions(ctx, testID("t1"), role.ID)
	assert.ErrorIs(t, err, ErrRoleNotFound)
	assert.Nil(t, codes)
	assert.ErrorIs(t, repo.DeleteRole(ctx, testID("t1"), role.ID), ErrRoleNotFound)
}

func TestRepositoryIDAndDialectErrors(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := NewRepository(db, func() (guid.ID, error) { return 0, errors.New("id failed") })
	_, err = repo.CreateRole(context.Background(), testID("t"), "r", "")
	assert.ErrorContains(t, err, "id failed")

	assert.Panics(t, func() { NewRepository(nil, newTestIDGenerator()) })
	assert.Panics(t, func() { NewRepository(db, nil) })
}

func TestRepositoryConflict(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := context.Background()
	first := createTestRole(t, repo, testID("t1"), "first")
	second := createTestRole(t, repo, testID("t1"), "second")
	_, err := repo.UpdateRole(ctx, testID("t1"), second.ID, first.Name, "")
	assert.ErrorIs(t, err, ErrRoleNameConflict)

}

func TestRepositoryReportsClosedDatabaseErrors(t *testing.T) {
	repo := newIAMRepository(t)
	require.NoError(t, repo.db.Close())
	ctx := context.Background()
	_, err := repo.ListRoles(ctx, testID("t1"))
	assert.Error(t, err)
	_, err = repo.UpdateRole(ctx, testID("t1"), testID("r1"), "name", "")
	assert.Error(t, err)
	_, err = repo.ListPermissions(ctx, testID("t1"), testID("r1"))
	assert.Error(t, err)
	_, err = repo.ListPermissionGrants(ctx, testID("t1"), testID("r1"))
	assert.Error(t, err)
	_, err = repo.ListMembers(ctx, testID("t1"), testID("r1"))
	assert.Error(t, err)
}
