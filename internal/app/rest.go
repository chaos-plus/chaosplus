package app

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/docs"
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

	config := huma.DefaultConfig(app.name+" API", "1.0.0")
	// Disable huma's built-in single-renderer /docs so our own tabbed page
	// (registered below) is not overwritten when humachi.New registers routes.
	config = docs.Register(router, config, app.name)

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
