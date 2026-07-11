// Package app wires the Chaosplus API together: it loads configuration, builds
// dependencies, mounts the huma API on a chi router and runs an HTTP server
// alongside a gRPC server, both with graceful shutdown. It owns its own
// lifecycle, logging and configuration (no external web framework).
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/chaos-plus/chaosplus/internal/infra/wuid"
	"github.com/chaos-plus/chaosplus/pkg/utils"
	"google.golang.org/grpc"

	_ "github.com/danielgtaylor/huma/v2/formats/cbor"
)

// shutdownTimeout bounds how long graceful shutdown waits for in-flight work to
// drain before connections are forced closed.
const shutdownTimeout = 15 * time.Second

type App struct {
	name string
	cfg  Config

	dbr bunx.DatasourceRouter

	// worker holds the leased worker id backing the guid generator. Closed on
	// shutdown to release the lease. Nil when no database is available.
	worker *wuid.Worker

	rest *http.Server
	grpc *grpc.Server

	// ctx is the application's root context; cancel tears down background workers
	// (e.g. geoip database refresh) during shutdown. Set in Run.
	ctx    context.Context
	cancel context.CancelFunc

	// serveErr receives a fatal error that must bring the whole app down: a
	// server's Serve/ListenAndServe failing after bind, or the worker-id lease
	// being lost. Buffered by the number of such sources (2 servers + worker) so
	// no reporter blocks if several fail at once.
	serveErr chan error
}

func NewApp(cfg Config) *App {
	if cfg.Name == "" {
		cfg.Name = utils.GetExecutableName()
	}
	cfg.Name = strings.ToUpper(cfg.Name)
	return &App{
		name:     cfg.Name,
		cfg:      cfg,
		serveErr: make(chan error, 3),
	}
}

// Run bootstraps dependencies, starts the gRPC and REST servers in the
// background, then blocks in awaitShutdown until a termination signal arrives or
// a server fails. If a server fails to start, whatever was already started is
// torn down before returning, so Run never leaks a running server.
func (a *App) Run() error {
	// Root context for background workers; cancelled in shutdown.
	a.ctx, a.cancel = context.WithCancel(context.Background())

	// bootstrap
	if err := a.Bootstrap(); err != nil {
		return errors.Join(fmt.Errorf("bootstrap: %w", err), a.shutdown())
	}

	// grpc server
	if err := a.StartGrpcServer(); err != nil {
		return errors.Join(fmt.Errorf("start grpc server: %w", err), a.shutdown())
	}

	// http server
	if err := a.StartRestServer(); err != nil {
		return errors.Join(fmt.Errorf("start rest server: %w", err), a.shutdown())
	}

	// graceful shutdown
	return a.awaitShutdown()
}

// awaitShutdown blocks until either a termination signal (SIGINT/SIGTERM) or a
// server goroutine reports a fatal error, then tears everything down. A serve
// failure is included in the returned error so the process exits non-zero.
func (a *App) awaitShutdown() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var startErr error
	select {
	case <-ctx.Done():
		slog.Info("shutdown signal received, gracefully stopping servers")
	case err := <-a.serveErr:
		// A server died after binding; take the whole app down.
		slog.Error("server failed, shutting down", "error", err)
		startErr = err
	}

	return errors.Join(startErr, a.shutdown())
}

// shutdown stops the REST server, the gRPC server and the database within
// shutdownTimeout, joining any errors. It is safe to call with only some
// components started (nil servers are skipped), so both the startup-failure and
// signal paths can share it.
func (a *App) shutdown() error {
	// Stop background workers first (geoip refresh, etc.) so they wind down while
	// the servers drain. Guarded because lifecycle tests build an App directly
	// without running Run.
	if a.cancel != nil {
		a.cancel()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	var errs []error

	// Stop the REST server first: refuse new connections, drain in-flight ones.
	if a.rest != nil {
		if err := a.rest.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("rest shutdown: %w", err))
		} else {
			slog.Info("rest server stopped")
		}
	}

	// Stop the gRPC server, falling back to a forced stop on timeout.
	if a.grpc != nil {
		stopped := make(chan struct{})
		go func() {
			a.grpc.GracefulStop()
			close(stopped)
		}()
		select {
		case <-stopped:
			slog.Info("grpc server stopped")
		case <-shutdownCtx.Done():
			a.grpc.Stop() // force-close remaining connections
			errs = append(errs, fmt.Errorf("grpc graceful stop timed out: %w", shutdownCtx.Err()))
		}
	}

	// Release the worker-id lease before closing the database it lives in, so the
	// slot is freed for reuse rather than waiting out its TTL.
	if a.worker != nil {
		if err := a.worker.Close(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("worker close: %w", err))
		}
	}

	// Close the database connection pools once nothing is serving.
	if err := a.dbr.Close(); err != nil {
		errs = append(errs, fmt.Errorf("db close: %w", err))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	slog.Info("shutdown complete")
	return nil
}
