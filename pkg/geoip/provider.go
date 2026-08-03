package geoip

import (
	"context"
)

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

// Stoppable joins background provider work before application resources are
// released. Providers without background work do not implement it.
type Stoppable interface {
	Stop(ctx context.Context) error
}

// GeoIpConfig holds optional per-provider configuration, one section per
// provider. Endpoint overrides support trusted internal mirrors while keeping
// the public service defaults.
type GeoIpConfig struct {
	Geolite2 struct {
		Owner      string `mapstructure:"owner" description:"geolite2 github owner"`
		Repo       string `mapstructure:"repo" description:"geolite2 github repo"`
		Db         string `mapstructure:"db" description:"geolite2 db asset name"`
		APIBaseURL string `mapstructure:"api_base_url" description:"github-compatible release API base URL"`
	} `mapstructure:"geolite2" group:"geolite2"`

	Ip2region struct {
		//
	} `mapstructure:"ip2region" group:"ip2region"`

	Ip2location struct {
		Token           string `mapstructure:"token" description:"ip2location token"`
		DownloadBaseURL string `mapstructure:"download_base_url" description:"ip2location download endpoint"`
	} `mapstructure:"ip2location" group:"ip2location"`

	Ipapi struct {
		BaseURL string `mapstructure:"base_url" description:"ipapi-compatible lookup endpoint"`
	} `mapstructure:"ipapi" group:"ipapi"`
}

// Configurable is an optional capability for providers that accept settings from
// application config. A provider applies only the fields it understands.
type Configurable interface {
	Configure(GeoIpConfig)
}

// GeoIpProviders holds all registered geoip providers.
var GeoIpProviders = make(map[string]GeoIpProvider)

// RegisterGeoIpProvider registers a provider by name.
func RegisterGeoIpProvider(name string, provider GeoIpProvider) {
	GeoIpProviders[name] = provider
}
