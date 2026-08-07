package wuid

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/infra/dlock"
)

func newDB(t *testing.T) *bun.DB {
	t.Helper()
	database, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	require.NoError(t, dlock.Migrate(context.Background(), database))
	require.NoError(t, Migrate(context.Background(), database))
	return database
}

func rowExpiresAt(t *testing.T, database *bun.DB, id int) int64 {
	t.Helper()
	var exp int64
	err := database.NewSelect().Model((*workerRow)(nil)).
		Column("expires_at").Where("id = ?", id).Scan(context.Background(), &exp)
	require.NoError(t, err)
	return exp
}

func TestAllocate_SequentialIDs(t *testing.T) {
	ctx := context.Background()
	database := newDB(t)

	var got []uint16
	for i := 0; i < 3; i++ {
		w, err := allocate(ctx, database)
		require.NoError(t, err)
		got = append(got, w.ID())
		t.Cleanup(func() { _ = w.Close(ctx) })
	}
	assert.Equal(t, []uint16{0, 1, 2}, got)
}

func TestAllocate_ReuseExpiredSlot(t *testing.T) {
	ctx := context.Background()
	database := newDB(t)

	w0, err := allocate(ctx, database)
	require.NoError(t, err)
	require.Equal(t, uint16(0), w0.ID())

	w1, err := allocate(ctx, database)
	require.NoError(t, err)
	require.Equal(t, uint16(1), w1.ID())
	t.Cleanup(func() { _ = w1.Close(ctx) })

	// Releasing id 0 frees the slot for reuse.
	require.NoError(t, w0.Close(ctx))

	w2, err := allocate(ctx, database)
	require.NoError(t, err)
	assert.Equal(t, uint16(0), w2.ID(), "lowest expired slot should be reused")
	t.Cleanup(func() { _ = w2.Close(ctx) })
}

func TestHeartbeat_RenewsLease(t *testing.T) {
	ctx := context.Background()
	database := newDB(t)

	w, err := allocate(ctx, database, WithLease(300*time.Millisecond))
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close(ctx) })

	// Force the lease near expiry; the heartbeat must renew it back to ~now+lease
	// (resolution-independent: the sqlite clock advances too coarsely to observe a
	// sub-second bump directly).
	_, err = database.NewUpdate().Model((*workerRow)(nil)).
		Set("expires_at = ?", int64(1)).
		Where("id = ?", w.ID()).
		Exec(ctx)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return rowExpiresAt(t, database, int(w.ID())) > 1000
	}, 2*time.Second, 20*time.Millisecond, "heartbeat should renew the lease")
}

func TestHeartbeat_LostTriggersOnLost(t *testing.T) {
	ctx := context.Background()
	database := newDB(t)

	lost := make(chan error, 1)
	w, err := allocate(ctx, database,
		WithLease(200*time.Millisecond),
		OnLost(func(e error) { lost <- e }),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close(ctx) })
	assert.True(t, w.Alive())

	// Simulate another node stealing the worker id (its token changes).
	_, err = database.NewUpdate().Model((*workerRow)(nil)).
		Set("token = ?", "thief").
		Where("id = ?", w.ID()).
		Exec(ctx)
	require.NoError(t, err)

	select {
	case e := <-lost:
		assert.ErrorContains(t, e, "taken over")
	case <-time.After(2 * time.Second):
		t.Fatal("OnLost was not called after the id was taken over")
	}
	assert.False(t, w.Alive(), "worker should not be Alive after losing its id")
}

func TestHeartbeat_TransientErrorEventuallyLost(t *testing.T) {
	ctx := context.Background()
	database := newDB(t)

	lost := make(chan error, 1)
	w, err := allocate(ctx, database,
		WithLease(150*time.Millisecond),
		OnLost(func(e error) { lost <- e }),
	)
	require.NoError(t, err)

	// Kill the database: renews now fail transiently. The worker should keep
	// retrying and only declare the id lost once the safety margin runs out.
	require.NoError(t, database.Close())

	select {
	case e := <-lost:
		assert.ErrorContains(t, e, "could not renew")
	case <-time.After(2 * time.Second):
		t.Fatal("OnLost was not called after repeated renew failures")
	}
	assert.False(t, w.Alive())
}

func TestHeartbeat_ReacquiresAfterLoss(t *testing.T) {
	ctx := context.Background()
	database := newDB(t)

	reacquired := make(chan uint16, 1)
	w, err := allocate(ctx, database,
		WithLease(150*time.Millisecond),
		OnReacquire(func(id uint16) { reacquired <- id }),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close(ctx) })

	// Steal the id (change its token); the worker declares it lost, then
	// re-allocates a fresh id in the background and resumes.
	_, err = database.NewUpdate().Model((*workerRow)(nil)).
		Set("token = ?", "thief").Where("id = ?", w.ID()).Exec(ctx)
	require.NoError(t, err)

	select {
	case <-reacquired:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not re-acquire an id after loss")
	}
	assert.True(t, w.Alive(), "worker is Alive again after re-acquiring")
}

func TestStaticWorker(t *testing.T) {
	w := newStatic(42)
	assert.Equal(t, uint16(42), w.ID())
	assert.True(t, w.Alive())
	// Static workers have no lease; Close is a no-op and needs no database.
	require.NoError(t, w.Close(context.Background()))
	assert.False(t, w.Alive())
}

func TestOpen_FromEnv(t *testing.T) {
	t.Setenv(EnvKey, "123")
	w, err := Open(context.Background(), nil) // no DB needed for the env path
	require.NoError(t, err)
	assert.Equal(t, uint16(123), w.ID())
	assert.True(t, w.Alive())
}

func TestOpen_EnvInvalid(t *testing.T) {
	t.Setenv(EnvKey, "not-a-number")
	_, err := Open(context.Background(), nil)
	assert.Error(t, err)
}

func TestOpen_FallsBackToLease(t *testing.T) {
	t.Setenv(EnvKey, "") // force the env source to be skipped
	ctx := context.Background()
	database := newDB(t)

	w, err := Open(ctx, database)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close(ctx) })
	assert.Equal(t, uint16(0), w.ID())
	assert.False(t, w.static)
}

func TestFromEnv(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		t.Setenv(EnvKey, "")
		_, ok, err := fromEnv(EnvKey)
		require.NoError(t, err)
		assert.False(t, ok)
	})
	t.Run("valid", func(t *testing.T) {
		t.Setenv(EnvKey, "7")
		id, ok, err := fromEnv(EnvKey)
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, uint16(7), id)
	})
	t.Run("not a number", func(t *testing.T) {
		t.Setenv(EnvKey, "abc")
		_, _, err := fromEnv(EnvKey)
		assert.Error(t, err)
	})
	t.Run("out of range", func(t *testing.T) {
		t.Setenv(EnvKey, "70000")
		_, _, err := fromEnv(EnvKey)
		assert.Error(t, err)
	})
}

func TestWorkerIDRejectsCorruptStoredValue(t *testing.T) {
	w := &Worker{}
	w.id.Store(-1)
	assert.Panics(t, func() { w.ID() })
	w.id.Store(MaxWorkerID + 1)
	assert.Panics(t, func() { w.ID() })
}

func TestClose_Idempotent(t *testing.T) {
	ctx := context.Background()
	database := newDB(t)

	w, err := allocate(ctx, database)
	require.NoError(t, err)

	require.NoError(t, w.Close(ctx))
	require.NoError(t, w.Close(ctx)) // second call is a no-op
}

func TestWithLease_ZeroIgnored(t *testing.T) {
	c := config{lease: DefaultLease}
	WithLease(0)(&c)
	assert.Equal(t, DefaultLease, c.lease)

	WithLease(5 * time.Second)(&c)
	assert.Equal(t, 5*time.Second, c.lease)
}

func TestAllocate_PoolExhausted(t *testing.T) {
	ctx := context.Background()
	database := newDB(t)

	// Occupy the top slot with a live (non-expired) lease so it cannot be
	// reused and MAX(id)+1 overflows the pool.
	future := time.Now().Add(time.Hour).UnixMilli()
	_, err := database.NewInsert().Model(&workerRow{
		ID: MaxWorkerID, Token: "x", ExpiresAt: future,
	}).Exec(ctx)
	require.NoError(t, err)

	_, err = allocate(ctx, database)
	assert.ErrorContains(t, err, "pool exhausted")
}

func TestAllocate_LockAcquireError(t *testing.T) {
	ctx := context.Background()
	database := newDB(t)
	require.NoError(t, database.Close())

	_, err := allocate(ctx, database)
	assert.ErrorContains(t, err, "acquire alloc lock")
}

func TestAllocate_ClaimError(t *testing.T) {
	ctx := context.Background()
	database, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	// Only the dlock table exists, so the lock is acquired but claimID fails
	// because worker_ids is missing.
	require.NoError(t, dlock.Migrate(ctx, database))

	_, err = allocate(ctx, database)
	assert.Error(t, err)
}

func TestClose_AfterDBClosed(t *testing.T) {
	ctx := context.Background()
	database := newDB(t)

	w, err := allocate(ctx, database)
	require.NoError(t, err)

	require.NoError(t, database.Close())
	assert.Error(t, w.Close(ctx)) // release UPDATE fails on the closed DB
}

func TestClaimIDDatabaseFailures(t *testing.T) {
	t.Run("expired row update", func(t *testing.T) {
		database := newDB(t)
		_, err := database.NewInsert().Model(&workerRow{ID: 7, Token: "old", ExpiresAt: -1}).Exec(t.Context())
		require.NoError(t, err)
		_, err = database.ExecContext(t.Context(), `CREATE TRIGGER deny_worker_update BEFORE UPDATE ON worker_ids BEGIN SELECT RAISE(ABORT, 'denied'); END`)
		require.NoError(t, err)
		_, err = claimID(t.Context(), database, "new", sysinfo{}, time.Minute.Milliseconds(), "0")
		assert.ErrorContains(t, err, "denied")
	})

	t.Run("new row insert", func(t *testing.T) {
		database := newDB(t)
		_, err := database.ExecContext(t.Context(), `CREATE TRIGGER deny_worker_insert BEFORE INSERT ON worker_ids BEGIN SELECT RAISE(ABORT, 'denied'); END`)
		require.NoError(t, err)
		_, err = claimID(t.Context(), database, "new", sysinfo{}, time.Minute.Milliseconds(), "0")
		assert.ErrorContains(t, err, "denied")
	})

	t.Run("closed database", func(t *testing.T) {
		database := newDB(t)
		require.NoError(t, database.Close())
		_, err := claimID(t.Context(), database, "new", sysinfo{}, time.Minute.Milliseconds(), "0")
		assert.Error(t, err)
	})
}

func TestWorkerTimingAndRepeatedLoss(t *testing.T) {
	worker := &Worker{lease: 0}
	worker.alive.Store(true)
	assert.Equal(t, time.Second, worker.renewInterval())
	worker.lease = time.Hour
	assert.Equal(t, renewRetryInterval, worker.retryInterval())
	worker.declareLost(errors.New("first"))
	worker.declareLost(errors.New("second"))
	assert.False(t, worker.Alive())
}
