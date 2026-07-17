package iam

import (
	"context"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssertMigrated(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	assert.Error(t, AssertMigrated(context.Background(), db))
	require.NoError(t, Migrate(context.Background(), db))
	assert.NoError(t, AssertMigrated(context.Background(), db))
}
