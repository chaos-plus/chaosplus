package iam_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
)

// TestMigrationDialectSmoke validates the IAM DDL against a real MySQL or
// PostgreSQL database selected by the caller. Use a disposable database.
func TestMigrationDialectSmoke(t *testing.T) {
	dialect := os.Getenv("IAM_DB_SMOKE_TYPE")
	if dialect == "" {
		t.Skip("set IAM_DB_SMOKE_TYPE and IAM_DB_SMOKE_DSN to test a real database dialect")
	}
	dsn := requiredEnv(t, "IAM_DB_SMOKE_DSN")
	db := (&bunx.Datasource{Type: dialect, Dsn: dsn}).NewDB()
	require.NotNil(t, db)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.PingContext(context.Background()))
	require.NoError(t, iam.Migrate(context.Background(), db))
}
