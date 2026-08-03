package provisioning

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
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestProvisioningMigrationLifecycle(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, iam.Migrate(t.Context(), db))
	require.NoError(t, organization.Migrate(t.Context(), db))

	require.NoError(t, Migrate(t.Context(), db))
	require.NoError(t, AssertMigrated(t.Context(), db))
	require.NoError(t, MigrateDown(t.Context(), db))
	assert.Error(t, AssertMigrated(t.Context(), db))
	require.NoError(t, Migrate(t.Context(), db))
	require.NoError(t, AssertMigrated(t.Context(), db))
	require.NoError(t, MigrateDownTo(t.Context(), db, 0))
	assert.Error(t, AssertMigrated(t.Context(), db))

	assert.Error(t, Migrate(t.Context(), nil))
	assert.Error(t, MigrateDown(t.Context(), nil))
	assert.Error(t, MigrateDownTo(t.Context(), nil, 0))
	assert.Error(t, AssertMigrated(t.Context(), nil))
}

func TestProvisioningMigrationDialectLifecycle(t *testing.T) {
	dialect := os.Getenv("IAM_DB_LIFECYCLE_TYPE")
	if dialect == "" {
		t.Skip("set IAM_DB_LIFECYCLE_TYPE and IAM_DB_LIFECYCLE_ADMIN_DSN to test a disposable real database")
	}
	db := newProvisioningLifecycleDatabase(t, dialect, os.Getenv("IAM_DB_LIFECYCLE_ADMIN_DSN"))
	require.NoError(t, iam.Migrate(t.Context(), db))
	require.NoError(t, organization.Migrate(t.Context(), db))
	require.NoError(t, Migrate(t.Context(), db))
	require.NoError(t, AssertMigrated(t.Context(), db))
	require.NoError(t, MigrateDown(t.Context(), db))
	assert.Error(t, AssertMigrated(t.Context(), db))
	require.NoError(t, Migrate(t.Context(), db))
	require.NoError(t, AssertMigrated(t.Context(), db))
	require.NoError(t, MigrateDownTo(t.Context(), db, 0))
	assert.Error(t, AssertMigrated(t.Context(), db))
}

func newProvisioningLifecycleDatabase(t *testing.T, dialect, adminDSN string) *bun.DB {
	t.Helper()
	dialect = strings.ToLower(strings.TrimSpace(dialect))
	require.Contains(t, []string{"mysql", "postgres"}, dialect)
	require.NotEmpty(t, adminDSN)
	admin := (&bunx.Datasource{Type: dialect, Dsn: adminDSN}).NewDB()
	require.NotNil(t, admin)
	t.Cleanup(func() { _ = admin.Close() })
	require.NoError(t, admin.PingContext(t.Context()))

	name := fmt.Sprintf("chaosplus_provisioning_%d", time.Now().UTC().UnixNano())
	quoted := `"` + name + `"`
	create := "CREATE DATABASE " + quoted
	if dialect == "mysql" {
		quoted = "`" + name + "`"
		create = "CREATE DATABASE " + quoted + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"
	}
	_, err := admin.ExecContext(t.Context(), create)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanupErr := admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+quoted)
		assert.NoError(t, cleanupErr)
	})

	targetDSN := adminDSN
	if dialect == "mysql" {
		cfg, parseErr := mysql.ParseDSN(adminDSN)
		require.NoError(t, parseErr)
		cfg.DBName = name
		targetDSN = cfg.FormatDSN()
	} else {
		parsed, parseErr := url.Parse(adminDSN)
		require.NoError(t, parseErr)
		parsed.Path = "/" + name
		targetDSN = parsed.String()
	}
	db := (&bunx.Datasource{Type: dialect, Dsn: targetDSN}).NewDB()
	require.NotNil(t, db)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.PingContext(t.Context()))
	return db
}
