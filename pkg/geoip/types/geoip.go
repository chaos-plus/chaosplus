package types

// GeoIp holds geolocation data for an IP address.
type GeoIp struct {
	Provider string `json:"provider"`
	Ip       string `json:"ip"`
	Country  string `json:"country"`
	Province string `json:"province"`
	City     string `json:"city"`
}
