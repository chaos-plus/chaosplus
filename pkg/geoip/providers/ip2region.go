package providers

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/chaos-plus/chaosplus/pkg/geoip/types"
	"github.com/lionsoul2014/ip2region/binding/golang/xdb"
	"github.com/robfig/cron/v3"
)

var ip2regionDatabase = "ip2region_v4.xdb"

// IP2Region uses ip2region xdb database.
type IP2Region struct {
	vector  []byte
	version *xdb.Version
}

func init() {
	m := &IP2Region{version: xdb.IPv4}
	types.RegisterGeoIpProvider("ip2region", m)
	go func() {
		dbPath, err := m.GetDbPath()
		if err != nil || dbPath == "" {
			m.DownloadDb()
		}
	}()
	timer := cron.New()
	timer.AddFunc("@every 1h", func() {
		m.DownloadDb()
	})
	timer.AddFunc("@every 1m", func() {
		dbPath, err := m.GetDbPath()
		if err != nil || dbPath == "" {
			err := m.DownloadDb()
			if err != nil {
				slog.Error("ip2region download db error", "err", err)
			}
		}
	})
	timer.Start()
}

func (m *IP2Region) GetIpInfo(ip string) (*types.GeoIp, error) {
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

	return &types.GeoIp{
		Provider: "ip2region",
		Ip:       ip,
		Country:  regions[0],
		Province: regions[1],
		City:     regions[2],
	}, nil
}
