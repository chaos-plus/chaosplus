package guid

import (
	"context"
	"encoding/json"
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

	m := NewModule(db, 0)
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
}

func TestModule_StartWithoutMigrate_Errors(t *testing.T) {
	// No tables exist, so leasing a worker id (which takes a dlock) fails.
	m := NewModule(newDB(t), 0)
	assert.Error(t, m.Start(context.Background()))
}

func TestModule_MigrateError_OnClosedDB(t *testing.T) {
	db := newDB(t)
	require.NoError(t, db.Close())

	m := NewModule(db, 0)
	assert.Error(t, m.Migrate(context.Background()))
}

func TestModule_StopWithoutStart_IsNoop(t *testing.T) {
	m := NewModule(newDB(t), 0)
	assert.NoError(t, m.Stop(context.Background()))
}

func TestModule_WorkerLostSuspends_ReacquireResumes(t *testing.T) {
	g, err := New(5)
	require.NoError(t, err)
	SetDefault(g)

	m := NewModule(nil, 0)

	// Lost -> generator cleared so GET /guid returns 503.
	m.onWorkerLost(nil)
	assert.Nil(t, Default(), "generator cleared on loss")
	_, nextErr := Next()
	assert.ErrorIs(t, nextErr, ErrNotInitialized)

	// Re-acquire -> generator reinstalled so generation resumes.
	m.onWorkerReacquire(9)
	require.NotNil(t, Default(), "generator reinstalled on re-acquire")
	_, nextErr = Next()
	assert.NoError(t, nextErr)
}
