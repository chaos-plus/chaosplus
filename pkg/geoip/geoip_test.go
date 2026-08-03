package geoip_test

import (
	"context"
	"testing"

	"github.com/chaos-plus/chaosplus/pkg/geoip"
	"github.com/chaos-plus/chaosplus/pkg/geoip/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLookupInputAndProviderAvailability(t *testing.T) {
	saved := geoip.GeoIpProviders
	geoip.GeoIpProviders = map[string]geoip.GeoIpProvider{}
	t.Cleanup(func() { geoip.GeoIpProviders = saved })

	_, err := geoip.GetIpLocation("")
	assert.ErrorContains(t, err, "ip is empty")
	_, err = geoip.GetIpLocations("")
	assert.ErrorContains(t, err, "ip is empty")
	_, err = geoip.GetIpLocation("8.8.8.8")
	assert.ErrorContains(t, err, "no geoip provider")
	_, err = geoip.GetIpLocations("8.8.8.8")
	assert.ErrorContains(t, err, "no geoip provider")
}

func TestLookupWithRealLocalProvider(t *testing.T) {
	saved := geoip.GeoIpProviders
	geoip.GeoIpProviders = map[string]geoip.GeoIpProvider{"ipapi": &providers.IPAPI{}}
	t.Cleanup(func() { geoip.GeoIpProviders = saved })

	result, err := geoip.GetIpLocation("127.0.0.1")
	require.NoError(t, err)
	assert.Equal(t, "Local", result.Country)
	results, err := geoip.GetIpLocations("::1")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "ipapi", results[0].Provider)
	geoip.StartProviders(context.Background())
}

func TestLookupContinuesAfterRealProviderErrorAndSortsResults(t *testing.T) {
	saved := geoip.GeoIpProviders
	geoip.GeoIpProviders = map[string]geoip.GeoIpProvider{
		"first":  &providers.IPAPI{},
		"second": &providers.IPAPI{},
	}
	t.Cleanup(func() { geoip.GeoIpProviders = saved })

	_, err := geoip.GetIpLocation("not-an-ip")
	assert.ErrorContains(t, err, "no geoip provider found")
	results, err := geoip.GetIpLocations("not-an-ip")
	require.NoError(t, err)
	assert.Empty(t, results)

	results, err = geoip.GetIpLocations("127.0.0.1")
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, "ipapi", results[0].Provider)
}

func TestConfigureRealDatabaseProviders(t *testing.T) {
	location := &providers.IP2Location{}
	lite := &providers.Geolite2{}
	saved := geoip.GeoIpProviders
	geoip.GeoIpProviders = map[string]geoip.GeoIpProvider{"location": location, "lite": lite, "ipapi": &providers.IPAPI{}}
	t.Cleanup(func() { geoip.GeoIpProviders = saved })

	var config geoip.GeoIpConfig
	config.Ip2location.Token = "token"
	config.Geolite2.Owner = "owner"
	config.Geolite2.Repo = "repo"
	config.Geolite2.Db = "database.mmdb"
	geoip.Configure(config)
	assert.Equal(t, "token", location.Token)
	assert.Equal(t, "owner", lite.Owner)
	assert.Equal(t, "repo", lite.Repo)
	assert.Equal(t, "database.mmdb", lite.Db)
}

func TestRealDatabaseProviderLifecycleStops(t *testing.T) {
	saved := geoip.GeoIpProviders
	geoip.GeoIpProviders = map[string]geoip.GeoIpProvider{
		"geolite2":    &providers.Geolite2{},
		"ip2location": &providers.IP2Location{},
		"ip2region":   &providers.IP2Region{},
	}
	t.Cleanup(func() { geoip.GeoIpProviders = saved })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	geoip.StartProviders(ctx)
	require.NoError(t, geoip.StopProviders(t.Context()))
}
