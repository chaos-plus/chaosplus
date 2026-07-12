package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/chaos-plus/chaosplus/pkg/geoip"
	_ "github.com/chaos-plus/chaosplus/pkg/geoip/providers" // register geoip providers
)

// buildModules is the composition root: the single place that constructs the
// application's modules and their dependencies, in registration order. Adding a
// feature means adding a module here — nothing else in the app changes.
func (app *App) buildModules() []any {
	mods := make([]any, 0, 2)

	// Identity generation needs a writable database. Skipped when none exists so
	// the app can still serve endpoints that don't need one.
	if len(app.dbr.Writer) > 0 {
		mods = append(mods, guid.NewModule(app.dbr.Write(), app.failStop))
	} else {
		slog.Warn("no writable database; skipping id generator")
	}

	mods = append(mods, geoipModule{})

	return mods
}

// failStop reports a fatal background failure (e.g. a lost worker-id lease) on
// serveErr so awaitShutdown brings the whole app down. Non-blocking: serveErr is
// buffered for every fatal source, and a full channel already means the app is
// going down.
func (app *App) failStop(err error) {
	slog.Error("fatal background failure; shutting down", "err", err)
	select {
	case app.serveErr <- fmt.Errorf("worker id lost: %w", err):
	default:
	}
}

// geoipModule adapts pkg/geoip's provider maintenance to the app lifecycle. The
// adapter lives here, at the boundary, so pkg/geoip stays free of app concepts.
// Start receives the app's root context, so the background refresh stops on
// shutdown and no separate Stop is needed.
type geoipModule struct{}

func (geoipModule) Start(ctx context.Context) error {
	geoip.StartProviders(ctx)
	return nil
}
