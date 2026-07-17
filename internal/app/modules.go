package app

import (
	"log/slog"
	"strconv"
	"time"

	"github.com/chaos-plus/chaosplus/internal/infra/geoip"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	authnmod "github.com/chaos-plus/chaosplus/internal/modules/authn"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
)

// buildModules is the composition root: the single place that constructs the
// application's modules and their dependencies, in registration order. Adding a
// feature means adding a module here — nothing else in the app changes.
func (app *App) buildModules() []any {
	mods := make([]any, 0, 3)

	// Identity generation needs a writable database. Skipped when none exists so
	// the app can still serve endpoints that don't need one.
	if len(app.dbr.Writer) > 0 {
		mods = append(mods, guid.NewModule(app.dbr.Write(), time.Duration(app.cfg.WorkerLease)*time.Second))
	} else {
		slog.Warn("no writable database; skipping id generator")
	}

	if app.cfg.Authn.Enabled {
		authenticator := app.authnRequest
		if authenticator == nil {
			authenticator = app.authnVerifier
		}
		mods = append(mods, authnmod.NewModule(authenticator, app.authnWeb))
	}
	if app.authzRegistrar != nil {
		if app.authzRegistrar.IsDeclarationOnly() {
			mods = append(mods, iam.NewDeclarationOnlyModule(app.authzRegistrar))
		} else {
			mods = append(mods, iam.NewModule(app.dbr.Write(), app.authzRegistrar, app.spicedb, app.spicedb, func() (string, error) {
				id, err := guid.Next()
				return strconv.FormatInt(id, 10), err
			}, app.cfg.Authz.Outbox))
		}
	} else {
		slog.Warn("authorization stack disabled; skipping iam management API")
	}
	mods = append(mods, geoip.NewModule(app.cfg.GeoIP))

	return mods
}
