package providers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/chaos-plus/chaosplus/pkg/geoip/types"
)

// maxResponseBytes caps how much of the external provider's response we read into
// memory before JSON-decoding. A hostile or misbehaving endpoint could otherwise
// stream an unbounded body and exhaust memory; the legitimate JSON payload is well
// under this 64 KiB ceiling.
const maxResponseBytes = 64 * 1024

func init() {
	types.RegisterGeoIpProvider("ipapi", &IPAPIProvider{})
}

// defaultLookupClient is the shared, read-only HTTP client for live lookups. It
// is assigned once and only ever read, so it is safe for concurrent use.
var defaultLookupClient = &http.Client{Timeout: 3 * time.Second}

// IPAPIProvider uses ipapi.co free tier for lookups.
type IPAPIProvider struct {
	// client overrides the HTTP client used for lookups; nil uses the shared
	// defaultLookupClient. Unexported so tests can inject without exposing it.
	client *http.Client
}

// httpClient returns the provider's HTTP client, falling back to the shared
// read-only default when none was injected.
func (p *IPAPIProvider) httpClient() *http.Client {
	if p.client != nil {
		return p.client
	}
	return defaultLookupClient
}

func (p *IPAPIProvider) GetIpInfo(ip string) (*types.GeoIp, error) {
	if ip == "" {
		return nil, errors.New("ip is empty")
	}
	if ip == "127.0.0.1" || ip == "::1" {
		return &types.GeoIp{Provider: "ipapi", Ip: ip, Country: "Local", Province: "Local", City: "Local"}, nil
	}
	if net.ParseIP(ip) == nil {
		return nil, fmt.Errorf("invalid IP address: %s", ip)
	}

	url := fmt.Sprintf("https://ipapi.co/%s/json/", ip)
	resp, err := p.httpClient().Get(url)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var raw struct {
		Ip          string `json:"ip"`
		City        string `json:"city"`
		Region      string `json:"region"`
		CountryName string `json:"country_name"`
		CountryCode string `json:"country_code"`
	}
	// Bound the response body before decoding to prevent memory exhaustion from an
	// oversized/hostile response (the provider is an untrusted external boundary).
	limited := io.LimitReader(resp.Body, maxResponseBytes)
	if err := json.NewDecoder(limited).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &types.GeoIp{
		Provider: "ipapi",
		Ip:       raw.Ip,
		Country:  raw.CountryName,
		Province: raw.Region,
		City:     raw.City,
	}, nil
}
