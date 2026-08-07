package app

import (
	"errors"
	"expvar"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
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

var (
	httpRequests  = expvar.NewInt("http_requests_total")
	httpErrors4xx = expvar.NewInt("http_errors_4xx")
	httpErrors5xx = expvar.NewInt("http_errors_5xx")
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
	router.Use(requestMetrics) // count requests by status code
	clientIP, err := trustedProxyClientIP(app.cfg.RestServer.TrustedProxies)
	if err != nil {
		return err
	}
	router.Use(clientIP)
	router.Use(middleware.Recoverer)
	app.useSecurity(router)  // security response headers
	app.useCors(router)      // CORS (handles preflight before routing)
	router.Use(respx.Timing) // stamp request start time for response meta
	router.Use(respx.Locale) // resolve request locale for message i18n
	app.useRateLimit(router) // per-IP / per-account limiting (after RealIP + Locale)

	// API version header on every response so clients can negotiate compatibility.
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-API-Version", "1.0.0")
			next.ServeHTTP(w, r)
		})
	})

	// ponytail: basic observability via expvar (stdlib, zero deps).
	// Prometheus/OTLP exporters deferred until ops requirements firm up.
	router.Get("/debug/vars", expvar.Handler().ServeHTTP)

	// Health and readiness probes — plain chi routes, no huma envelope.
	router.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	router.Get("/readyz", app.readyHandler())

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
	registry := authz.DefaultRegistry()
	if app.authzRegistrar != nil {
		registry = app.authzRegistrar.Registry()
	}
	if err := authz.ValidateOperations(api, registry); err != nil {
		return err
	}

	addr := fmt.Sprintf("%s:%d", app.cfg.RestServer.Host, app.cfg.RestServer.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("rest listen %s: %w", addr, err)
	}
	server := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: readHeaderTimeout,
	}
	app.rest = server

	go func() {
		slog.Info("rest server listening", "addr", addr)
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			app.serveErr <- fmt.Errorf("rest serve: %w", err)
		}
	}()

	return nil
}

func trustedProxyClientIP(values []string) (func(http.Handler) http.Handler, error) {
	prefixes := make([]netip.Prefix, len(values))
	for i, value := range values {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("rest trusted proxy %q: %w", value, err)
		}
		prefixes[i] = prefix
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			peer, ok := remoteIP(request.RemoteAddr)
			if ok && trustedIP(peer, prefixes) {
				client := peer
				for i := len(request.Header.Values("X-Forwarded-For")) - 1; i >= 0; i-- {
					parts := strings.Split(request.Header.Values("X-Forwarded-For")[i], ",")
					for j := len(parts) - 1; j >= 0; j-- {
						candidate, err := netip.ParseAddr(strings.TrimSpace(parts[j]))
						if err != nil {
							next.ServeHTTP(writer, request)
							return
						}
						client = candidate.Unmap().WithZone("")
						if !trustedIP(client, prefixes) {
							request.RemoteAddr = client.String()
							next.ServeHTTP(writer, request)
							return
						}
					}
				}
				request.RemoteAddr = client.String()
			}
			next.ServeHTTP(writer, request)
		})
	}, nil
}

func remoteIP(value string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(value)
	if err != nil {
		host = value
	}
	ip, err := netip.ParseAddr(host)
	return ip.Unmap(), err == nil
}

func trustedIP(ip netip.Addr, prefixes []netip.Prefix) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
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

// readyHandler returns an HTTP handler that pings the primary database.
// Kubernetes uses /readyz as a startup/liveness probe. Returns 503 when
// the database is unreachable.
func (app *App) readyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if app.dbr.Write() == nil {
			http.Error(w, "database not configured", http.StatusServiceUnavailable)
			return
		}
		if err := app.dbr.Write().PingContext(r.Context()); err != nil {
			http.Error(w, "database unreachable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	}
}

// requestMetrics is a chi middleware that increments expvar counters for each
// HTTP response by status-code bucket.
func requestMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		httpRequests.Add(1)
		if sw.status >= 500 {
			httpErrors5xx.Add(1)
		} else if sw.status >= 400 {
			httpErrors4xx.Add(1)
		}
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
