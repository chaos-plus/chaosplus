package app

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/docs"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
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
	router.Use(respx.Timing) // stamp request start time for response meta
	router.Use(respx.Locale) // resolve request locale for message i18n

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
