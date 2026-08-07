package providers

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/chaos-plus/chaosplus/pkg/geoip"
	"github.com/lionsoul2014/ip2region/binding/golang/xdb"
)

var ip2regionDatabase = "ip2region_v4.xdb"

// IP2Region uses ip2region xdb database.
type IP2Region struct {
	enabled bool
	vector  []byte
	version *xdb.Version
	worker  maintenanceWorker
}

func init() {
	geoip.RegisterGeoIpProvider("ip2region", &IP2Region{version: xdb.IPv4})
}

// Configure applies provider settings. Ip2region.Enabled turns on maintenance;
// it defaults to off because building the xdb clones a git repo and runs go at
// startup, which must be an explicit operator choice.
func (m *IP2Region) Configure(c geoip.GeoIpConfig) {
	m.enabled = c.Ip2region.Enabled
}

// Start begins background maintenance of the ip2region database, bound to ctx.
// Unlike the other providers it builds the xdb from the upstream git repo rather
// than an HTTP download, so it needs no injected HTTP client. It no-ops unless
// explicitly enabled in configuration.
func (m *IP2Region) Start(ctx context.Context) error {
	if !m.enabled {
		slog.Info("geoip ip2region disabled: not enabled in configuration")
		return nil
	}
	m.worker.start(ctx, func(ctx context.Context) {
		maintainDB(ctx, "ip2region", m.GetDbPath, m.DownloadDb)
	})
	return nil
}

func (m *IP2Region) Stop(ctx context.Context) error { return m.worker.stop(ctx) }

func (m *IP2Region) GetIpInfo(ip string) (*geoip.GeoIp, error) {
	if ip == "" {
		return nil, errors.New("ip is empty")
	}
	dbPath, err := m.GetDbPath()
	if err != nil {
		return nil, err
	}

	if len(m.vector) == 0 {
		m.vector, err = xdb.LoadVectorIndexFromFile(dbPath)
		if err != nil {
			slog.Error("ip2region load db error", "err", err)
			return nil, err
		}
		slog.Info("ip2region load db success", "dbPath", dbPath)
	}

	if len(m.vector) == 0 {
		slog.Error("ip2region load db error", "err", "vector is nil")
		return nil, errors.New("ip2region load db error")
	}

	searcher, err := xdb.NewWithVectorIndex(m.version, dbPath, m.vector)
	if err != nil {
		return nil, err
	}
	defer searcher.Close()

	record, err := searcher.Search(ip)
	if err != nil {
		return nil, err
	}
	// Record is pipe-delimited "country|region|province|city|isp",
	// e.g. "China|0|Henan|Zhengzhou|Unicom".
	regions := strings.Split(record, "|")
	if len(regions) < 4 {
		return nil, errors.New("invalid record")
	}

	return &geoip.GeoIp{
		Provider: "ip2region",
		Ip:       ip,
		Country:  regions[0],
		Province: regions[1],
		City:     regions[2],
	}, nil
}
