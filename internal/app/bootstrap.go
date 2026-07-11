package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/chaos-plus/chaosplus/internal/infra/dlock"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/chaos-plus/chaosplus/internal/infra/wuid"
	"github.com/chaos-plus/chaosplus/pkg/geoip"
	_ "github.com/chaos-plus/chaosplus/pkg/geoip/providers" // register geoip providers
	"github.com/chaos-plus/chaosplus/pkg/timezone"
	"go.opentelemetry.io/contrib/bridges/otelslog"
)

// Bootstrap wires up cross-cutting dependencies before the servers start. It
// returns an error so a failed migration or worker-id lease aborts startup
// instead of leaving the app half-initialised.
func (a *App) Bootstrap() error {
	cfg := a.cfg

	// init profiler

	// init timezone
	timezone.SetTimezone(cfg.Timezone)

	// init tracer

	// init logger — always to stdout, optionally also to a file when configured.
	var handlers []slog.Handler
	handlers = append(handlers, slog.NewJSONHandler(os.Stdout, nil))
	handlers = append(handlers, otelslog.NewHandler(a.name))
	slog.SetDefault(slog.New(slog.NewMultiHandler(handlers...)))

	// init db — in debug mode with no datasource configured, fall back to an
	// in-memory sqlite so the full stack (migrations, worker id, guid) runs with
	// no external database. A single connection keeps the private ":memory:"
	// database alive and consistent for the process lifetime.
	if len(cfg.Database) == 0 && cfg.Debug {
		slog.Warn("debug mode: no database configured, falling back to in-memory sqlite")
		cfg.Database = map[string]bunx.Datasource{
			"default": {Type: "sqlite", Dsn: ":memory:", Writable: true, Readable: true, MaxOpenConns: 1, MaxIdleConns: 1},
		}
	}
	a.dbr = bunx.NewDatasourceRouter(a.name, cfg.Database)

	// init db-backed infra: schema migrations, worker id and the guid generator.
	// Skipped when no writable datasource exists so the app can still serve
	// endpoints that don't need a database.
	if len(a.dbr.Writer) > 0 {
		if err := a.initDatabase(a.ctx); err != nil {
			return err
		}
	} else {
		slog.Warn("no writable database; skipping migrations, worker-id and guid setup")
	}

	// init geoip providers — background db download/refresh, tied to the app
	// context so it stops on shutdown. Providers self-register via their package
	// init (blank import above); those without a database do no background work.
	geoip.StartProviders(a.ctx)

	// init cache

	// init redis

	// init mq

	// init ...

	return nil
}

// initDatabase runs the infra schema migrations, leases a worker id (coordinated
// through the dlock distributed lock) and installs the process-wide guid
// generator seeded with that id. Losing the lease later is fatal — ids could
// collide across nodes — so it reports on serveErr to bring the app down.
func (a *App) initDatabase(ctx context.Context) error {
	db := a.dbr.Write()

	if err := dlock.Migrate(ctx, db); err != nil {
		return fmt.Errorf("dlock migrate: %w", err)
	}
	if err := wuid.Migrate(ctx, db); err != nil {
		return fmt.Errorf("wuid migrate: %w", err)
	}

	w, err := wuid.Resolve(ctx, db, wuid.OnLost(func(err error) {
		slog.Error("worker id lease lost; shutting down to avoid duplicate ids", "err", err)
		select {
		case a.serveErr <- fmt.Errorf("worker id lost: %w", err):
		default:
		}
	}))
	if err != nil {
		return fmt.Errorf("resolve worker id: %w", err)
	}
	a.worker = w

	g, err := guid.New(w.ID())
	if err != nil {
		return fmt.Errorf("init guid generator: %w", err)
	}
	guid.SetDefault(g)

	slog.Info("guid generator ready", "worker_id", w.ID())
	return nil
}
