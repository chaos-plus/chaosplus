package app

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/spicedbx"
	"github.com/chaos-plus/chaosplus/pkg/i18n"
	"github.com/chaos-plus/chaosplus/pkg/timezone"
	"github.com/redis/go-redis/v9"
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

	// init redis — created lazily (no startup ping) so the rate limiter can fail
	// open if Redis is briefly unavailable. The universal client selects
	// standalone/sentinel/cluster from the options. Absent when no address is set.
	if len(app.cfg.Redis.Addrs) > 0 {
		app.redis = redis.NewUniversalClient(&redis.UniversalOptions{
			Addrs:      app.cfg.Redis.Addrs,
			MasterName: app.cfg.Redis.MasterName,
			Username:   app.cfg.Redis.Username,
			Password:   app.cfg.Redis.Password,
			DB:         app.cfg.Redis.DB,
		})
	}

	registry := authz.DefaultRegistry()
	verifier, err := authn.NewVerifier(app.cfg.Authn)
	if err != nil {
		return fmt.Errorf("init authn: %w", err)
	}
	app.authnVerifier = verifier

	if app.cfg.Authz.SpiceDB.Enabled {
		if !app.cfg.Authn.Enabled {
			return fmt.Errorf("spicedb authz requires authn to be enabled")
		}
		client, err := spicedbx.Open(app.cfg.Authz.SpiceDB)
		if err != nil {
			return fmt.Errorf("connect spicedb: %w", err)
		}
		app.spicedb = client
		if app.cfg.Authz.SpiceDB.ApplySchema {
			if _, err := client.WriteSchema(app.ctx, authz.GenerateSchema(registry.All())); err != nil {
				return fmt.Errorf("apply spicedb schema: %w", err)
			}
		}
	}
	if app.cfg.Authn.Enabled && app.spicedb != nil {
		app.authzRegistrar = authz.NewRegistrar(registry, verifier, app.spicedb)
	}

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
