package types

// GeoIpProvider defines the geolocation lookup contract.
type GeoIpProvider interface {
	GetIpInfo(ip string) (geo *GeoIp, err error)
}

// GeoIpProviders holds all registered geoip providers.
var GeoIpProviders = make(map[string]GeoIpProvider)

// RegisterGeoIpProvider registers a provider by name.
func RegisterGeoIpProvider(name string, provider GeoIpProvider) {
	GeoIpProviders[name] = provider
}
