package app

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/docs"
	guidapi "github.com/chaos-plus/chaosplus/internal/infra/guid/api"
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
func (a *App) StartRestServer() error {
	router := chi.NewMux()
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)

	config := huma.DefaultConfig(a.name+" API", "1.0.0")
	config, multi := docs.Apply("all", nil, config)

	// router.Route("/api", func(r chi.Router) {
	api := humachi.New(router, config)
	RegisteRouter(api)
	// })

	if multi {
		docs.Register(router, config, a.name)
	}

	addr := fmt.Sprintf("%s:%d", a.cfg.RestServer.Host, a.cfg.RestServer.Port)
	server := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: readHeaderTimeout,
	}
	a.rest = server

	go func() {
		slog.Info("rest server listening", "addr", addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.serveErr <- fmt.Errorf("rest serve: %w", err)
		}
	}()

	return nil
}

func RegisteRouter(api huma.API) {
	guidapi.RegisterREST(api)
}
