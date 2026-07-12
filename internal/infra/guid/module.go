package guid

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/danielgtaylor/huma/v2"
	"github.com/uptrace/bun"

	"github.com/chaos-plus/chaosplus/internal/infra/dlock"
	"github.com/chaos-plus/chaosplus/internal/infra/wuid"
)

// Module wires a leased worker id to the process-wide guid generator and exposes
// the guid HTTP endpoints as a single application-lifecycle unit. It implements
// the app's Migrator, Starter, Stopper and RESTRegistrar capabilities: Migrate
// prepares the dlock/wuid tables, Start leases a worker id and installs the
// generator, and Stop releases the lease.
type Module struct {
	db     *bun.DB
	onLost func(error)

	worker *wuid.Worker
}

// NewModule builds the module against db. onLost is invoked if the worker-id
// lease is later lost (treat as fatal: ids could collide across nodes); it may
// be nil.
func NewModule(db *bun.DB, onLost func(error)) *Module {
	return &Module{db: db, onLost: onLost}
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
	w, err := wuid.Open(ctx, m.db, wuid.OnLost(m.onLost))
	if err != nil {
		return fmt.Errorf("open worker id: %w", err)
	}
	m.worker = w

	g, err := New(w.ID())
	if err != nil {
		return fmt.Errorf("init guid generator: %w", err)
	}
	SetDefault(g)

	slog.Info("guid generator ready", "worker_id", w.ID())
	return nil
}

// Stop releases the worker-id lease so the slot is freed for reuse rather than
// waiting out its TTL. Safe to call when Start never ran.
func (m *Module) Stop(ctx context.Context) error {
	if m.worker == nil {
		return nil
	}
	return m.worker.Close(ctx)
}

// RegisterREST mounts the guid HTTP endpoints on the huma API.
func (m *Module) RegisterREST(api huma.API) {
	registerREST(api)
}
