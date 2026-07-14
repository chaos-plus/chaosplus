package guid

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/uptrace/bun"
	"google.golang.org/grpc"

	"github.com/chaos-plus/chaosplus/internal/infra/dlock"
	guidapi "github.com/chaos-plus/chaosplus/internal/infra/guid/api"
	"github.com/chaos-plus/chaosplus/internal/infra/wuid"
)

// Module wires a leased worker id to the process-wide guid generator and exposes
// the guid HTTP endpoints as a single application-lifecycle unit. It implements
// the app's Migrator, Starter, Stopper and RESTRegistrar capabilities: Migrate
// prepares the dlock/wuid tables, Start leases a worker id and installs the
// generator, and Stop releases the lease.
type Module struct {
	db    *bun.DB
	lease time.Duration

	worker *wuid.Worker
}

// NewModule builds the module against db. lease sets the worker-id lease duration
// (0 uses wuid's default). The worker id is always auto-allocated via the lease.
func NewModule(db *bun.DB, lease time.Duration) *Module {
	return &Module{db: db, lease: lease}
}

// Migrate applies the dlock and wuid schemas (each with its own goose version
// table, so they migrate independently). The generator itself needs no schema.
func (m *Module) Migrate(ctx context.Context) error {
	if err := dlock.Migrate(ctx, m.db); err != nil {
		return fmt.Errorf("dlock migrate: %w", err)
	}
	if err := wuid.Migrate(ctx, m.db); err != nil {
		return fmt.Errorf("wuid migrate: %w", err)
	}
	return nil
}

// Start leases a worker id and installs the process-wide generator seeded with
// it. Requires Migrate to have run first.
func (m *Module) Start(ctx context.Context) error {
	opts := []wuid.Option{
		wuid.OnLost(m.onWorkerLost),
		wuid.OnReacquire(m.onWorkerReacquire),
	}
	if m.lease > 0 {
		opts = append(opts, wuid.WithLease(m.lease))
	}

	w, err := wuid.Open(ctx, m.db, opts...)
	if err != nil {
		return fmt.Errorf("open worker id: %w", err)
	}
	m.worker = w

	if err := m.installGenerator(w.ID()); err != nil {
		return err
	}
	slog.Info("guid generator ready", "worker_id", w.ID())
	return nil
}

// installGenerator builds and installs the process-wide generator for id.
func (m *Module) installGenerator(id uint16) error {
	g, err := New(id)
	if err != nil {
		return fmt.Errorf("init guid generator: %w", err)
	}
	SetDefault(g)
	return nil
}

// onWorkerLost fires when the worker-id lease is lost. It clears the process-wide
// generator so GET /guid returns 503 (id generation suspended) rather than
// minting ids with an id another node may now hold. The process keeps running;
// the wuid worker retries re-allocation in the background.
func (m *Module) onWorkerLost(error) {
	SetDefault(nil)
	slog.Error("guid generation suspended: worker id lost, waiting for re-allocation")
}

// onWorkerReacquire fires when the wuid worker re-allocates a fresh id after a
// loss. It reinstalls the generator so id generation resumes.
func (m *Module) onWorkerReacquire(id uint16) {
	if err := m.installGenerator(id); err != nil {
		slog.Error("guid generation stays suspended: generator reinstall failed", "worker_id", id, "err", err)
		return
	}
	slog.Info("guid generation resumed", "worker_id", id)
}

// Stop releases the worker-id lease so the slot is freed for reuse rather than
// waiting out its TTL. Safe to call when Start never ran.
func (m *Module) Stop(ctx context.Context) error {
	if m.worker == nil {
		return nil
	}
	return m.worker.Close(ctx)
}

// RegisterREST mounts the guid HTTP endpoints, injecting the package-level id
// source so the transport layer stays decoupled from the generator.
func (m *Module) RegisterREST(api huma.API) {
	guidapi.RegisterREST(api, Next)
}

// RegisterGRPC registers the guid gRPC service, injecting the same id source.
func (m *Module) RegisterGRPC(server *grpc.Server) {
	guidapi.RegisterGRPC(server, Next)
}
