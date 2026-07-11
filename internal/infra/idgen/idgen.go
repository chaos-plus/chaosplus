// Package idgen is the identity-generation application module. It composes the
// dlock, wuid and guid infra packages into a single lifecycle unit: it migrates
// their tables, leases a worker id (coordinated through the distributed lock),
// installs the process-wide guid generator seeded with that id, and exposes the
// guid HTTP endpoints.
package idgen

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/danielgtaylor/huma/v2"
	"github.com/uptrace/bun"

	"github.com/chaos-plus/chaosplus/internal/infra/dlock"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	guidapi "github.com/chaos-plus/chaosplus/internal/infra/guid/api"
	"github.com/chaos-plus/chaosplus/internal/infra/wuid"
)

// Module wires worker-id leasing and the guid generator to an application
// lifecycle. It implements the app's Migrator, Starter, Stopper and
// RESTRegistrar capabilities.
type Module struct {
	db     *bun.DB
	onLost func(error)

	worker *wuid.Worker
}

// New builds the module against db. onLost is invoked if the worker-id lease is
// later lost (treat as fatal: ids could collide across nodes); it may be nil.
func New(db *bun.DB, onLost func(error)) *Module {
	return &Module{db: db, onLost: onLost}
}

// Migrate applies the dlock and wuid schemas (each with its own goose version
// table, so they migrate independently).
func (m *Module) Migrate(ctx context.Context) error {
	if err := dlock.Migrate(ctx, m.db); err != nil {
		return fmt.Errorf("dlock migrate: %w", err)
	}
	if err := wuid.Migrate(ctx, m.db); err != nil {
		return fmt.Errorf("wuid migrate: %w", err)
	}
	return nil
}

// Start leases a worker id and installs the process-wide guid generator seeded
// with it. Requires Migrate to have run first.
func (m *Module) Start(ctx context.Context) error {
	w, err := wuid.Resolve(ctx, m.db, wuid.OnLost(m.onLost))
	if err != nil {
		return fmt.Errorf("resolve worker id: %w", err)
	}
	m.worker = w

	g, err := guid.New(w.ID())
	if err != nil {
		return fmt.Errorf("init guid generator: %w", err)
	}
	guid.SetDefault(g)

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

// RegisterREST mounts the guid HTTP endpoints.
func (m *Module) RegisterREST(api huma.API) {
	guidapi.RegisterREST(api)
}
