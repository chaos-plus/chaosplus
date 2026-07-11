package providers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/chaos-plus/chaosplus/pkg/geoip/types"
	"github.com/ip2location/ip2location-go/v9"
)

// IP2Location uses IP2Location database.
type IP2Location struct {
	Token string

	// client overrides the HTTP client used for downloads; nil uses the shared
	// defaultDownloadClient. Kept unexported so it can be injected in tests
	// without becoming public API.
	client *http.Client
}

func init() {
	types.RegisterGeoIpProvider("ip2location", &IP2Location{})
}

// httpClient returns the provider's HTTP client, falling back to the shared
// read-only default when none was injected.
func (m *IP2Location) httpClient() *http.Client {
	if m.client != nil {
		return m.client
	}
	return defaultDownloadClient
}

// Start begins background maintenance of the IP2Location database, bound to ctx.
func (m *IP2Location) Start(ctx context.Context) error {
	maintainDB(ctx, "ip2location", m.GetDbPath, func() error { return m.DownloadDb() })
	return nil
}

func (m *IP2Location) GetIpInfo(ip string) (*types.GeoIp, error) {
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

	return &types.GeoIp{
		Provider: "ip2location",
		Ip:       ip,
		Country:  strings.ReplaceAll(record.Country_long, "-", ""),
		Province: strings.ReplaceAll(record.Region, "-", ""),
		City:     strings.ReplaceAll(record.City, "-", ""),
	}, nil
}
