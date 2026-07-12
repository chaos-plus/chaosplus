// Package wuid allocates a process-unique worker id for building Sonyflake-style
// ids across a cluster. It supports two deployment models, tried in order by
// Open:
//
//  1. Static via env (WUID=<n>) — explicit, no lease needed.
//  2. Database lease coordinated by the dlock distributed lock — for ephemeral
//     nodes (Deployments, VMs) with no stable identity. Allocation reuses the
//     lowest expired slot or creates a new id, stamping the row with a random
//     token kept only in memory. A background heartbeat renews the lease with
//     that token as an optimistic lock; if renewal fails past the safety margin,
//     or the token is taken over, the id is declared lost (id generation stops)
//     and the worker keeps trying to re-allocate a fresh id in the background.
//
// The id space (0..MaxWorkerID) matches Sonyflake's 16-bit machine id, so
// Worker.ID() feeds directly into the guid package.
package wuid

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"runtime"
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
// It is deliberately long (hour-level) so the heartbeat is infrequent, which
// keeps database chatter — and serverless cost — low for stable nodes.
const DefaultLease = time.Hour

// renewRetryInterval is how often the heartbeat retries after a failed renewal
// or while re-allocating a lost id, instead of waiting a full renew interval.
const renewRetryInterval = 5 * time.Second

// EnvKey is the environment variable Open reads for an explicit worker id.
const EnvKey = "WUID"

const allocLockName = "wuid:alloc"

// errTakenOver marks a definitive lease loss (another node holds the row under a
// different token), distinct from a transient database error which is retried.
var errTakenOver = errors.New("wuid: lease taken over")

// workerRow is one row of the worker_ids table (one row per allocated id). The
// diagnostic columns are flattened node info; token is the optimistic lock.
type workerRow struct {
	bun.BaseModel `bun:"table:worker_ids"`

	ID        int    `bun:"id,pk"` // worker id, app-assigned (not auto-increment)
	Token     string `bun:"token,notnull"`
	ExpiresAt int64  `bun:"expires_at,notnull"` // unix millis

	OS        string `bun:"os"`
	Host      string `bun:"host"`
	IPv4Lan   string `bun:"ipv4_lan"` // comma-separated
	MAC       string `bun:"mac"`      // comma-separated
	Disk      string `bun:"disk"`     // JSON array
	Container bool   `bun:"container"`
	KVM       bool   `bun:"kvm"`
}

type config struct {
	lease       time.Duration
	onLost      func(error)
	onReacquire func(uint16)
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
// its lease. Treat this as "id generation must stop": callers should refuse to
// mint ids until OnReacquire fires, otherwise another node may reuse the id.
func OnLost(fn func(error)) Option {
	return func(c *config) { c.onLost = fn }
}

// OnReacquire registers a callback invoked when a previously-lost worker manages
// to allocate a fresh id in the background, so callers can resume id generation
// with the new id.
func OnReacquire(fn func(uint16)) Option {
	return func(c *config) { c.onReacquire = fn }
}

func newConfig(opts ...Option) config {
	c := config{lease: DefaultLease}
	for _, o := range opts {
		o(&c)
	}
	return c
}

// Worker is a worker id. A leased worker renews its lease in the background until
// Close; a static worker (env / ordinal) has no lease and is always Alive. On a
// leased worker the id may change if the lease is lost and re-allocated.
type Worker struct {
	db      *bun.DB
	nowExpr string // SQL for the DB server's unix-millis clock
	lease   time.Duration
	static  bool

	id    atomic.Int64 // current worker id (changes on re-allocation)
	token atomic.Value // string; current lease token

	cancel context.CancelFunc
	wg     sync.WaitGroup

	alive     atomic.Bool
	lastRenew atomic.Int64 // unix millis of the last successful renew/alloc

	mu          sync.Mutex
	onLost      func(error)
	onReacquire func(uint16)
	closed      bool
}

// ID returns the worker id, ready to use as a Sonyflake machine id.
func (w *Worker) ID() uint16 { return uint16(w.id.Load()) }

// Alive reports whether the worker id is still valid to generate with. It is
// always true for static workers, and becomes false for a leased worker while
// its lease is lost (until it re-allocates). Callers must stop generating ids
// when this is false.
func (w *Worker) Alive() bool { return w.alive.Load() }

// newStatic returns a static worker for an externally-guaranteed-unique id (the
// WUID env var or a StatefulSet ordinal). No lease, no heartbeat, always Alive.
func newStatic(id uint16) *Worker {
	w := &Worker{static: true}
	w.id.Store(int64(id))
	w.alive.Store(true)
	return w
}

// Open picks a worker id using, in order: the WUID env var and then an auto
// database lease. On the lease path the returned Worker owns a lease + heartbeat,
// so callers must Close it.
func Open(ctx context.Context, db *bun.DB, opts ...Option) (*Worker, error) {
	if id, ok, err := fromEnv(EnvKey); err != nil {
		return nil, err
	} else if ok {
		return newStatic(id), nil
	}

	return allocate(ctx, db, opts...)
}

// allocate acquires a worker id via a database lease, skipping the static
// (env/ordinal) sources. Open calls it once those are exhausted; tests call it
// directly to drive the lease path deterministically.
func allocate(ctx context.Context, db *bun.DB, opts ...Option) (*Worker, error) {
	cfg := newConfig(opts...)
	nowExpr := bunx.NowMillisExpr(db.Dialect().Name().String())

	id, token, err := claimUnderLock(ctx, db, cfg.lease, nowExpr)
	if err != nil {
		return nil, err
	}

	w := &Worker{db: db, nowExpr: nowExpr, lease: cfg.lease, onLost: cfg.onLost, onReacquire: cfg.onReacquire}
	w.id.Store(int64(id))
	w.token.Store(token)
	w.alive.Store(true)
	w.lastRenew.Store(time.Now().UnixMilli())

	hbCtx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.wg.Add(1)
	go w.run(hbCtx)

	return w, nil
}

// claimUnderLock claims a worker id under the shared allocation lock and returns
// it with a fresh random token.
func claimUnderLock(ctx context.Context, db *bun.DB, lease time.Duration, nowExpr string) (int, string, error) {
	token := newToken()
	si := collectSysinfo()

	locker := dlock.New(db, dlock.WithTTL(lease))
	lk, err := locker.Lock(ctx, allocLockName)
	if err != nil {
		return 0, "", fmt.Errorf("wuid: acquire alloc lock: %w", err)
	}
	id, err := claimID(ctx, db, token, si, lease.Milliseconds(), nowExpr)
	_ = lk.Unlock(context.WithoutCancel(ctx))
	if err != nil {
		return 0, "", err
	}
	return id, token, nil
}

// claimID picks and claims a worker id inside the allocation lock: it reuses the
// lowest expired slot when one exists, otherwise allocates MAX(id)+1. Expiry is
// evaluated against the database clock (nowExpr) so a leased id is never reclaimed
// early because of a skewed local clock.
func claimID(ctx context.Context, db *bun.DB, token string, si sysinfo, leaseMs int64, nowExpr string) (int, error) {
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
			Set("token = ?", token).
			Set("expires_at = "+nowExpr+" + ?", leaseMs).
			Set("os = ?", si.OS).
			Set("host = ?", si.Host).
			Set("ipv4_lan = ?", si.IPv4Lan).
			Set("mac = ?", si.MAC).
			Set("disk = ?", si.Disk).
			Set("container = ?", si.Container).
			Set("kvm = ?", si.KVM).
			Where("id = ?", reuse).
			Where("expires_at < " + nowExpr).
			Exec(ctx)
		if uerr != nil {
			return 0, uerr
		}
		if n, _ := res.RowsAffected(); n == 1 {
			return reuse, nil
		}
		// Lost the race for this slot under the lock (extremely unlikely); fall
		// through to allocate a new id.
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

	row := &workerRow{
		ID: next, Token: token,
		OS: si.OS, Host: si.Host, IPv4Lan: si.IPv4Lan, MAC: si.MAC,
		Disk: si.Disk, Container: si.Container, KVM: si.KVM,
	}
	if _, err := db.NewInsert().Model(row).
		Value("expires_at", nowExpr+" + ?", leaseMs).
		Exec(ctx); err != nil {
		return 0, err
	}
	return next, nil
}

// run is the background heartbeat/recovery loop. It renews the lease on the renew
// interval; on failure it retries quickly, and once the lease is lost it keeps
// trying to re-allocate a fresh id.
func (w *Worker) run(ctx context.Context) {
	defer w.wg.Done()
	timer := time.NewTimer(w.renewInterval())
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		timer.Reset(w.step(ctx))
	}
}

// step performs one heartbeat action and returns the delay until the next one.
func (w *Worker) step(ctx context.Context) time.Duration {
	if !w.alive.Load() {
		// Lost: keep trying to re-allocate a fresh id so the process can recover.
		if err := w.reacquire(ctx); err != nil {
			slog.Error("wuid: re-allocation failed, will retry", "err", err)
			return w.retryInterval()
		}
		return w.renewInterval()
	}

	err := w.renew(ctx)
	switch {
	case err == nil:
		w.lastRenew.Store(time.Now().UnixMilli())
		return w.renewInterval()
	case errors.Is(err, errTakenOver):
		w.declareLost(fmt.Errorf("wuid: worker id %d taken over", w.ID()))
		return w.retryInterval()
	default:
		// Transient error: keep the id (the lease is still valid) and retry fast,
		// until we can no longer guarantee it (a margin before actual expiry).
		margin := w.lease / 3
		if time.Now().UnixMilli()-w.lastRenew.Load() >= (w.lease - margin).Milliseconds() {
			w.declareLost(fmt.Errorf("wuid: could not renew worker id %d: %w", w.ID(), err))
			return w.retryInterval()
		}
		slog.Error("wuid: renew failed, will retry", "worker_id", w.ID(), "err", err)
		return w.retryInterval()
	}
}

func (w *Worker) renewInterval() time.Duration {
	iv := w.lease / 3
	if iv <= 0 {
		iv = time.Second
	}
	return iv
}

// retryInterval is how long to wait before the next renew/re-allocate attempt
// after a failure: a few seconds in production, but never longer than the renew
// interval so short-lease tests converge quickly.
func (w *Worker) retryInterval() time.Duration {
	if iv := w.renewInterval(); iv < renewRetryInterval {
		return iv
	}
	return renewRetryInterval
}

// renew extends the lease, using the in-memory token as an optimistic lock: if a
// different node has taken the id (token no longer matches), 0 rows update.
func (w *Worker) renew(ctx context.Context) error {
	res, err := w.db.NewUpdate().Model((*workerRow)(nil)).
		Set("expires_at = "+w.nowExpr+" + ?", w.lease.Milliseconds()).
		Where("id = ?", w.ID()).
		Where("token = ?", w.token.Load().(string)).
		Exec(ctx)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errTakenOver
	}
	return nil
}

// reacquire allocates a fresh id after the previous one was lost, and notifies
// the caller (onReacquire) so it can resume id generation.
func (w *Worker) reacquire(ctx context.Context) error {
	id, token, err := claimUnderLock(ctx, w.db, w.lease, w.nowExpr)
	if err != nil {
		return err
	}
	w.id.Store(int64(id))
	w.token.Store(token)
	w.lastRenew.Store(time.Now().UnixMilli())
	w.alive.Store(true)
	slog.Info("wuid: re-allocated worker id; id generation resumed", "worker_id", id)

	w.mu.Lock()
	fn := w.onReacquire
	w.mu.Unlock()
	if fn != nil {
		fn(uint16(id))
	}
	return nil
}

// declareLost marks the id lost (id generation must stop) and fires onLost, once.
func (w *Worker) declareLost(err error) {
	if !w.alive.CompareAndSwap(true, false) {
		return
	}
	slog.Error("wuid: worker id lost; id generation suspended", "worker_id", w.ID(), "err", err)

	w.mu.Lock()
	fn := w.onLost
	w.mu.Unlock()
	if fn != nil {
		fn(err)
	}
}

// Close stops the heartbeat and releases a leased worker id for reuse. It is safe
// to call multiple times and is a no-op for static workers.
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
		Where("id = ?", w.ID()).
		Where("token = ?", w.token.Load().(string)).
		Exec(ctx)
	return err
}

// sysinfo is the flattened node diagnostics stored on a claimed row.
type sysinfo struct {
	OS        string
	Host      string
	IPv4Lan   string
	MAC       string
	Disk      string
	Container bool
	KVM       bool
}

func collectSysinfo() sysinfo {
	host, _ := os.Hostname()
	return sysinfo{
		OS:        runtime.GOOS + "/" + runtime.GOARCH,
		Host:      host,
		IPv4Lan:   allIPv4(),
		MAC:       allMAC(),
		Disk:      disksJSON(),
		Container: isContainer(),
		KVM:       isKVM(),
	}
}

// allIPv4 returns every non-loopback IPv4 address, comma-separated.
func allIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	var ips []string
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback != 0 || ifc.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && !n.IP.IsLoopback() {
				ips = append(ips, n.IP.String())
			}
		}
	}
	return strings.Join(ips, ",")
}

// allMAC returns every non-loopback hardware address, comma-separated.
func allMAC() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	var macs []string
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback != 0 || len(ifc.HardwareAddr) == 0 {
			continue
		}
		macs = append(macs, ifc.HardwareAddr.String())
	}
	return strings.Join(macs, ",")
}

// disksJSON is a best-effort JSON array of block device names (Linux); it is
// "[]" on platforms where it cannot be determined.
func disksJSON() string {
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return "[]"
	}
	disks := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") {
			continue
		}
		disks = append(disks, name)
	}
	b, _ := json.Marshal(disks)
	return string(b)
}

// isContainer is a best-effort check for running inside a container.
func isContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		s := string(data)
		if strings.Contains(s, "docker") || strings.Contains(s, "kubepods") || strings.Contains(s, "containerd") {
			return true
		}
	}
	return os.Getenv("KUBERNETES_SERVICE_HOST") != ""
}

// isKVM is a best-effort check for running inside a virtual machine (Linux).
func isKVM() bool {
	if data, err := os.ReadFile("/proc/cpuinfo"); err == nil && strings.Contains(string(data), "hypervisor") {
		return true
	}
	if data, err := os.ReadFile("/sys/class/dmi/id/product_name"); err == nil {
		s := strings.ToLower(string(data))
		if strings.Contains(s, "kvm") || strings.Contains(s, "qemu") || strings.Contains(s, "virtual") {
			return true
		}
	}
	return false
}

// fromEnv reads a worker id from the given environment variable. ok is false when
// the variable is unset/empty.
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

func newToken() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
