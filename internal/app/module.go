package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/danielgtaylor/huma/v2"
	"google.golang.org/grpc"
)

// A module is a self-contained unit of application wiring. It declares the
// lifecycle phases it participates in by implementing the small capability
// interfaces below (interface segregation): the app runs each phase by iterating
// the registered modules and calling only those that implement that phase. This
// keeps Bootstrap a fixed-size phase runner instead of growing an init call per
// feature — a new feature is a new module plus one line in buildModules.
type (
	// Migrator applies a module's schema migrations.
	Migrator interface {
		Migrate(ctx context.Context) error
	}
	// Starter starts a module's background/runtime work.
	Starter interface {
		Start(ctx context.Context) error
	}
	// Stopper releases a module's resources on shutdown.
	Stopper interface {
		Stop(ctx context.Context) error
	}
	// RESTRegistrar mounts a module's HTTP endpoints on the huma API.
	RESTRegistrar interface{ RegisterREST(api huma.API) }
	// GRPCRegistrar registers a module's gRPC services.
	GRPCRegistrar interface{ RegisterGRPC(server *grpc.Server) }
)

// migrateModules runs the Migrate phase, stopping at the first failure so the
// app never starts against a half-migrated schema.
func (a *App) migrateModules(ctx context.Context) error {
	for _, m := range a.mods {
		if x, ok := m.(Migrator); ok {
			if err := x.Migrate(ctx); err != nil {
				return fmt.Errorf("%T migrate: %w", m, err)
			}
		}
	}
	return nil
}

// startModules runs the Start phase, stopping at the first failure.
func (a *App) startModules(ctx context.Context) error {
	for _, m := range a.mods {
		if x, ok := m.(Starter); ok {
			if err := x.Start(ctx); err != nil {
				return fmt.Errorf("%T start: %w", m, err)
			}
		}
	}
	return nil
}

// registerREST runs the REST-registration phase across all modules.
func (a *App) registerREST(api huma.API) {
	for _, m := range a.mods {
		if x, ok := m.(RESTRegistrar); ok {
			x.RegisterREST(api)
		}
	}
}

// registerGRPC runs the gRPC-registration phase across all modules.
func (a *App) registerGRPC(server *grpc.Server) {
	for _, m := range a.mods {
		if x, ok := m.(GRPCRegistrar); ok {
			x.RegisterGRPC(server)
		}
	}
}

// stopModules runs the Stop phase in reverse registration order (so dependents
// stop before their dependencies), joining every error rather than bailing on
// the first so one failing module can't strand the rest.
func (a *App) stopModules(ctx context.Context) error {
	var errs []error
	for i := len(a.mods) - 1; i >= 0; i-- {
		m := a.mods[i]
		if x, ok := m.(Stopper); ok {
			if err := x.Stop(ctx); err != nil {
				errs = append(errs, fmt.Errorf("%T stop: %w", m, err))
			}
		}
	}
	return errors.Join(errs...)
}
