package geoip

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeIPv4(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain ipv4", "203.0.113.7", "203.0.113.7"},
		{"ipv4 with spaces", "  203.0.113.7 ", "203.0.113.7"},
		{"ipv4-mapped ipv6", "::ffff:192.168.1.5", "192.168.1.5"},
		{"ipv6 loopback maps to v4", "::1", "127.0.0.1"},
		{"ipv6 global rejected", "2001:db8::1", ""},
		{"garbage rejected", "not-an-ip", ""},
		{"empty rejected", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, normalizeIPv4(tc.in))
		})
	}
}

func TestIsIPv4(t *testing.T) {
	valid := []string{"8.8.8.8", "127.0.0.1", "192.168.0.1", " 203.0.113.9 "}
	for _, s := range valid {
		assert.Truef(t, isIPv4(s), "expected %q to be valid IPv4", s)
	}

	invalid := []string{
		"123.123.123.1234", // octet out of range / extra digit
		"256.1.1.1",        // octet > 255
		"1.2.3",            // too few octets
		"::1",              // IPv6 loopback
		"2001:db8::1",      // IPv6
		"::ffff:1.2.3.4",   // IPv4-mapped IPv6
		"example.com",      // hostname
		"",                 // empty
	}
	for _, s := range invalid {
		assert.Falsef(t, isIPv4(s), "expected %q to be invalid IPv4", s)
	}
}

func TestRankIPv4(t *testing.T) {
	assert.Equal(t, rankPublic, rankIPv4("8.8.8.8"))
	assert.Equal(t, rankPrivate, rankIPv4("10.1.2.3"))
	assert.Equal(t, rankPrivate, rankIPv4("192.168.0.1"))
	assert.Equal(t, rankPrivate, rankIPv4("172.16.5.4"))
	assert.Equal(t, rankLoopback, rankIPv4("127.0.0.1"))
	assert.Equal(t, rankOther, rankIPv4("169.254.1.1")) // link-local
	assert.Equal(t, rankOther, rankIPv4("bad"))

	// Public must outrank private, which must outrank loopback.
	assert.Greater(t, rankIPv4("8.8.8.8"), rankIPv4("10.0.0.1"))
	assert.Greater(t, rankIPv4("10.0.0.1"), rankIPv4("127.0.0.1"))
}
