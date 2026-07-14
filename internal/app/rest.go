package app

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/docs"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/ratex"
	"github.com/chaos-plus/chaosplus/internal/core/extension/secure"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

// readHeaderTimeout bounds how long the server waits for request headers,
// guarding against Slowloris-style connections.
const readHeaderTimeout = 10 * time.Second

// StartRestServer mounts the huma API on a chi router (plus the docs UI) and
// starts an HTTP server in a background goroutine. The server is stored on the
// App so awaitShutdown can drain it gracefully. A bind failure is returned; a
// serve failure after bind is reported on a.serveErr so awaitShutdown can bring
// the whole app down.
func (app *App) StartRestServer() error {
	router := chi.NewMux()
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)
	app.useSecurity(router)  // security response headers
	app.useCors(router)      // CORS (handles preflight before routing)
	router.Use(respx.Timing) // stamp request start time for response meta
	router.Use(respx.Locale) // resolve request locale for message i18n
	app.useRateLimit(router) // per-IP / per-account limiting (after RealIP + Locale)

	config := huma.DefaultConfig(app.name+" API", "1.0.0")
	// Disable huma's built-in single-renderer /docs so our own tabbed page
	// (registered below) is not overwritten when humachi.New registers routes.
	config = docs.Register(router, config, app.name)

	// Localize every envelope Message into the request locale at serialize time.
	config.Transformers = append(config.Transformers, respx.LocalizeMessage)

	// Unify every error response into the {code,message,meta,data} envelope.
	respx.Install()

	api := humachi.New(router, config)
	app.registerREST(api)

	addr := fmt.Sprintf("%s:%d", app.cfg.RestServer.Host, app.cfg.RestServer.Port)
	server := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: readHeaderTimeout,
	}
	app.rest = server

	go func() {
		slog.Info("rest server listening", "addr", addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			app.serveErr <- fmt.Errorf("rest serve: %w", err)
		}
	}()

	return nil
}

// useSecurity mounts the security-headers middleware when enabled.
func (app *App) useSecurity(router chi.Router) {
	if app.cfg.Security.Enabled {
		router.Use(secure.Headers(app.cfg.Security.HSTS))
	}
}

// useCors mounts CORS when enabled, filling sensible defaults for any unset list
// (all origins/methods/headers) so a bare `cors.enabled: true` is usable.
func (app *App) useCors(router chi.Router) {
	c := app.cfg.Cors
	if !c.Enabled {
		return
	}
	opts := cors.Options{
		AllowedOrigins:   c.AllowedOrigins,
		AllowedMethods:   c.AllowedMethods,
		AllowedHeaders:   c.AllowedHeaders,
		ExposedHeaders:   c.ExposedHeaders,
		AllowCredentials: c.AllowCredentials,
		MaxAge:           c.MaxAge,
	}
	if len(opts.AllowedOrigins) == 0 {
		opts.AllowedOrigins = []string{"*"}
	}
	if len(opts.AllowedMethods) == 0 {
		opts.AllowedMethods = []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions}
	}
	if len(opts.AllowedHeaders) == 0 {
		opts.AllowedHeaders = []string{"*"}
	}
	router.Use(cors.Handler(opts))
}

// useRateLimit mounts the Redis-backed rate limiter when enabled and a Redis
// client is configured. Each dimension (per-IP, per-account) is added only when
// enabled with a positive rate; with no dimensions the middleware is not mounted.
func (app *App) useRateLimit(router chi.Router) {
	rl := app.cfg.RateLimit
	if !rl.Enabled {
		return
	}
	if app.redis == nil {
		slog.Warn("rate limiting enabled but no redis configured; skipping")
		return
	}

	var dims []ratex.Dimension
	if rl.IP.Enabled && rl.IP.Rate > 0 {
		dims = append(dims, ratex.Dimension{
			Name:  "ip",
			Key:   ratex.IPKey,
			Limit: ratex.Limit(rl.IP.Rate, rl.IP.Period, rl.IP.Burst),
		})
	}
	if rl.Account.Enabled && rl.Account.Rate > 0 {
		dims = append(dims, ratex.Dimension{
			Name:  "account",
			Key:   ratex.HeaderKey(rl.Account.Header),
			Limit: ratex.Limit(rl.Account.Rate, rl.Account.Period, rl.Account.Burst),
		})
	}
	if len(dims) == 0 {
		return
	}
	router.Use(ratex.New(app.redis, rl.Prefix, dims...).Handler)
	slog.Info("rate limiting enabled", "dimensions", len(dims))
}
