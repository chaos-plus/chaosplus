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

	var lostCalled bool
	m := NewModule(db, func(error) { lostCalled = true })

	require.NoError(t, m.Migrate(ctx))
	require.NoError(t, m.Start(ctx))

	// Start installed the process-wide generator, so the REST endpoint mints ids.
	_, api := humatest.New(t)
	m.RegisterREST(api)

	resp := api.Get("/guid")
	require.Equal(t, http.StatusOK, resp.Code)
	var body struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Regexp(t, `^\d+$`, body.ID)

	// Stop releases the lease cleanly and is idempotent.
	require.NoError(t, m.Stop(ctx))
	require.NoError(t, m.Stop(ctx))
	assert.False(t, lostCalled, "lease was not lost during a clean run")
}

func TestModule_StartWithoutMigrate_Errors(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)

	// No tables exist, so leasing a worker id (which takes a dlock) fails.
	m := NewModule(db, nil)
	assert.Error(t, m.Start(ctx))
}

func TestModule_MigrateError_OnClosedDB(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	require.NoError(t, db.Close())

	m := NewModule(db, nil)
	assert.Error(t, m.Migrate(ctx))
}

func TestModule_StopWithoutStart_IsNoop(t *testing.T) {
	m := NewModule(newDB(t), nil)
	assert.NoError(t, m.Stop(context.Background()))
}
