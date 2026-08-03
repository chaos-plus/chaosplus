package app

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/plugin"
	"github.com/chaos-plus/chaosplus/internal/core/extension/secretx"
	auditmod "github.com/chaos-plus/chaosplus/internal/modules/audit"
	authnmod "github.com/chaos-plus/chaosplus/internal/modules/authn"
	"github.com/chaos-plus/chaosplus/internal/modules/governance"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	identitymod "github.com/chaos-plus/chaosplus/internal/modules/identity"
	oauthmod "github.com/chaos-plus/chaosplus/internal/modules/oauth"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/chaos-plus/chaosplus/internal/modules/provisioning"
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
	if err := initModuleI18n(); err != nil {
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
		password, err := secretx.Resolve("redis.password", app.cfg.Redis.Password, app.cfg.Redis.PasswordFile, 4096)
		if err != nil {
			return err
		}
		app.cfg.Redis.Password = password
		app.redis = redis.NewUniversalClient(&redis.UniversalOptions{
			Addrs:      app.cfg.Redis.Addrs,
			MasterName: app.cfg.Redis.MasterName,
			Username:   app.cfg.Redis.Username,
			Password:   app.cfg.Redis.Password,
			DB:         app.cfg.Redis.DB,
		})
	}

	registry := authz.DefaultRegistry()
	if app.cfg.Authn.Enabled {
		if len(app.dbr.Writer) == 0 {
			return fmt.Errorf("local authentication requires a writable database")
		}
		claimPlugins, err := plugin.LoadClaims(app.cfg.Plugins)
		if err != nil {
			return fmt.Errorf("load claim plugins: %w", err)
		}
		app.claimPlugins = claimPlugins
		options := []authnmod.WebOption{authnmod.WithRegistrationPrincipalCreator(registrationPrincipalCreator)}
		if claimPlugins != nil {
			options = append(options, authnmod.WithClaimEnricher(claimPlugins))
		}
		web, err := authnmod.NewWebService(app.cfg.Authn, app.dbr.Write(), options...)
		if err != nil {
			return fmt.Errorf("init local authn: %w", err)
		}
		app.authnWeb = web
		app.authnRequest = web
	}

	if app.cfg.Authz.Enabled {
		if !app.cfg.Authn.Enabled {
			return fmt.Errorf("authorization requires authentication to be enabled")
		}
		if len(app.dbr.Writer) == 0 {
			return fmt.Errorf("authorization requires a writable database")
		}
		app.authzRegistrar = authz.NewRegistrar(registry, app.authnRequest, iam.NewAuthorizer(app.dbr.Write()), iam.NewMembershipChecker(app.dbr.Write()))
	}

	// build modules, then run the migrate and start phases in order.
	app.mods = app.buildModules()
	if app.cfg.Migrations.Auto {
		if err := app.migrateModules(app.ctx); err != nil {
			return err
		}
	} else if app.authzRegistrar != nil {
		if err := iam.AssertMigrated(app.ctx, app.dbr.Write()); err != nil {
			return err
		}
		if err := organization.AssertMigrated(app.ctx, app.dbr.Write()); err != nil {
			return err
		}
		if err := provisioning.AssertMigrated(app.ctx, app.dbr.Write()); err != nil {
			return err
		}
		if err := governance.AssertMigrated(app.ctx, app.dbr.Write()); err != nil {
			return err
		}
	}
	if err := app.startModules(app.ctx); err != nil {
		return err
	}

	return nil
}

func initModuleI18n() error {
	if err := i18n.InitEmbedded(i18n.Base); err != nil {
		return err
	}
	modules := []struct {
		name     string
		register func() error
	}{
		{"audit", auditmod.RegisterI18n},
		{"authn", authnmod.RegisterI18n},
		{"governance", governance.RegisterI18n},
		{"iam", iam.RegisterI18n},
		{"identity", identitymod.RegisterI18n},
		{"oauth", oauthmod.RegisterI18n},
		{"organization", organization.RegisterI18n},
		{"provisioning", provisioning.RegisterI18n},
	}
	for _, module := range modules {
		if err := module.register(); err != nil {
			return fmt.Errorf("register %s locales: %w", module.name, err)
		}
	}
	return nil
}
