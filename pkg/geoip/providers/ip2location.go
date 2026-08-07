package providers

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/chaos-plus/chaosplus/pkg/geoip"
	"github.com/ip2location/ip2location-go/v9"
)

// IP2Location uses IP2Location database.
type IP2Location struct {
	Token           string `mapstructure:"token" description:"ip2location token"`
	DownloadBaseURL string `mapstructure:"download_base_url" description:"ip2location download endpoint"`

	worker maintenanceWorker
}

func init() {
	geoip.RegisterGeoIpProvider("ip2location", &IP2Location{})
}

// Configure applies provider settings: Ip2location.Token enables downloads.
func (m *IP2Location) Configure(c geoip.GeoIpConfig) {
	if c.Ip2location.Token != "" {
		m.Token = c.Ip2location.Token
	}
	if c.Ip2location.DownloadBaseURL != "" {
		m.DownloadBaseURL = c.Ip2location.DownloadBaseURL
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
	m.worker.start(ctx, func(ctx context.Context) {
		maintainDB(ctx, "ip2location", m.GetDbPath, func() error { return m.DownloadDb() })
	})
	return nil
}

func (m *IP2Location) Stop(ctx context.Context) error { return m.worker.stop(ctx) }

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
