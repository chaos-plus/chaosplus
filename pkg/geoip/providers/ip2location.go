package providers

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/chaos-plus/chaosplus/pkg/geoip/types"
	"github.com/ip2location/ip2location-go/v9"
	"github.com/robfig/cron/v3"
)

// IP2Location uses IP2Location database.
type IP2Location struct {
	Token string
}

func init() {
	m := &IP2Location{}
	types.RegisterGeoIpProvider("ip2location", m)
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
				slog.Error("ip2location download db error", "err", err)
			}
		}
	})
	timer.Start()
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
