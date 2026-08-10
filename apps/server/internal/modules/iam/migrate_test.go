package iam

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestMigrationDialectSmoke(t *testing.T) {
	dialect := os.Getenv("IAM_DB_SMOKE_TYPE")
	if dialect == "" {
		t.Skip("set IAM_DB_SMOKE_TYPE and IAM_DB_SMOKE_DSN to test a real database dialect")
	}
	dsn := os.Getenv("IAM_DB_SMOKE_DSN")
	require.NotEmpty(t, dsn, "IAM_DB_SMOKE_DSN is required")
	db := (&bunx.Datasource{Type: dialect, Dsn: dsn}).NewDB()
	require.NotNil(t, db)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.PingContext(context.Background()))
	require.NoError(t, Migrate(context.Background(), db))
}

func TestAssertMigrated(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	assert.Error(t, AssertMigrated(context.Background(), db))
	require.NoError(t, Migrate(context.Background(), db))
	assert.NoError(t, AssertMigrated(context.Background(), db))
	require.NoError(t, MigrateDownTo(t.Context(), db, 0))
	assert.Error(t, AssertMigrated(t.Context(), db))
	require.NoError(t, Migrate(t.Context(), db))
	assert.NoError(t, AssertMigrated(t.Context(), db))
	require.NoError(t, MigrateDown(t.Context(), db))
	// The last migration (00025) is an ALTER adding the email-verification code
	// column to a table AssertMigrated does not check, so one MigrateDown leaves
	// the checked schema ready; the down-to-zero case above covers "schema not
	// ready after rollback".
	assert.NoError(t, AssertMigrated(t.Context(), db))
	require.NoError(t, Migrate(t.Context(), db))
	assert.NoError(t, AssertMigrated(t.Context(), db))
}

func TestPolicyRevisionMigrationLifecycle(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, Migrate(t.Context(), db))

	count, err := db.NewSelect().Table("sqlite_master").Where("type = 'table' AND name = 'iam_policy_revisions'").Count(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	require.NoError(t, MigrateDownTo(t.Context(), db, 9))
	count, err = db.NewSelect().Table("sqlite_master").Where("type = 'table' AND name = 'iam_policy_revisions'").Count(t.Context())
	require.NoError(t, err)
	assert.Zero(t, count)
	require.NoError(t, Migrate(t.Context(), db))
}

func TestTemporaryRoleGrantMigrationLifecycle(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, Migrate(t.Context(), db))

	count, err := db.NewSelect().Table("sqlite_master").Where("type = 'table' AND name = 'iam_temporary_role_grants'").Count(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	require.NoError(t, MigrateDownTo(t.Context(), db, 19))
	count, err = db.NewSelect().Table("sqlite_master").Where("type = 'table' AND name = 'iam_temporary_role_grants'").Count(t.Context())
	require.NoError(t, err)
	assert.Zero(t, count)
	require.NoError(t, Migrate(t.Context(), db))
}

func TestAuditRetentionPolicyMigrationLifecycle(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, Migrate(t.Context(), db))

	count, err := db.NewSelect().Table("sqlite_master").Where("type = 'table' AND name = 'iam_audit_retention_policies'").Count(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_audit_retention_policies (tenant_id, min_days, archive_after_days, updated_at) VALUES ('tenant-a', 30, 90, 1)`)
	require.NoError(t, err)

	require.NoError(t, MigrateDownTo(t.Context(), db, 21))
	count, err = db.NewSelect().Table("sqlite_master").Where("type = 'table' AND name = 'iam_audit_retention_policies'").Count(t.Context())
	require.NoError(t, err)
	assert.Zero(t, count)
	require.NoError(t, Migrate(t.Context(), db))
	assert.NoError(t, AssertMigrated(t.Context(), db))
}

func TestRolePermissionConditionMigrationLifecycle(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, Migrate(t.Context(), db))
	columnCount := func() int {
		var count int
		require.NoError(t, db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM pragma_table_info('iam_role_permissions') WHERE name = 'condition_json'`).Scan(&count))
		return count
	}
	assert.Equal(t, 1, columnCount())
	require.NoError(t, MigrateDownTo(t.Context(), db, 20))
	assert.Zero(t, columnCount())
	assert.Error(t, AssertMigrated(t.Context(), db))
	require.NoError(t, Migrate(t.Context(), db))
	assert.Equal(t, 1, columnCount())
}

func TestEntityConstraintMigrationLifecycle(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, Migrate(t.Context(), db))

	insertRoot := func(id string) error {
		_, err := db.ExecContext(t.Context(), `INSERT INTO iam_entities
			(tenant_id, id, parent_id, type, name, status, metadata, created_at, updated_at)
			VALUES ('tenant', ?, NULL, 'company', 'Acme', 'active', '{}', 1, 1)`, id)
		return err
	}
	require.NoError(t, insertRoot("root-1"))
	assert.Error(t, insertRoot("root-2"))

	require.NoError(t, MigrateDownTo(t.Context(), db, 10))
	count, err := db.NewSelect().Table("sqlite_master").
		Where("type = 'index' AND name = 'uq_iam_entities_sibling_name'").Count(t.Context())
	require.NoError(t, err)
	assert.Zero(t, count)
	require.NoError(t, Migrate(t.Context(), db))
	_, err = db.NewDelete().Table("iam_entities").Where("tenant_id = ?", "tenant").Exec(t.Context())
	require.NoError(t, err)
	require.NoError(t, insertRoot("root-3"))
	assert.Error(t, insertRoot("root-4"))
}

func TestEmailVerificationMigrationLifecycle(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, Migrate(t.Context(), db))

	var definition string
	require.NoError(t, db.NewSelect().Table("sqlite_master").Column("sql").Where("type = 'table' AND name = 'iam_email_verification_tokens'").Scan(t.Context(), &definition))
	assert.Contains(t, definition, "token_hmac")
	assert.Contains(t, definition, "principal_id")
	assert.Contains(t, definition, "email")

	require.NoError(t, MigrateDownTo(t.Context(), db, 8))
	count, err := db.NewSelect().Table("sqlite_master").Where("type = 'table' AND name = 'iam_email_verification_tokens'").Count(t.Context())
	require.NoError(t, err)
	assert.Zero(t, count)
	require.NoError(t, Migrate(t.Context(), db))
	count, err = db.NewSelect().Table("sqlite_master").Where("type = 'table' AND name = 'iam_email_verification_tokens'").Count(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestServiceAccountMigrationLifecycle(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, Migrate(t.Context(), db))

	for _, table := range []string{"iam_service_accounts", "iam_service_account_credentials"} {
		count, err := db.NewSelect().Table("sqlite_master").Where("type = 'table' AND name = ?", table).Count(t.Context())
		require.NoError(t, err)
		assert.Equal(t, 1, count, table)
	}
	require.NoError(t, MigrateDownTo(t.Context(), db, 12))
	for _, table := range []string{"iam_service_accounts", "iam_service_account_credentials"} {
		count, err := db.NewSelect().Table("sqlite_master").Where("type = 'table' AND name = ?", table).Count(t.Context())
		require.NoError(t, err)
		assert.Zero(t, count, table)
	}
	require.NoError(t, Migrate(t.Context(), db))
}

func TestSelfRegistrationMigrationLifecycle(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, Migrate(t.Context(), db))

	columnCount := func() int {
		var count int
		require.NoError(t, db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM pragma_table_info('iam_principals') WHERE name = 'activation_required'`).Scan(&count))
		return count
	}
	assert.Equal(t, 1, columnCount())
	require.NoError(t, MigrateDownTo(t.Context(), db, 13))
	assert.Zero(t, columnCount())
	require.NoError(t, Migrate(t.Context(), db))
	assert.Equal(t, 1, columnCount())
}

func TestRelationshipMigrationLifecycle(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, Migrate(t.Context(), db))
	columnCount := func(table, column string) int {
		var count int
		require.NoError(t, db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&count))
		return count
	}
	for _, table := range []string{"iam_relationships", "iam_resource_relationships"} {
		assert.Equal(t, 1, columnCount(table, "starts_at"))
		assert.Equal(t, 1, columnCount(table, "ends_at"))
		assert.Equal(t, 1, columnCount(table, "condition_json"))
	}
	for _, table := range []string{"iam_sessions", "iam_oauth_codes", "iam_refresh_tokens"} {
		for _, column := range []string{"auth_time", "acr", "amr"} {
			assert.Equal(t, 1, columnCount(table, column), table+"."+column)
		}
	}
	require.NoError(t, MigrateDownTo(t.Context(), db, 18))
	for _, table := range []string{"iam_sessions", "iam_oauth_codes", "iam_refresh_tokens"} {
		for _, column := range []string{"auth_time", "acr", "amr"} {
			assert.Zero(t, columnCount(table, column), table+"."+column)
		}
	}
	require.NoError(t, Migrate(t.Context(), db))
	require.NoError(t, MigrateDownTo(t.Context(), db, 17))
	for _, table := range []string{"iam_relationships", "iam_resource_relationships"} {
		assert.Zero(t, columnCount(table, "condition_json"))
	}
	require.NoError(t, Migrate(t.Context(), db))
	require.NoError(t, MigrateDownTo(t.Context(), db, 16))
	for _, table := range []string{"iam_relationships", "iam_resource_relationships"} {
		assert.Zero(t, columnCount(table, "starts_at"))
		assert.Zero(t, columnCount(table, "ends_at"))
	}
	require.NoError(t, Migrate(t.Context(), db))

	count, err := db.NewSelect().Table("sqlite_master").Where("type = 'table' AND name = 'iam_resource_relationships'").Count(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	require.NoError(t, MigrateDownTo(t.Context(), db, 15))
	count, err = db.NewSelect().Table("sqlite_master").Where("type = 'table' AND name = 'iam_resource_relationships'").Count(t.Context())
	require.NoError(t, err)
	assert.Zero(t, count)
	require.NoError(t, Migrate(t.Context(), db))

	count, err = db.NewSelect().Table("sqlite_master").Where("type = 'table' AND name = 'iam_relationships'").Count(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	require.NoError(t, MigrateDownTo(t.Context(), db, 14))
	count, err = db.NewSelect().Table("sqlite_master").Where("type = 'table' AND name = 'iam_relationships'").Count(t.Context())
	require.NoError(t, err)
	assert.Zero(t, count)
	require.NoError(t, Migrate(t.Context(), db))
}

func TestMigrationDialectLifecycle(t *testing.T) {
	db := newLifecycleDatabase(t)
	require.NoError(t, Migrate(t.Context(), db))
	require.NoError(t, AssertMigrated(t.Context(), db))
	require.NoError(t, MigrateDownTo(t.Context(), db, 0))
	assert.Error(t, AssertMigrated(t.Context(), db))
	require.NoError(t, Migrate(t.Context(), db))
	require.NoError(t, AssertMigrated(t.Context(), db))
}

func TestAuditAppendOnlyDialectLifecycle(t *testing.T) {
	db := newLifecycleDatabase(t)
	require.NoError(t, Migrate(t.Context(), db))
	_, err := db.ExecContext(t.Context(), `INSERT INTO iam_audit_events
		(id, tenant_id, event_type, outcome, detail, created_at, sequence, previous_hash, event_hash)
		VALUES ('evt-1', 'tenant', 'created', 'success', '{}', 1, 1, '', 'hash-1')`)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `UPDATE iam_audit_events SET outcome = 'denied' WHERE id = 'evt-1'`)
	require.ErrorContains(t, err, "append-only")
	_, err = db.ExecContext(t.Context(), `DELETE FROM iam_audit_events WHERE id = 'evt-1'`)
	require.ErrorContains(t, err, "append-only")
}

func TestIAMConstraintBehaviorDialectLifecycle(t *testing.T) {
	db := newLifecycleDatabase(t)
	require.NoError(t, Migrate(t.Context(), db))

	insertEntity := func(tenant, id string, parent any, kind, name string) error {
		_, err := db.ExecContext(t.Context(), `INSERT INTO iam_entities
			(tenant_id, id, parent_id, type, name, status, metadata, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, 'active', '{}', 1, 1)`, tenant, id, parent, kind, name)
		return err
	}
	require.NoError(t, insertEntity("tenant-a", "root-1", nil, "company", "Acme"))
	require.NoError(t, insertEntity("tenant-b", "root-2", nil, "company", "Acme"), "same sibling name in another tenant must coexist (tenant isolation)")
	assert.Error(t, insertEntity("tenant-a", "root-3", nil, "company", "Acme"), "duplicate sibling name within a tenant must be rejected")
	require.NoError(t, insertEntity("tenant-a", "child-1", "root-1", "department", "Engineering"))
	_, fkErr := db.ExecContext(t.Context(), `DELETE FROM iam_entities WHERE tenant_id = 'tenant-a' AND id = 'root-1'`)
	assert.Error(t, fkErr, "parent FK must RESTRICT deletion while children exist")
	_, err := db.ExecContext(t.Context(), `DELETE FROM iam_entities WHERE tenant_id = 'tenant-a' AND id = 'child-1'`)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `DELETE FROM iam_entities WHERE tenant_id = 'tenant-a' AND id = 'root-1'`)
	require.NoError(t, err)

	insertRole := func(tenant, id string) error {
		_, err := db.ExecContext(t.Context(), `INSERT INTO iam_roles (tenant_id, id, name, description, created_at, updated_at)
			VALUES (?, ?, ?, 'desc', 1, 1)`, tenant, id, id)
		return err
	}
	require.NoError(t, insertRole("tenant-a", "admin-role"))
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_role_permissions (tenant_id, role_id, permission_code, created_at)
		VALUES ('tenant-a', 'admin-role', 'tenant_administer', 1)`)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_role_members (tenant_id, role_id, user_subject, created_at)
		VALUES ('tenant-a', 'admin-role', 'subject-1', 1)`)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `DELETE FROM iam_roles WHERE tenant_id = 'tenant-a' AND id = 'admin-role'`)
	require.NoError(t, err)
	var remaining int
	require.NoError(t, db.NewSelect().Table("iam_role_permissions").ColumnExpr("COUNT(*)").Where("tenant_id = 'tenant-a' AND role_id = 'admin-role'").Scan(t.Context(), &remaining))
	assert.Zero(t, remaining)
	require.NoError(t, db.NewSelect().Table("iam_role_members").ColumnExpr("COUNT(*)").Where("tenant_id = 'tenant-a' AND role_id = 'admin-role'").Scan(t.Context(), &remaining))
	assert.Zero(t, remaining)

	insertAudit := func(id, tenant string, seq int64, hash string) error {
		_, err := db.ExecContext(t.Context(), `INSERT INTO iam_audit_events
			(id, tenant_id, event_type, outcome, detail, created_at, sequence, previous_hash, event_hash)
			VALUES (?, ?, 'created', 'success', '{}', 1, ?, '', ?)`, id, tenant, seq, hash)
		return err
	}
	require.NoError(t, insertAudit("evt-1", "tenant-a", 1, "hash-1"))
	require.NoError(t, insertAudit("evt-2", "tenant-b", 1, "hash-2"), "same sequence in another tenant must coexist")
	assert.Error(t, insertAudit("evt-3", "tenant-a", 1, "hash-3"), "duplicate sequence within a tenant must be rejected")
}

func newLifecycleDatabase(t *testing.T) *bun.DB {
	t.Helper()
	dialect := strings.ToLower(strings.TrimSpace(os.Getenv("IAM_DB_LIFECYCLE_TYPE")))
	if dialect == "" {
		t.Skip("set IAM_DB_LIFECYCLE_TYPE and IAM_DB_LIFECYCLE_ADMIN_DSN to test a disposable real database")
	}
	if dialect != "mysql" && dialect != "postgres" {
		t.Fatalf("unsupported lifecycle dialect %q", dialect)
	}
	adminDSN := os.Getenv("IAM_DB_LIFECYCLE_ADMIN_DSN")
	require.NotEmpty(t, adminDSN, "IAM_DB_LIFECYCLE_ADMIN_DSN is required")
	admin := (&bunx.Datasource{Type: dialect, Dsn: adminDSN}).NewDB()
	require.NotNil(t, admin)
	t.Cleanup(func() { _ = admin.Close() })
	require.NoError(t, admin.PingContext(t.Context()))

	name := fmt.Sprintf("chaosplus_iam_%d", time.Now().UTC().UnixNano())
	create, drop := lifecycleDatabaseStatements(dialect, name)
	_, err := admin.ExecContext(t.Context(), create)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanupErr := admin.ExecContext(context.Background(), drop)
		assert.NoError(t, cleanupErr)
	})

	targetDSN := lifecycleTargetDSN(t, dialect, adminDSN, name)
	db := (&bunx.Datasource{Type: dialect, Dsn: targetDSN}).NewDB()
	require.NotNil(t, db)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.PingContext(t.Context()))
	return db
}

func lifecycleDatabaseStatements(dialect, name string) (string, string) {
	if dialect == "mysql" {
		return "CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci", "DROP DATABASE IF EXISTS `" + name + "`"
	}
	return `CREATE DATABASE "` + name + `"`, `DROP DATABASE IF EXISTS "` + name + `"`
}

func lifecycleTargetDSN(t *testing.T, dialect, adminDSN, name string) string {
	t.Helper()
	if dialect == "mysql" {
		cfg, err := mysql.ParseDSN(adminDSN)
		require.NoError(t, err)
		cfg.DBName = name
		return cfg.FormatDSN()
	}
	parsed, err := url.Parse(adminDSN)
	require.NoError(t, err)
	parsed.Path = "/" + name
	return parsed.String()
}
