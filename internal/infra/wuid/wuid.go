// Package wuid allocates a process-unique worker id for building Sonyflake-style
// ids across a cluster. It supports three deployment models, tried in order by
// Open:
//
//  1. Static via env (WUID=<n>) — explicit, no lease needed.
//  2. Kubernetes StatefulSet ordinal parsed from the hostname (opt-in) — stable
//     per-pod, no lease needed.
//  3. Database lease coordinated by the dlock distributed lock — for ephemeral
//     nodes (Deployments, VMs) with no stable identity. A background heartbeat
//     renews the lease; if it can no longer be renewed the worker is marked not
//     Alive and onLost fires, so the process can fail-stop before another node
//     reuses the id. A leased id is never "acquire once, use forever".
//
// The id space (0..MaxWorkerID) matches Sonyflake's 16-bit machine id, so
// Worker.ID() feeds directly into the guid package.
package wuid

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/uptrace/bun"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/chaos-plus/chaosplus/internal/infra/dlock"
)

// MaxWorkerID is the largest allocatable worker id, bounded by Sonyflake's
// 16-bit machine id.
const MaxWorkerID = 1<<16 - 1 // 65535

// DefaultLease is the worker-id lease duration when no WithLease option is set.
const DefaultLease = 30 * time.Second

// EnvKey is the environment variable Open reads for an explicit worker id.
const EnvKey = "WUID"

const allocLockName = "wuid:alloc"

// errTakenOver marks a definitive lease loss (another owner holds the row),
// distinct from a transient database error which is retried.
var errTakenOver = errors.New("wuid: lease taken over")

// workerRow is one row of the worker_ids table (one row per allocated id).
type workerRow struct {
	bun.BaseModel `bun:"table:worker_ids"`

	ID        int    `bun:"id,pk"` // worker id, app-assigned (not auto-increment)
	Owner     string `bun:"owner,notnull"`
	Host      string `bun:"host,notnull"`
	ExpiresAt int64  `bun:"expires_at,notnull"` // unix millis
}

type config struct {
	lease      time.Duration
	onLost     func(error)
	useOrdinal bool
}

// Option configures worker-id allocation.
type Option func(*config)

// WithLease sets the lease duration renewed by the heartbeat.
func WithLease(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.lease = d
		}
	}
}

// OnLost registers a callback invoked when a leased worker can no longer renew
// its lease. Treat this as fatal: stop generating ids and shut the process down,
// otherwise another node may reuse the id and produce duplicates.
func OnLost(fn func(error)) Option {
	return func(c *config) { c.onLost = fn }
}

// WithHostnameOrdinal lets Open derive the worker id from a Kubernetes
// StatefulSet pod ordinal (hostname ending in "-<n>"). Off by default because a
// Deployment pod's random suffix could coincidentally look like an ordinal.
func WithHostnameOrdinal() Option {
	return func(c *config) { c.useOrdinal = true }
}

func newConfig(opts ...Option) config {
	c := config{lease: DefaultLease}
	for _, o := range opts {
		o(&c)
	}
	return c
}

// Worker is a worker id. A leased worker renews its lease in the background
// until Close; a static worker (env / ordinal) has no lease and is always Alive.
type Worker struct {
	db      *bun.DB
	nowExpr string // SQL for the DB server's unix-millis clock
	id      int
	owner   string
	lease   time.Duration
	static  bool

	cancel context.CancelFunc
	wg     sync.WaitGroup

	alive     atomic.Bool
	lastRenew atomic.Int64 // unix millis of the last successful renew/alloc

	mu     sync.Mutex
	onLost func(error)
	closed bool
}

// ID returns the worker id, ready to use as a Sonyflake machine id.
func (w *Worker) ID() uint16 { return uint16(w.id) }

// Alive reports whether the worker id is still valid to generate with. It is
// always true for static workers, and becomes false for a leased worker once
// its lease is lost. Callers should stop generating ids when this is false.
func (w *Worker) Alive() bool { return w.alive.Load() }

// newStatic returns a static worker for an externally-guaranteed-unique id
// (the WUID env var or a StatefulSet ordinal). It has no lease or heartbeat and
// is always Alive.
func newStatic(id uint16) *Worker {
	w := &Worker{id: int(id), static: true}
	w.alive.Store(true)
	return w
}

// Open picks a worker id using, in order: the WUID env var, the Kubernetes
// StatefulSet ordinal (only when WithHostnameOrdinal is set), and finally a
// database lease. This makes the same code work across static config, K8s
// StatefulSets, and ephemeral Deployment/VM nodes. The returned Worker owns a
// lease + heartbeat on the database path, so callers must Close it.
func Open(ctx context.Context, db *bun.DB, opts ...Option) (*Worker, error) {
	cfg := newConfig(opts...)

	if id, ok, err := fromEnv(EnvKey); err != nil {
		return nil, err
	} else if ok {
		return newStatic(id), nil
	}

	if cfg.useOrdinal {
		if id, ok, err := fromHostnameOrdinal(); err != nil {
			return nil, err
		} else if ok {
			return newStatic(id), nil
		}
	}

	return allocate(ctx, db, opts...)
}

// allocate acquires a worker id via a database lease, skipping the static
// (env/ordinal) sources. Open calls it once those are exhausted; tests call
// it directly to drive the lease path deterministically.
func allocate(ctx context.Context, db *bun.DB, opts ...Option) (*Worker, error) {
	cfg := newConfig(opts...)
	locker := dlock.New(db, dlock.WithTTL(cfg.lease))
	lk, err := locker.Lock(ctx, allocLockName)
	if err != nil {
		return nil, fmt.Errorf("wuid: acquire alloc lock: %w", err)
	}
	defer func() { _ = lk.Unlock(context.WithoutCancel(ctx)) }()

	host, _ := os.Hostname()
	owner := newOwner()

	// Judge expiry by the database clock (nowExpr), evaluated inside each
	// statement, so nodes with skewed local clocks agree on when a slot is free.
	nowExpr := bunx.NowMillisExpr(db.Dialect().Name().String())

	id, err := claimID(ctx, db, owner, host, cfg.lease.Milliseconds(), nowExpr)
	if err != nil {
		return nil, err
	}

	w := &Worker{db: db, nowExpr: nowExpr, id: id, owner: owner, lease: cfg.lease, onLost: cfg.onLost}
	w.alive.Store(true)
	w.lastRenew.Store(time.Now().UnixMilli())

	hbCtx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.startHeartbeat(hbCtx)

	return w, nil
}

// claimID picks and claims a worker id inside the allocation lock. Expiry is
// evaluated against the database clock (nowExpr) in every statement, so a leased
// id is never reclaimed early because of a skewed local clock; leaseMs is the
// lease length in milliseconds added to that clock.
func claimID(ctx context.Context, db *bun.DB, owner, host string, leaseMs int64, nowExpr string) (int, error) {
	// 1. Prefer reusing the lowest expired slot.
	var reuse int
	err := db.NewSelect().Model((*workerRow)(nil)).
		Column("id").
		Where("expires_at < "+nowExpr).
		Order("id ASC").Limit(1).
		Scan(ctx, &reuse)
	switch {
	case err == nil:
		res, uerr := db.NewUpdate().Model((*workerRow)(nil)).
			Set("owner = ?", owner).
			Set("host = ?", host).
			Set("expires_at = "+nowExpr+" + ?", leaseMs).
			Where("id = ?", reuse).
			Where("expires_at < " + nowExpr).
			Exec(ctx)
		if uerr != nil {
			return 0, uerr
		}
		if n, _ := res.RowsAffected(); n == 1 {
			return reuse, nil
		}
		// Extremely unlikely under the lock; fall through to allocate a new id.
	case errors.Is(err, sql.ErrNoRows):
		// No expired slot to reuse.
	default:
		return 0, err
	}

	// 2. Allocate the next new id.
	var maxID int
	if err := db.NewSelect().Model((*workerRow)(nil)).
		ColumnExpr("COALESCE(MAX(id), -1)").Scan(ctx, &maxID); err != nil {
		return 0, err
	}
	next := maxID + 1
	if next > MaxWorkerID {
		return 0, fmt.Errorf("wuid: worker id pool exhausted (max %d)", MaxWorkerID)
	}

	if _, err := db.NewInsert().Model(&workerRow{ID: next, Owner: owner, Host: host}).
		Value("expires_at", nowExpr+" + ?", leaseMs).
		Exec(ctx); err != nil {
		return 0, err
	}
	return next, nil
}

func (w *Worker) startHeartbeat(ctx context.Context) {
	interval := w.lease / 3
	if interval <= 0 {
		interval = time.Second
	}

	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if done := w.tick(ctx); done {
					return
				}
			}
		}
	}()
}

// tick performs one heartbeat. It returns true when the heartbeat loop should
// stop (the lease was definitively lost, or transient failures ran out of
// safety margin before expiry).
func (w *Worker) tick(ctx context.Context) (done bool) {
	err := w.renew(ctx)
	switch {
	case err == nil:
		w.lastRenew.Store(time.Now().UnixMilli())
		return false
	case errors.Is(err, errTakenOver):
		// Another node owns the id — definitively lost.
		w.declareLost(fmt.Errorf("wuid: worker id %d taken over", w.id))
		return true
	default:
		// Transient error: keep retrying until we can no longer guarantee the
		// lease (one heartbeat interval of slack before the lease expires).
		margin := w.lease / 3
		if time.Now().UnixMilli()-w.lastRenew.Load() >= (w.lease - margin).Milliseconds() {
			w.declareLost(fmt.Errorf("wuid: could not renew worker id %d: %w", w.id, err))
			return true
		}
		return false
	}
}

func (w *Worker) renew(ctx context.Context) error {
	res, err := w.db.NewUpdate().Model((*workerRow)(nil)).
		Set("expires_at = "+w.nowExpr+" + ?", w.lease.Milliseconds()).
		Where("id = ?", w.id).
		Where("owner = ?", w.owner).
		Exec(ctx)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errTakenOver
	}
	return nil
}

func (w *Worker) declareLost(err error) {
	w.alive.Store(false)
	w.mu.Lock()
	fn := w.onLost
	w.mu.Unlock()
	if fn != nil {
		fn(err)
	}
}

// Close stops the heartbeat and releases a leased worker id for reuse. It is
// safe to call multiple times and is a no-op for static workers.
func (w *Worker) Close(ctx context.Context) error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	w.mu.Unlock()

	w.alive.Store(false)

	if w.static {
		return nil
	}

	if w.cancel != nil {
		w.cancel()
	}
	w.wg.Wait()

	_, err := w.db.NewUpdate().Model((*workerRow)(nil)).
		Set("expires_at = ?", int64(0)).
		Where("id = ?", w.id).
		Where("owner = ?", w.owner).
		Exec(ctx)
	return err
}

// fromEnv reads a worker id from the given environment variable. ok is false
// when the variable is unset/empty.
func fromEnv(key string) (id uint16, ok bool, err error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return 0, false, nil
	}
	n, perr := strconv.ParseUint(v, 10, 64)
	if perr != nil {
		return 0, false, fmt.Errorf("wuid: invalid %s %q: %w", key, v, perr)
	}
	if n > MaxWorkerID {
		return 0, false, fmt.Errorf("wuid: %s=%d exceeds max %d", key, n, MaxWorkerID)
	}
	return uint16(n), true, nil
}

var ordinalRe = regexp.MustCompile(`-(\d+)$`)

// fromHostnameOrdinal derives a worker id from a Kubernetes StatefulSet pod
// ordinal (hostname ending in "-<n>", e.g. "api-3"). ok is false when the
// hostname does not end in an ordinal.
func fromHostnameOrdinal() (id uint16, ok bool, err error) {
	host, herr := os.Hostname()
	if herr != nil {
		return 0, false, herr
	}
	return parseOrdinal(host)
}

func parseOrdinal(host string) (id uint16, ok bool, err error) {
	m := ordinalRe.FindStringSubmatch(host)
	if m == nil {
		return 0, false, nil
	}
	n, perr := strconv.ParseUint(m[1], 10, 64)
	if perr != nil {
		return 0, false, nil
	}
	if n > MaxWorkerID {
		return 0, false, fmt.Errorf("wuid: hostname ordinal %d exceeds max %d", n, MaxWorkerID)
	}
	return uint16(n), true, nil
}

func newOwner() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
