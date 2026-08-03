package governance

import (
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGovernanceMigrationLifecycle(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	assert.Error(t, AssertMigrated(t.Context(), db))
	require.NoError(t, iam.Migrate(t.Context(), db))
	require.NoError(t, organization.Migrate(t.Context(), db))
	require.NoError(t, Migrate(t.Context(), db))
	assert.NoError(t, AssertMigrated(t.Context(), db))

	require.NoError(t, MigrateDownTo(t.Context(), db, 0))
	assert.Error(t, AssertMigrated(t.Context(), db))
	require.NoError(t, Migrate(t.Context(), db))
	require.NoError(t, MigrateDown(t.Context(), db))
	assert.Error(t, AssertMigrated(t.Context(), db))
}

func TestGovernanceMigrationValidation(t *testing.T) {
	assert.Error(t, Migrate(t.Context(), nil))
	assert.Error(t, MigrateDown(t.Context(), nil))
	assert.Error(t, MigrateDownTo(t.Context(), nil, 0))
	assert.Error(t, AssertMigrated(t.Context(), nil))
}
