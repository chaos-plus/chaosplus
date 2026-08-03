package wuid

import (
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrationLifecyclePreservesWorkerLease(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	require.NoError(t, Migrate(t.Context(), db))
	_, err = db.ExecContext(t.Context(), `INSERT INTO worker_ids (id, token, expires_at, host) VALUES (7, 'lease-token', 1234, 'node-a')`)
	require.NoError(t, err)

	require.NoError(t, MigrateDown(t.Context(), db))
	var owner, host string
	var expiresAt int64
	require.NoError(t, db.NewSelect().Table("worker_ids").Column("owner", "host", "expires_at").Where("id = 7").Scan(t.Context(), &owner, &host, &expiresAt))
	assert.Equal(t, "lease-token", owner)
	assert.Equal(t, "node-a", host)
	assert.Equal(t, int64(1234), expiresAt)

	require.NoError(t, Migrate(t.Context(), db))
	var token string
	require.NoError(t, db.NewSelect().Table("worker_ids").Column("token").Where("id = 7").Scan(t.Context(), &token))
	assert.Equal(t, "lease-token", token)

	require.NoError(t, MigrateDownTo(t.Context(), db, 0))
	_, err = db.NewSelect().Table("worker_ids").ColumnExpr("1").Exec(t.Context())
	assert.Error(t, err)
	require.NoError(t, Migrate(t.Context(), db))
}
