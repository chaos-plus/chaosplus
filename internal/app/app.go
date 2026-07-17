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

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/spicedbx"
	authnmod "github.com/chaos-plus/chaosplus/internal/modules/authn"
	"github.com/chaos-plus/chaosplus/pkg/utils"
	"github.com/redis/go-redis/v9"
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

	// redis is the shared client used by the rate limiter. The universal client
	// abstracts standalone, sentinel, and cluster deployments. nil when no Redis
	// is configured (Config.Redis.Addrs empty), in which case rate limiting is off.
	redis redis.UniversalClient

	// mods are the application modules, in registration order. Their lifecycle
	// phases (migrate/start/register/stop) are driven by the phase runners.
	mods []any

	authnVerifier  *authn.Verifier
	authnRequest   authz.TokenVerifier
	authnWeb       *authnmod.WebService
	authzRegistrar *authz.Registrar
	spicedb        *spicedbx.AuthzedClient

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
func (app *App) Run() error {
	// Root context for background workers; cancelled in shutdown.
	app.ctx, app.cancel = context.WithCancel(context.Background())

	if app.cfg.Debug {
		app.SetupDebug()
	}

	// bootstrap
	if err := app.Bootstrap(); err != nil {
		return errors.Join(fmt.Errorf("bootstrap: %w", err), app.shutdown())
	}

	// grpc server
	if err := app.StartGrpcServer(); err != nil {
		return errors.Join(fmt.Errorf("start grpc server: %w", err), app.shutdown())
	}

	// http server
	if err := app.StartRestServer(); err != nil {
		return errors.Join(fmt.Errorf("start rest server: %w", err), app.shutdown())
	}

	// graceful shutdown
	return app.awaitShutdown()
}

// awaitShutdown blocks until either a termination signal (SIGINT/SIGTERM) or a
// server goroutine reports a fatal error, then tears everything down. A serve
// failure is included in the returned error so the process exits non-zero.
func (app *App) awaitShutdown() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var startErr error
	select {
	case <-ctx.Done():
		slog.Info("shutdown signal received, gracefully stopping servers")
	case err := <-app.serveErr:
		// A server died after binding; take the whole app down.
		slog.Error("server failed, shutting down", "error", err)
		startErr = err
	}

	return errors.Join(startErr, app.shutdown())
}

// shutdown stops the REST server, the gRPC server and the database within
// shutdownTimeout, joining any errors. It is safe to call with only some
// components started (nil servers are skipped), so both the startup-failure and
// signal paths can share it.
func (app *App) shutdown() error {
	// Stop background workers first (geoip refresh, etc.) so they wind down while
	// the servers drain. Guarded because lifecycle tests build an App directly
	// without running Run.
	if app.cancel != nil {
		app.cancel()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	var errs []error

	// Stop the REST server first: refuse new connections, drain in-flight ones.
	if app.rest != nil {
		if err := app.rest.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("rest shutdown: %w", err))
		} else {
			slog.Info("rest server stopped")
		}
	}

	// Stop the gRPC server, falling back to a forced stop on timeout.
	if app.grpc != nil {
		stopped := make(chan struct{})
		go func() {
			app.grpc.GracefulStop()
			close(stopped)
		}()
		select {
		case <-stopped:
			slog.Info("grpc server stopped")
		case <-shutdownCtx.Done():
			app.grpc.Stop() // force-close remaining connections
			errs = append(errs, fmt.Errorf("grpc graceful stop timed out: %w", shutdownCtx.Err()))
		}
	}

	// Stop modules (reverse order) before closing the database they use, so e.g.
	// the worker-id lease is released rather than left to expire.
	if err := app.stopModules(shutdownCtx); err != nil {
		errs = append(errs, err)
	}

	// Close the database connection pools once nothing is serving.
	if err := app.dbr.Close(); err != nil {
		errs = append(errs, fmt.Errorf("db close: %w", err))
	}

	// Close the Redis client used by the rate limiter, if any.
	if app.redis != nil {
		if err := app.redis.Close(); err != nil {
			errs = append(errs, fmt.Errorf("redis close: %w", err))
		}
	}

	if app.spicedb != nil {
		if err := app.spicedb.Close(); err != nil {
			errs = append(errs, fmt.Errorf("spicedb close: %w", err))
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	slog.Info("shutdown complete")
	return nil
}
