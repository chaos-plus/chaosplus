// Package geoip is the geolocation application module. It wraps the pkg/geoip
// provider registry as a lifecycle unit — configuring the providers and starting
// their background maintenance — so the app wires it exactly like the other
// internal/infra modules: NewModule in the composition root, Start driven by the
// phase runner.
package geoip

import (
	"context"

	geoiplib "github.com/chaos-plus/chaosplus/pkg/geoip"
	_ "github.com/chaos-plus/chaosplus/pkg/geoip/providers" // register geoip providers
)

// Config is the per-provider geoip configuration (re-exported from pkg/geoip so
// the app depends only on this module).
type Config = geoiplib.GeoIpConfig

// Module wires the geoip providers to the application lifecycle. It implements
// the app's Starter and Stopper capabilities.
type Module struct {
	cfg Config
}

// NewModule builds the module with the given provider configuration.
func NewModule(cfg Config) *Module {
	return &Module{cfg: cfg}
}

// Start applies the provider configuration and starts each provider's background
// database maintenance, tied to ctx.
func (m *Module) Start(ctx context.Context) error {
	geoiplib.Configure(m.cfg)
	geoiplib.StartProviders(ctx)
	return nil
}

// Stop waits for provider maintenance to exit before application resources are
// released or another application instance is bootstrapped in the same process.
func (m *Module) Stop(ctx context.Context) error {
	return geoiplib.StopProviders(ctx)
}
