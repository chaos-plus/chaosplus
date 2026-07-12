package guid

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
)

func newDB(t *testing.T) *bun.DB {
	t.Helper()
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestModule_FullChain(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)

	var lostCalled bool
	m := NewModule(db, func(error) { lostCalled = true }, 0, 0)

	require.NoError(t, m.Migrate(ctx))
	require.NoError(t, m.Start(ctx))

	// Start installed the process-wide generator, so the REST endpoint mints ids.
	_, api := humatest.New(t)
	m.RegisterREST(api)

	resp := api.Get("/guid")
	require.Equal(t, http.StatusOK, resp.Code)
	var body struct {
		Code int    `json:"code"`
		Data string `json:"data"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Equal(t, 0, body.Code)
	assert.Regexp(t, `^\d+$`, body.Data)

	// Stop releases the lease cleanly and is idempotent.
	require.NoError(t, m.Stop(ctx))
	require.NoError(t, m.Stop(ctx))
	assert.False(t, lostCalled, "lease was not lost during a clean run")
}

func TestModule_StartWithoutMigrate_Errors(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)

	// No tables exist, so leasing a worker id (which takes a dlock) fails.
	m := NewModule(db, nil, 0, 0)
	assert.Error(t, m.Start(ctx))
}

func TestModule_MigrateError_OnClosedDB(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	require.NoError(t, db.Close())

	m := NewModule(db, nil, 0, 0)
	assert.Error(t, m.Migrate(ctx))
}

func TestModule_StopWithoutStart_IsNoop(t *testing.T) {
	m := NewModule(newDB(t), nil, 0, 0)
	assert.NoError(t, m.Stop(context.Background()))
}

func TestModule_PinnedWorkerID_Starts(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)

	m := NewModule(db, nil, 12, 0)
	require.NoError(t, m.Migrate(ctx))
	require.NoError(t, m.Start(ctx))
	t.Cleanup(func() { _ = m.Stop(ctx) })

	_, err := Next() // generator installed with the pinned id
	require.NoError(t, err)
}

func TestModule_WorkerID_OutOfRange_Errors(t *testing.T) {
	m := NewModule(newDB(t), nil, 70000, 0)
	assert.ErrorContains(t, m.Start(context.Background()), "out of range")
}

func TestModule_WorkerLost_DisablesGeneratorAndEscalates(t *testing.T) {
	g, err := New(5)
	require.NoError(t, err)
	SetDefault(g)

	var lost error
	m := NewModule(nil, func(e error) { lost = e }, 0, 0)

	m.onWorkerLost(errors.New("lease gone"))

	assert.Nil(t, Default(), "generator cleared so GET /guid returns 503")
	_, nextErr := Next()
	assert.ErrorIs(t, nextErr, ErrNotInitialized)
	require.Error(t, lost)
	assert.ErrorContains(t, lost, "lease gone")
}
