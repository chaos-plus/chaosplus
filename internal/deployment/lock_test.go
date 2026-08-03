package deployment

import (
	"context"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLiteDeploymentLockSerializesAndReleases(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	first, err := acquireAdvisoryLock(context.Background(), db, "sqlite", time.Second)
	require.NoError(t, err)

	_, err = acquireAdvisoryLock(context.Background(), db, "sqlite", 20*time.Millisecond)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	require.NoError(t, first.Close(context.Background()))
	require.NoError(t, first.Close(context.Background()))
	second, err := acquireAdvisoryLock(context.Background(), db, "sqlite", time.Second)
	require.NoError(t, err)
	require.NoError(t, second.Close(context.Background()))
}

func TestAdvisoryLockValidationAndClosedDatabase(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)

	lock, err := acquireAdvisoryLock(context.Background(), db, "sqlite", 0)
	require.NoError(t, err)
	require.NoError(t, lock.Close(context.Background()))

	assert.NoError(t, (*advisoryLock)(nil).Close(context.Background()))
	assert.NoError(t, (&advisoryLock{dialect: "postgres"}).Close(context.Background()))

	_, err = acquireAdvisoryLock(context.Background(), db, "unsupported", time.Second)
	assert.ErrorContains(t, err, "unsupported bootstrap database dialect")

	require.NoError(t, db.Close())
	_, err = acquireAdvisoryLock(context.Background(), db, "postgres", time.Second)
	assert.ErrorContains(t, err, "open bootstrap lock connection")
}
