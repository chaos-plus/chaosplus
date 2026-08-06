package policyx

import (
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestRevisionLifecycleUsesRealSQLite(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.ExecContext(t.Context(), `CREATE TABLE iam_policy_revisions (tenant_id TEXT PRIMARY KEY, revision BIGINT NOT NULL, updated_at BIGINT NOT NULL)`)
	require.NoError(t, err)

	revision, err := Current(t.Context(), db, "tenant")
	require.NoError(t, err)
	assert.Zero(t, revision)
	require.NoError(t, Lock(t.Context(), db, "sqlite", "tenant"))
	revision, err = Current(t.Context(), db, "tenant")
	require.NoError(t, err)
	assert.Zero(t, revision)
	require.NoError(t, Advance(t.Context(), db, "sqlite", "tenant", 1))
	require.NoError(t, Advance(t.Context(), db, "sqlite", "tenant", 2))
	revision, err = Current(t.Context(), db, "tenant")
	require.NoError(t, err)
	assert.Equal(t, int64(2), revision)
}

func TestRevisionDialectAndValidation(t *testing.T) {
	assert.Contains(t, upsertSQL("sqlite"), "ON CONFLICT")
	assert.Contains(t, upsertSQL("postgres"), "ON CONFLICT")
	assert.Contains(t, upsertSQL("mysql"), "ON DUPLICATE KEY")
	assert.Contains(t, ensureSQL("sqlite"), "DO NOTHING")
	assert.Contains(t, ensureSQL("postgres"), "DO NOTHING")
	assert.Contains(t, ensureSQL("mysql"), "ON DUPLICATE KEY")
	assert.Empty(t, ensureSQL("unknown"))
	assert.Empty(t, upsertSQL("unknown"))
	assert.Equal(t, "postgres", normalizeDialect("pg"))
	assert.Error(t, Advance(t.Context(), nil, "sqlite", "tenant", 1))
	assert.Error(t, Lock(t.Context(), nil, "sqlite", "tenant"))
	assert.Error(t, Lock(t.Context(), dbWithNoPolicyTable(t), "sqlite", "tenant"))
	assert.Error(t, Advance(t.Context(), dbWithNoPolicyTable(t), "sqlite", "tenant", 1))
	_, err := Current(t.Context(), nil, "tenant")
	_, err = Current(t.Context(), dbWithNoPolicyTable(t), "tenant")
	assert.Error(t, err)
}

func dbWithNoPolicyTable(t *testing.T) *bun.DB {
	t.Helper()
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}
