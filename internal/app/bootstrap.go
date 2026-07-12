package app

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/chaos-plus/chaosplus/pkg/i18n"
	"github.com/chaos-plus/chaosplus/pkg/timezone"
	"go.opentelemetry.io/contrib/bridges/otelslog"
)

// Bootstrap wires up cross-cutting dependencies before the servers start: base
// services (timezone, logging, database), then the application modules through
// their migrate and start phases. It returns an error so a failed migration or
// module start aborts startup instead of leaving the app half-initialised.
func (app *App) Bootstrap() error {

	// init timezone — timestamps are UTC end to end (DB, API), and only the
	// frontend converts to a display timezone. Fail fast on an invalid config.
	if err := timezone.SetTimezone(app.cfg.Timezone); err != nil {
		return fmt.Errorf("set timezone %q: %w", app.cfg.Timezone, err)
	}

	// init i18n — load the global locale bundle so response messages can be
	// localized from their i18n keys (see respx.LocalizeMessage).
	if err := i18n.InitEmbedded(i18n.Base); err != nil {
		return fmt.Errorf("init i18n: %w", err)
	}

	// init logger — always to stdout, optionally also to a file when configured.
	var handlers []slog.Handler
	handlers = append(handlers, slog.NewJSONHandler(os.Stdout, nil))
	handlers = append(handlers, otelslog.NewHandler(app.name))
	slog.SetDefault(slog.New(slog.NewMultiHandler(handlers...)))

	// init db — a single sqlite connection keeps the private ":memory:" database
	// alive and consistent for the process lifetime (see SetupDebug).
	app.dbr = bunx.NewDatasourceRouter(app.name, app.cfg.Debug, app.cfg.Database)

	// build modules, then run the migrate and start phases in order.
	app.mods = app.buildModules()
	if err := app.migrateModules(app.ctx); err != nil {
		return err
	}
	if err := app.startModules(app.ctx); err != nil {
		return err
	}

	return nil
}
