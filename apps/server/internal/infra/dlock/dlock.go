// Package dlock provides a database-backed distributed lock that works across
// SQLite, MySQL and PostgreSQL. A lock is a single row in the dlocks table with
// a TTL lease; acquisition is a portable two-step "ensure row, then atomically
// take if free/expired" that needs no dialect-specific UPSERT.
//
// Duration model:
//   - TTL (WithTTL) is the lease length. If the holder crashes, the lock is
//     automatically reclaimable after the TTL — no lock is ever held forever.
//   - For critical sections longer than the TTL, enable WithAutoRefresh so a
//     background keep-alive renews the lease until Unlock. Without it you must
//     call Lock.Refresh yourself before the TTL elapses.
//   - WithMaxHold caps the total hold time even with auto-refresh, as a safety
//     valve against a hung holder: past the cap the keep-alive stops, the lease
//     expires, and OnLost fires.
//   - Release (Unlock) only clears the row if it is still ours, so releasing a
//     lock we have already lost never disturbs the new holder.
package dlock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/uptrace/bun"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
)

// DefaultTTL is the lease duration used when no WithTTL option is given.
const DefaultTTL = 30 * time.Second

// ErrNotAcquired is returned when a lock is currently held by someone else, and
// by Refresh when the lock is no longer ours.
var ErrNotAcquired = errors.New("dlock: not acquired")

// ErrMaxHoldExceeded is passed to OnLost when a lock is force-released because
// it exceeded its WithMaxHold cap.
var ErrMaxHoldExceeded = errors.New("dlock: max hold exceeded")

// lockRow is one row of the dlocks table (one row per lock name).
type lockRow struct {
	bun.BaseModel `bun:"table:dlocks"`

	Name      string `bun:"name,pk"`
	Owner     string `bun:"owner,notnull"`
	ExpiresAt int64  `bun:"expires_at,notnull"` // unix millis; a past value means free
}

// Locker acquires named distributed locks backed by database rows.
type Locker struct {
	db          *bun.DB
	nowExpr     string // SQL for the DB server's unix-millis clock
	ttl         time.Duration
	autoRefresh bool
	maxHold     time.Duration
	onLost      func(name string, err error)
}

// Option configures a Locker.
type Option func(*Locker)

// WithTTL sets the lease duration for acquired locks.
func WithTTL(ttl time.Duration) Option {
	return func(l *Locker) {
		if ttl > 0 {
			l.ttl = ttl
		}
	}
}

// WithAutoRefresh makes held locks renew their lease in the background until
// Unlock, so a critical section may safely run longer than the TTL.
func WithAutoRefresh() Option {
	return func(l *Locker) { l.autoRefresh = true }
}

// WithMaxHold caps the total time a lock may be auto-refreshed. Past the cap the
// keep-alive stops and OnLost fires with ErrMaxHoldExceeded. Zero means no cap.
func WithMaxHold(d time.Duration) Option {
	return func(l *Locker) { l.maxHold = d }
}

// WithOnLost registers a callback invoked when an auto-refreshed lock is lost
// (taken over, un-renewable, or past its max hold).
func WithOnLost(fn func(name string, err error)) Option {
	return func(l *Locker) { l.onLost = fn }
}

// New creates a Locker on the given database.
func New(db *bun.DB, opts ...Option) *Locker {
	l := &Locker{
		db:      db,
		nowExpr: bunx.NowMillisExpr(db.Dialect().Name().String()),
		ttl:     DefaultTTL,
	}
	for _, o := range opts {
		o(l)
	}
	return l
}

// Lock is a currently-held distributed lock.
type Lock struct {
	locker *Locker
	name   string
	owner  string

	cancel    context.CancelFunc
	wg        sync.WaitGroup
	alive     atomic.Bool
	lastRenew atomic.Int64

	mu     sync.Mutex
	closed bool
}

// Name returns the lock's name.
func (lk *Lock) Name() string { return lk.name }

// Owner returns the opaque owner token identifying this holder.
func (lk *Lock) Owner() string { return lk.owner }

// Alive reports whether the lock is still held. It is meaningful with
// WithAutoRefresh: it becomes false once the lease is lost or the max hold is
// exceeded. Long critical sections should check it before committing work.
func (lk *Lock) Alive() bool { return lk.alive.Load() }

func (l *Locker) newLock(name, owner string) *Lock {
	lk := &Lock{locker: l, name: name, owner: owner}
	lk.alive.Store(true)
	lk.lastRenew.Store(nowMillis())
	if l.autoRefresh {
		ctx, cancel := context.WithCancel(context.Background())
		lk.cancel = cancel
		lk.startRefresh(ctx)
	}
	return lk
}

// TryLock attempts to acquire name once, returning ErrNotAcquired if it is held.
func (l *Locker) TryLock(ctx context.Context, name string) (*Lock, error) {
	owner := newOwner()
	ok, err := l.acquire(ctx, name, owner)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotAcquired
	}
	return l.newLock(name, owner), nil
}

// Lock blocks with exponential backoff until name is acquired or ctx is done.
func (l *Locker) Lock(ctx context.Context, name string) (*Lock, error) {
	owner := newOwner()
	const (
		minBackoff = 20 * time.Millisecond
		maxBackoff = 500 * time.Millisecond
	)
	backoff := minBackoff
	for {
		ok, err := l.acquire(ctx, name, owner)
		if err != nil {
			return nil, err
		}
		if ok {
			return l.newLock(name, owner), nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < maxBackoff {
			if backoff *= 2; backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// acquire runs the portable two-step take: ensure the row exists as an expired
// placeholder, then atomically claim it if it is free/expired or already ours.
// Expiry is evaluated against the database clock (l.nowExpr), so nodes with
// skewed local clocks never reclaim a lease early.
func (l *Locker) acquire(ctx context.Context, name, owner string) (bool, error) {
	ttlMs := l.ttl.Milliseconds()

	if _, err := l.db.NewInsert().
		Model(&lockRow{Name: name, Owner: "", ExpiresAt: 0}).
		Ignore().Exec(ctx); err != nil {
		return false, err
	}

	res, err := l.db.NewUpdate().Model((*lockRow)(nil)).
		Set("owner = ?", owner).
		Set("expires_at = "+l.nowExpr+" + ?", ttlMs).
		Where("name = ?", name).
		Where("(expires_at < "+l.nowExpr+" OR owner = ?)", owner).
		Exec(ctx)
	if err != nil {
		return false, err
	}

	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// Refresh extends the lease, returning ErrNotAcquired if the lock is no longer
// ours (e.g. it expired and was taken over).
func (lk *Lock) Refresh(ctx context.Context) error {
	res, err := lk.locker.db.NewUpdate().Model((*lockRow)(nil)).
		Set("expires_at = "+lk.locker.nowExpr+" + ?", lk.locker.ttl.Milliseconds()).
		Where("name = ?", lk.name).
		Where("owner = ?", lk.owner).
		Exec(ctx)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrNotAcquired
	}
	return nil
}

func (lk *Lock) startRefresh(ctx context.Context) {
	ttl := lk.locker.ttl
	interval := ttl / 3
	if interval <= 0 {
		interval = time.Second
	}
	acquiredAt := time.Now()

	lk.wg.Add(1)
	go func() {
		defer lk.wg.Done()
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if lk.refreshTick(ctx, acquiredAt) {
					return
				}
			}
		}
	}()
}

// refreshTick performs one keep-alive attempt; it returns true when the loop
// should stop.
func (lk *Lock) refreshTick(ctx context.Context, acquiredAt time.Time) (done bool) {
	if lk.locker.maxHold > 0 && time.Since(acquiredAt) >= lk.locker.maxHold {
		lk.declareLost(ErrMaxHoldExceeded)
		return true
	}

	err := lk.Refresh(ctx)
	switch {
	case err == nil:
		lk.lastRenew.Store(nowMillis())
		return false
	case errors.Is(err, ErrNotAcquired):
		lk.declareLost(ErrNotAcquired)
		return true
	default:
		// Transient DB error: retry until we can no longer guarantee the lease.
		margin := lk.locker.ttl / 3
		if nowMillis()-lk.lastRenew.Load() >= (lk.locker.ttl - margin).Milliseconds() {
			lk.declareLost(err)
			return true
		}
		return false
	}
}

func (lk *Lock) declareLost(err error) {
	lk.alive.Store(false)
	if fn := lk.locker.onLost; fn != nil {
		fn(lk.name, err)
	}
}

// Unlock stops any keep-alive and releases the lock if it is still held by us.
// Releasing a lock we no longer own is a safe no-op. Unlock is idempotent.
func (lk *Lock) Unlock(ctx context.Context) error {
	lk.mu.Lock()
	if lk.closed {
		lk.mu.Unlock()
		return nil
	}
	lk.closed = true
	lk.mu.Unlock()

	lk.alive.Store(false)
	if lk.cancel != nil {
		lk.cancel()
	}
	lk.wg.Wait()

	_, err := lk.locker.db.NewUpdate().Model((*lockRow)(nil)).
		Set("owner = ?", "").
		Set("expires_at = ?", int64(0)).
		Where("name = ?", lk.name).
		Where("owner = ?", lk.owner).
		Exec(ctx)
	return err
}

func nowMillis() int64 { return time.Now().UnixMilli() }

// newOwner returns a random opaque token identifying a lock holder.
func newOwner() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
