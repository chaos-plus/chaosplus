package organization

import (
	"context"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrganizationMigrationLifecycle(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	assert.Error(t, AssertMigrated(t.Context(), db))
	require.NoError(t, Migrate(t.Context(), db))
	assert.NoError(t, AssertMigrated(t.Context(), db))
	require.NoError(t, MigrateDownTo(t.Context(), db, 1))
	assert.Error(t, AssertMigrated(t.Context(), db))
	_, err = db.NewSelect().Table("iam_departments").ColumnExpr("1").Limit(1).Exec(t.Context())
	assert.NoError(t, err)
	require.NoError(t, Migrate(t.Context(), db))
	require.NoError(t, MigrateDown(t.Context(), db))
	_, err = db.ExecContext(t.Context(), "SELECT rule_json FROM iam_groups LIMIT 1")
	assert.Error(t, err)
	require.NoError(t, Migrate(t.Context(), db))
	assert.NoError(t, AssertMigrated(t.Context(), db))
	assert.Error(t, Migrate(t.Context(), nil))
	assert.Error(t, MigrateDown(t.Context(), nil))
	assert.Error(t, MigrateDownTo(t.Context(), nil, 0))
	assert.Error(t, AssertMigrated(t.Context(), nil))
}

func TestOrganizationMigrationsExistForEverySupportedDialect(t *testing.T) {
	for _, path := range []string{
		"sql/sqlite/00001_departments.sql",
		"sql/mysql/00001_departments.sql",
		"sql/postgres/00001_departments.sql",
	} {
		content, err := migrationsFS.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(content), "iam_departments")
		assert.Contains(t, string(content), "iam_department_closure")
		assert.Contains(t, string(content), "tenant_id")
		assert.Contains(t, string(content), "name_key")
	}
	for _, path := range []string{
		"sql/sqlite/00002_positions.sql",
		"sql/mysql/00002_positions.sql",
		"sql/postgres/00002_positions.sql",
	} {
		content, err := migrationsFS.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(content), "iam_positions")
		assert.Contains(t, string(content), "iam_position_members")
		assert.Contains(t, string(content), "principal_id")
	}
	for _, path := range []string{
		"sql/sqlite/00003_groups.sql",
		"sql/mysql/00003_groups.sql",
		"sql/postgres/00003_groups.sql",
	} {
		content, err := migrationsFS.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(content), "iam_groups")
		assert.Contains(t, string(content), "iam_group_members")
		assert.Contains(t, string(content), "name_key")
		assert.Contains(t, string(content), "principal_id")
	}
	for _, path := range []string{
		"sql/sqlite/00004_directory_role_bindings.sql",
		"sql/mysql/00004_directory_role_bindings.sql",
		"sql/postgres/00004_directory_role_bindings.sql",
	} {
		content, err := migrationsFS.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(content), "iam_group_role_bindings")
		assert.Contains(t, string(content), "iam_position_role_bindings")
		assert.Contains(t, string(content), "tenant_id")
		assert.Contains(t, string(content), "role_id")
	}
	for _, path := range []string{
		"sql/sqlite/00005_tenants.sql",
		"sql/mysql/00005_tenants.sql",
		"sql/postgres/00005_tenants.sql",
	} {
		content, err := migrationsFS.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(content), "iam_tenants")
		assert.Contains(t, string(content), "slug")
		assert.Contains(t, string(content), "status")
		assert.Contains(t, string(content), "version")
	}
	for _, path := range []string{
		"sql/sqlite/00006_iam_data_scopes.sql",
		"sql/mysql/00006_iam_data_scopes.sql",
		"sql/postgres/00006_iam_data_scopes.sql",
	} {
		content, err := migrationsFS.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(content), "iam_member_departments")
		assert.Contains(t, string(content), "iam_role_data_scopes")
		assert.Contains(t, string(content), "iam_role_scope_departments")
		assert.Contains(t, string(content), "department_id")
	}
	for _, path := range []string{
		"sql/sqlite/00007_invitations.sql",
		"sql/mysql/00007_invitations.sql",
		"sql/postgres/00007_invitations.sql",
	} {
		content, err := migrationsFS.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(content), "iam_invitations")
		assert.Contains(t, string(content), "iam_invitation_roles")
		assert.Contains(t, string(content), "token_hmac")
		assert.NotContains(t, string(content), "token_plaintext")
	}
	for _, path := range []string{
		"sql/sqlite/00008_dynamic_groups.sql",
		"sql/mysql/00008_dynamic_groups.sql",
		"sql/postgres/00008_dynamic_groups.sql",
	} {
		content, err := migrationsFS.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(content), "rule_json")
		assert.Contains(t, string(content), "dynamic")
		assert.Contains(t, string(content), "4096")
	}
}

func TestOrganizationMigrationBackfillsLegacyTenants(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(context.Background(), db))
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_tenant_members
		(tenant_id,user_subject,display_name,email,status,created_at,updated_at,disabled_at)
		VALUES ('t1','principal','Principal','','active',1,1,0)`)
	require.NoError(t, err)

	require.NoError(t, Migrate(t.Context(), db))
	require.NoError(t, Migrate(t.Context(), db))
	tenant, err := getTenantRow(t.Context(), db, "t1")
	require.NoError(t, err)
	assert.Equal(t, "tenant-t1", tenant.Slug)
	assert.Equal(t, TenantActive, tenant.Status)
}

func TestDynamicGroupMigrationRejectsDestructiveRollback(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	require.NoError(t, Migrate(t.Context(), db))
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_groups
		(tenant_id,id,name,name_key,group_type,rule_json,description,status,sort_order,version,created_at,updated_at)
		VALUES ('tenant','dynamic','Dynamic','dynamic','dynamic','{"version":1,"match":"all","conditions":[{"field":"member.status","operator":"in","values":["active"]}]}','','active',0,1,1,1)`)
	require.NoError(t, err)

	err = MigrateDown(t.Context(), db)
	assert.Error(t, err)
	var rule string
	require.NoError(t, db.NewSelect().Table("iam_groups").Column("rule_json").Where("tenant_id = 'tenant' AND id = 'dynamic'").Scan(t.Context(), &rule))
	assert.Contains(t, rule, `"version":1`)
	_, err = db.ExecContext(t.Context(), "DELETE FROM iam_groups WHERE tenant_id = 'tenant' AND id = 'dynamic'")
	require.NoError(t, err)
	require.NoError(t, MigrateDown(t.Context(), db))
	_, err = db.ExecContext(t.Context(), "SELECT rule_json FROM iam_groups LIMIT 1")
	assert.Error(t, err)
}
