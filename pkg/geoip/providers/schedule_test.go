package providers

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMaintenanceWorkerStartOnceAndStop(t *testing.T) {
	var worker maintenanceWorker
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.start(ctx, func(workerCtx context.Context) { <-workerCtx.Done() })
	worker.start(ctx, func(context.Context) {})
	require.NoError(t, worker.stop(context.Background()))
	require.NoError(t, worker.stop(context.Background()))
}

func TestMaintainDBDownloadsMissingDatabaseThenExits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "db")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	maintainDB(ctx, "test", func() (string, error) { return "", nil }, func() error {
		return os.WriteFile(path, []byte("db"), 0o600)
	})
	assert.FileExists(t, path)
}
