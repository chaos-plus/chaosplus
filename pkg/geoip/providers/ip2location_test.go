package providers

import (
	"context"
	"testing"

	"github.com/chaos-plus/chaosplus/pkg/geoip"
	"github.com/stretchr/testify/assert"
)

func TestIP2Location_Configure(t *testing.T) {
	m := &IP2Location{}

	var c geoip.GeoIpConfig
	c.Ip2location.Token = "abc"
	c.Ip2location.DownloadBaseURL = "http://127.0.0.1:9000/download"
	m.Configure(c)
	assert.Equal(t, "abc", m.Token)
	assert.Equal(t, "http://127.0.0.1:9000/download", m.DownloadBaseURL)

	m.Configure(geoip.GeoIpConfig{})
	assert.Equal(t, "abc", m.Token)
}

func TestIP2LocationStartRulesAndDefaultClient(t *testing.T) {
	provider := &IP2Location{}
	assert.NotNil(t, defaultDownloadClient)
	assert.NoError(t, provider.Start(context.Background()))
	provider.Token = "token"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.NoError(t, provider.Start(ctx))
	assert.NoError(t, provider.Stop(t.Context()))
}
