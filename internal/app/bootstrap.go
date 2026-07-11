package app

import (
	"log/slog"
	"os"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/chaos-plus/chaosplus/pkg/geoip"
	_ "github.com/chaos-plus/chaosplus/pkg/geoip/providers" // register geoip providers
	"github.com/chaos-plus/chaosplus/pkg/timezone"
	"go.opentelemetry.io/contrib/bridges/otelslog"
)

func (a *App) Bootstrap() {
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

	// init db
	a.dbr = bunx.NewDatasourceRouter(a.name, cfg.Database)

	// init geoip providers — background db download/refresh, tied to the app
	// context so it stops on shutdown. Providers self-register via their package
	// init (blank import above); those without a database do no background work.
	geoip.StartProviders(a.ctx)

	// init cache

	// init redis

	// init mq

	// init ...

}
