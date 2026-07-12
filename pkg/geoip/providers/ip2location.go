package providers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/chaos-plus/chaosplus/pkg/geoip"
	"github.com/ip2location/ip2location-go/v9"
)

// IP2Location uses IP2Location database.
type IP2Location struct {
	Token string `mapstructure:"token" description:"ip2location token"`

	// client overrides the HTTP client used for downloads; nil uses the shared
	// defaultDownloadClient. Kept unexported so it can be injected in tests
	// without becoming public API.
	client *http.Client
}

func init() {
	geoip.RegisterGeoIpProvider("ip2location", &IP2Location{})
}

// httpClient returns the provider's HTTP client, falling back to the shared
// read-only default when none was injected.
func (m *IP2Location) httpClient() *http.Client {
	if m.client != nil {
		return m.client
	}
	return defaultDownloadClient
}

// Configure applies provider settings: Ip2location.Token enables downloads.
func (m *IP2Location) Configure(c geoip.GeoIpConfig) {
	if c.Ip2location.Token != "" {
		m.Token = c.Ip2location.Token
	}
}

// Start begins background maintenance of the IP2Location database, bound to ctx.
// It no-ops without a token: the download endpoint requires one, so attempting it
// would only fetch an error page. Set Token to enable the provider.
func (m *IP2Location) Start(ctx context.Context) error {
	if m.Token == "" {
		slog.Info("geoip ip2location disabled: no token configured")
		return nil
	}
	maintainDB(ctx, "ip2location", m.GetDbPath, func() error { return m.DownloadDb() })
	return nil
}

func (m *IP2Location) GetIpInfo(ip string) (*geoip.GeoIp, error) {
	if ip == "" {
		return nil, errors.New("ip is empty")
	}
	dbPath, err := m.GetDbPath()
	if err != nil {
		return nil, err
	}
	db, err := ip2location.OpenDB(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	record, err := db.Get_all(ip)
	if err != nil {
		return nil, err
	}

	return &geoip.GeoIp{
		Provider: "ip2location",
		Ip:       ip,
		Country:  strings.ReplaceAll(record.Country_long, "-", ""),
		Province: strings.ReplaceAll(record.Region, "-", ""),
		City:     strings.ReplaceAll(record.City, "-", ""),
	}, nil
}
