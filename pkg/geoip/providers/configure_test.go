package providers

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/chaos-plus/chaosplus/pkg/geoip"
)

func TestIP2Location_Configure(t *testing.T) {
	m := &IP2Location{}

	var c geoip.GeoIpConfig
	c.Ip2location.Token = "abc"
	m.Configure(c)
	assert.Equal(t, "abc", m.Token)

	m.Configure(geoip.GeoIpConfig{}) // empty config leaves the value unchanged
	assert.Equal(t, "abc", m.Token)
}

func TestGeolite2_Configure(t *testing.T) {
	m := &Geolite2{}

	var c geoip.GeoIpConfig
	c.Geolite2.Owner = "o"
	c.Geolite2.Repo = "r"
	c.Geolite2.Db = "d"
	m.Configure(c)

	assert.Equal(t, "o", m.Owner)
	assert.Equal(t, "r", m.Repo)
	assert.Equal(t, "d", m.Db)
}
