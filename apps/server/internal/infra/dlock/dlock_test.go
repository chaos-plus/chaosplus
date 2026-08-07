package dlock

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
)

func newDB(t *testing.T) *bun.DB {
	t.Helper()
	database, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	require.NoError(t, Migrate(context.Background(), database))
	return database
}

func TestTryLock_AcquireContendRelease(t *testing.T) {
	ctx := context.Background()
	l := New(newDB(t))

	lk, err := l.TryLock(ctx, "job")
	require.NoError(t, err)
	assert.Equal(t, "job", lk.Name())
	assert.NotEmpty(t, lk.Owner())

	// A second holder cannot acquire while it is held.
	_, err = l.TryLock(ctx, "job")
	assert.ErrorIs(t, err, ErrNotAcquired)

	// After release it can be acquired again.
	require.NoError(t, lk.Unlock(ctx))
	lk2, err := l.TryLock(ctx, "job")
	require.NoError(t, err)
	assert.NotEqual(t, lk.Owner(), lk2.Owner())
}

func TestLock_BlocksUntilReleased(t *testing.T) {
	ctx := context.Background()
	l := New(newDB(t))

	held, err := l.TryLock(ctx, "res")
	require.NoError(t, err)

	acquired := make(chan *Lock, 1)
	go func() {
		lk, lerr := l.Lock(ctx, "res")
		if lerr == nil {
			acquired <- lk
		}
	}()

	// Should still be blocked while we hold it.
	select {
	case <-acquired:
		t.Fatal("Lock returned while the lock was still held")
	case <-time.After(150 * time.Millisecond):
	}

	require.NoError(t, held.Unlock(ctx))

	select {
	case lk := <-acquired:
		assert.Equal(t, "res", lk.Name())
	case <-time.After(2 * time.Second):
		t.Fatal("Lock did not acquire after release")
	}
}

func TestLock_ContextCancelled(t *testing.T) {
	l := New(newDB(t))

	held, err := l.TryLock(context.Background(), "x")
	require.NoError(t, err)
	defer func() { _ = held.Unlock(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()

	_, err = l.Lock(ctx, "x")
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestLock_ExpiryTakeover(t *testing.T) {
	ctx := context.Background()
	l := New(newDB(t), WithTTL(80*time.Millisecond))

	a, err := l.TryLock(ctx, "lease")
	require.NoError(t, err)

	// Wait for the lease to expire, then a different holder takes over.
	time.Sleep(150 * time.Millisecond)
	b, err := l.TryLock(ctx, "lease")
	require.NoError(t, err)
	assert.NotEqual(t, a.Owner(), b.Owner())

	// The original holder can no longer refresh — it was taken over.
	assert.ErrorIs(t, a.Refresh(ctx), ErrNotAcquired)
}

func TestRefresh_And_UnlockedRefreshFails(t *testing.T) {
	ctx := context.Background()
	l := New(newDB(t))

	lk, err := l.TryLock(ctx, "hb")
	require.NoError(t, err)

	// Refresh while held succeeds.
	require.NoError(t, lk.Refresh(ctx))

	// After releasing, refresh reports the lock is no longer ours.
	require.NoError(t, lk.Unlock(ctx))
	assert.ErrorIs(t, lk.Refresh(ctx), ErrNotAcquired)
}

func TestMutualExclusion(t *testing.T) {
	ctx := context.Background()
	l := New(newDB(t))

	const workers = 8
	const rounds = 5
	var active int32
	var total int32

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				lk, err := l.Lock(ctx, "mutex")
				if err != nil {
					t.Errorf("Lock: %v", err)
					return
				}
				if n := atomic.AddInt32(&active, 1); n != 1 {
					t.Errorf("mutual exclusion violated: %d holders", n)
				}
				atomic.AddInt32(&total, 1)
				time.Sleep(time.Millisecond)
				atomic.AddInt32(&active, -1)
				_ = lk.Unlock(ctx)
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, int32(workers*rounds), total)
}

func TestWithTTL_ZeroIgnored(t *testing.T) {
	l := New(newDB(t), WithTTL(0))
	assert.Equal(t, DefaultTTL, l.ttl)
}

func TestAutoRefresh_HoldsBeyondTTL(t *testing.T) {
	ctx := context.Background()
	l := New(newDB(t), WithTTL(120*time.Millisecond), WithAutoRefresh())

	lk, err := l.TryLock(ctx, "long")
	require.NoError(t, err)

	// Well beyond the TTL — auto-refresh must keep it held.
	time.Sleep(400 * time.Millisecond)
	assert.True(t, lk.Alive())

	_, err = l.TryLock(ctx, "long")
	assert.ErrorIs(t, err, ErrNotAcquired, "auto-refreshed lock should still be held")

	require.NoError(t, lk.Unlock(ctx))
	lk2, err := l.TryLock(ctx, "long")
	require.NoError(t, err, "lock should be free after unlock")
	t.Cleanup(func() { _ = lk2.Unlock(ctx) })
}

func TestAutoRefresh_TakenOver(t *testing.T) {
	ctx := context.Background()
	database := newDB(t)

	lost := make(chan error, 1)
	l := New(database, WithTTL(150*time.Millisecond), WithAutoRefresh(),
		WithOnLost(func(_ string, e error) { lost <- e }))

	lk, err := l.TryLock(ctx, "steal")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lk.Unlock(ctx) })

	// Another node steals the row.
	_, err = database.NewUpdate().Model((*lockRow)(nil)).
		Set("owner = ?", "thief").Where("name = ?", "steal").Exec(ctx)
	require.NoError(t, err)

	select {
	case e := <-lost:
		assert.ErrorIs(t, e, ErrNotAcquired)
	case <-time.After(2 * time.Second):
		t.Fatal("OnLost not called after takeover")
	}
	assert.False(t, lk.Alive())
}

func TestMaxHold_ForceReleases(t *testing.T) {
	ctx := context.Background()

	lost := make(chan error, 1)
	l := New(newDB(t), WithTTL(60*time.Millisecond), WithAutoRefresh(),
		WithMaxHold(120*time.Millisecond),
		WithOnLost(func(_ string, e error) { lost <- e }))

	lk, err := l.TryLock(ctx, "cap")
	require.NoError(t, err)

	select {
	case e := <-lost:
		assert.ErrorIs(t, e, ErrMaxHoldExceeded)
	case <-time.After(2 * time.Second):
		t.Fatal("OnLost not called after max hold")
	}
	assert.False(t, lk.Alive())
}

func TestAutoRefresh_TransientLost(t *testing.T) {
	ctx := context.Background()
	database := newDB(t)

	lost := make(chan error, 1)
	l := New(database, WithTTL(120*time.Millisecond), WithAutoRefresh(),
		WithOnLost(func(_ string, e error) { lost <- e }))

	lk, err := l.TryLock(ctx, "trans")
	require.NoError(t, err)
	_ = lk

	require.NoError(t, database.Close()) // renews now fail transiently

	select {
	case <-lost:
	case <-time.After(2 * time.Second):
		t.Fatal("OnLost not called after repeated renew failures")
	}
	assert.False(t, lk.Alive())
}

func TestAutoRefresh_UnlockIdempotent(t *testing.T) {
	ctx := context.Background()
	l := New(newDB(t), WithAutoRefresh())

	lk, err := l.TryLock(ctx, "a")
	require.NoError(t, err)
	assert.True(t, lk.Alive())

	require.NoError(t, lk.Unlock(ctx))
	assert.False(t, lk.Alive())
	require.NoError(t, lk.Unlock(ctx)) // second call is a no-op
}

func TestErrors_OnClosedDB(t *testing.T) {
	ctx := context.Background()
	database := newDB(t)
	l := New(database)

	lk, err := l.TryLock(ctx, "z")
	require.NoError(t, err)

	require.NoError(t, database.Close())

	// Every operation now surfaces the database error.
	_, err = l.TryLock(ctx, "z")
	assert.Error(t, err)

	_, err = l.Lock(ctx, "z")
	assert.Error(t, err)

	assert.Error(t, lk.Refresh(ctx))
	assert.Error(t, lk.Unlock(ctx))
}
