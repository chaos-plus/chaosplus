package app

import (
	"log/slog"
	"os"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/chaos-plus/chaosplus/pkg/timezone"
	"go.opentelemetry.io/contrib/bridges/otelslog"
)

// Bootstrap wires up cross-cutting dependencies before the servers start: base
// services (timezone, logging, database), then the application modules through
// their migrate and start phases. It returns an error so a failed migration or
// module start aborts startup instead of leaving the app half-initialised.
func (a *App) Bootstrap() error {
	cfg := a.cfg

	// init timezone
	timezone.SetTimezone(cfg.Timezone)

	// init logger — always to stdout, optionally also to a file when configured.
	var handlers []slog.Handler
	handlers = append(handlers, slog.NewJSONHandler(os.Stdout, nil))
	handlers = append(handlers, otelslog.NewHandler(a.name))
	slog.SetDefault(slog.New(slog.NewMultiHandler(handlers...)))

	// init db — in debug mode with no datasource configured, fall back to an
	// in-memory sqlite so the full stack runs with no external database. A single
	// connection keeps the private ":memory:" database alive and consistent for
	// the process lifetime.
	if len(cfg.Database) == 0 && cfg.Debug {
		slog.Warn("debug mode: no database configured, falling back to in-memory sqlite")
		cfg.Database = map[string]bunx.Datasource{
			"default": {Type: "sqlite", Dsn: ":memory:", Writable: true, Readable: true, MaxOpenConns: 1, MaxIdleConns: 1},
		}
	}
	a.dbr = bunx.NewDatasourceRouter(a.name, cfg.Debug, cfg.Database)

	// build modules, then run the migrate and start phases in order.
	a.mods = a.buildModules()
	if err := a.migrateModules(a.ctx); err != nil {
		return err
	}
	if err := a.startModules(a.ctx); err != nil {
		return err
	}

	return nil
}
