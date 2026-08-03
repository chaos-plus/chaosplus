package dlock

import (
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/stretchr/testify/require"
)

func TestMigrationLifecycle(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	require.NoError(t, Migrate(t.Context(), db))
	require.NoError(t, MigrateDown(t.Context(), db))
	require.NoError(t, Migrate(t.Context(), db))
	require.NoError(t, MigrateDownTo(t.Context(), db, 0))
	require.NoError(t, Migrate(t.Context(), db))
}
