package types

import "context"

// GeoIpProvider defines the geolocation lookup contract.
type GeoIpProvider interface {
	GetIpInfo(ip string) (geo *GeoIp, err error)
}

// Startable is an optional capability for providers that maintain a local
// database (downloading and periodically refreshing it). The caller drives the
// lifecycle explicitly and the work stops when ctx is cancelled, so importing a
// provider package never triggers background downloads on its own. Providers
// that need no background work (e.g. live-API lookups) simply do not implement
// it.
type Startable interface {
	Start(ctx context.Context) error
}

// GeoIpProviders holds all registered geoip providers.
var GeoIpProviders = make(map[string]GeoIpProvider)

// RegisterGeoIpProvider registers a provider by name.
func RegisterGeoIpProvider(name string, provider GeoIpProvider) {
	GeoIpProviders[name] = provider
}
