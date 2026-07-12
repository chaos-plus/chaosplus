package providers

import (
	"context"
	"errors"
	"net"
	"net/http"

	"github.com/chaos-plus/chaosplus/pkg/geoip"
	"github.com/oschwald/geoip2-golang"
)

// Geolite2 uses MaxMind GeoLite2-City database.
type Geolite2 struct {
	Owner string `mapstructure:"owner" description:"geolite2 github owner"`
	Repo  string `mapstructure:"repo" description:"geolite2 github repo"`
	Db    string `mapstructure:"db" description:"geolite2 db asset name"`

	// client overrides the HTTP client used for downloads; nil uses the shared
	// defaultDownloadClient. Unexported so tests can inject without exposing it.
	client *http.Client
}

func init() {
	geoip.RegisterGeoIpProvider("geolite2", &Geolite2{})
}

// httpClient returns the provider's HTTP client, falling back to the shared
// read-only default when none was injected.
func (m *Geolite2) httpClient() *http.Client {
	if m.client != nil {
		return m.client
	}
	return defaultDownloadClient
}

// Configure applies provider settings: Geolite2.Owner/Repo/Db override the GitHub
// mirror the database is fetched from.
func (m *Geolite2) Configure(c geoip.GeoIpConfig) {
	if c.Geolite2.Owner != "" {
		m.Owner = c.Geolite2.Owner
	}
	if c.Geolite2.Repo != "" {
		m.Repo = c.Geolite2.Repo
	}
	if c.Geolite2.Db != "" {
		m.Db = c.Geolite2.Db
	}
}

// Start begins background maintenance of the GeoLite2 database, bound to ctx.
func (m *Geolite2) Start(ctx context.Context) error {
	maintainDB(ctx, "geolite2", m.GetDbPath, func() error { return m.DownloadDb() })
	return nil
}

func (m *Geolite2) GetIpInfo(ip string) (*geoip.GeoIp, error) {
	if ip == "" {
		return nil, errors.New("ip is empty")
	}
	dbPath, err := m.GetDbPath()
	if err != nil {
		return nil, err
	}
	db, err := geoip2.Open(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	record, err := db.City(net.ParseIP(ip))
	if err != nil {
		return nil, err
	}

	var country string
	if record.Country.Names["zh-CN"] != "" {
		country = record.Country.Names["zh-CN"]
	} else {
		country = record.Country.Names["en"]
	}
	var province string
	if len(record.Subdivisions) > 0 {
		if record.Subdivisions[0].Names["zh-CN"] != "" {
			province = record.Subdivisions[0].Names["zh-CN"]
		} else {
			province = record.Subdivisions[0].Names["en"]
		}
	}
	var city string
	if record.City.Names["zh-CN"] != "" {
		city = record.City.Names["zh-CN"]
	} else {
		city = record.City.Names["en"]
	}

	return &geoip.GeoIp{
		Provider: "geolite2",
		Ip:       ip,
		Country:  country,
		City:     city,
		Province: province,
	}, nil
}
