package geoip

import (
	"context"
	"errors"
	"log/slog"
	"sort"

	"github.com/chaos-plus/chaosplus/pkg/geoip/types"
)

// StartProviders starts the background database maintenance of every registered
// provider that implements types.Startable, tying it to ctx. Providers without
// background work are skipped. Call this once at application startup; cancel ctx
// on shutdown to stop all refresh goroutines. Until it is called, no provider
// downloads anything — importing the providers package has no side effects.
func StartProviders(ctx context.Context) {
	for name, provider := range types.GeoIpProviders {
		s, ok := provider.(types.Startable)
		if !ok {
			continue
		}
		if err := s.Start(ctx); err != nil {
			slog.Error("start geoip provider", "provider", name, "err", err)
		}
	}
}

// GetIpLocation returns geolocation info for an IP using the first successful provider.
func GetIpLocation(ip string) (*types.GeoIp, error) {
	if ip == "" {
		return nil, errors.New("ip is empty")
	}
	if len(types.GeoIpProviders) == 0 {
		return nil, errors.New("no geoip provider")
	}
	for name, provider := range types.GeoIpProviders {
		geoip, err := provider.GetIpInfo(ip)
		if err != nil {
			slog.Error("get ip info by provider error", "provider", name, "err", err.Error())
			continue
		}
		if geoip == nil {
			slog.Error("get ip info by provider error", "provider", name, "err", "no ip data info")
			continue
		}
		return geoip, nil
	}
	return nil, errors.New("no geoip provider found")
}

// GetIpLocations returns geolocation info from all providers.
func GetIpLocations(ip string) ([]*types.GeoIp, error) {
	if ip == "" {
		return nil, errors.New("ip is empty")
	}
	if len(types.GeoIpProviders) == 0 {
		return nil, errors.New("no geoip provider")
	}
	results := make([]*types.GeoIp, 0)
	for name, provider := range types.GeoIpProviders {
		geoip, err := provider.GetIpInfo(ip)
		if err != nil {
			slog.Error("get ip info error", "provider", name, "err", err)
			continue
		}
		if geoip == nil {
			slog.Error("get ip info error", "provider", name, "err", "no ip data info")
			continue
		}
		if geoip.Country == "" && geoip.Province == "" && geoip.City == "" {
			slog.Error("get ip info error", "provider", name, "err", "no ip data info")
			continue
		}
		results = append(results, geoip)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Provider < results[j].Provider
	})

	return results, nil
}
