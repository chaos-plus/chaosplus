package providers

import (
	"errors"
	"net"

	"github.com/chaos-plus/chaosplus/pkg/geoip/types"
	"github.com/oschwald/geoip2-golang"
	"github.com/robfig/cron/v3"
)

// Geolite2 uses MaxMind GeoLite2-City database.
type Geolite2 struct {
	Owner string
	Repo  string
	Db    string
}

func init() {
	m := &Geolite2{}
	types.RegisterGeoIpProvider("geolite2", m)
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
			m.DownloadDb()
		}
	})
	timer.Start()
}

func (m *Geolite2) GetIpInfo(ip string) (*types.GeoIp, error) {
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

	return &types.GeoIp{
		Provider: "geolite2",
		Ip:       ip,
		Country:  country,
		City:     city,
		Province: province,
	}, nil
}
