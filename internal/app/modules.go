package app

import (
	"log/slog"
	"time"

	"github.com/chaos-plus/chaosplus/internal/infra/geoip"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

// buildModules is the composition root: the single place that constructs the
// application's modules and their dependencies, in registration order. Adding a
// feature means adding a module here — nothing else in the app changes.
func (app *App) buildModules() []any {
	mods := make([]any, 0, 2)

	// Identity generation needs a writable database. Skipped when none exists so
	// the app can still serve endpoints that don't need one.
	if len(app.dbr.Writer) > 0 {
		mods = append(mods, guid.NewModule(app.dbr.Write(), time.Duration(app.cfg.WorkerLease)*time.Second))
	} else {
		slog.Warn("no writable database; skipping id generator")
	}

	mods = append(mods, geoip.NewModule(app.cfg.GeoIP))

	return mods
}
