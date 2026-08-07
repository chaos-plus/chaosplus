package providers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/pkg/geoip"
)

func TestIPAPI_GetIpInfo_EmptyIP(t *testing.T) {
	p := &IPAPI{}
	_, err := p.GetIpInfo("")
	if err == nil {
		t.Fatal("expected error for empty ip")
	}
}

func TestIPAPI_GetIpInfo_Localhost(t *testing.T) {
	p := &IPAPI{}
	for _, ip := range []string{"127.0.0.1", "::1"} {
		geo, err := p.GetIpInfo(ip)
		if err != nil {
			t.Fatalf("ip %s: unexpected error: %v", ip, err)
		}
		if geo.Country != "Local" || geo.City != "Local" || geo.Province != "Local" {
			t.Fatalf("ip %s: expected Local fields, got %+v", ip, geo)
		}
		if geo.Provider != "ipapi" || geo.Ip != ip {
			t.Fatalf("ip %s: unexpected provider/ip: %+v", ip, geo)
		}
	}
}

func TestIPAPI_GetIpInfo_InvalidIP(t *testing.T) {
	p := &IPAPI{}
	_, err := p.GetIpInfo("not-an-ip")
	if err == nil {
		t.Fatal("expected error for invalid ip")
	}
	if !strings.Contains(err.Error(), "invalid IP address") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIPAPI_GetIpInfo_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"ip": "8.8.8.8",
			"city": "Mountain View",
			"region": "California",
			"country_name": "United States",
			"country_code": "US"
		}`))
	}))
	defer ts.Close()

	// Inject a client that redirects ipapi.co to the test server.
	p := &IPAPI{BaseURL: ts.URL}
	geo, err := p.GetIpInfo("8.8.8.8")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if geo.Country != "United States" || geo.City != "Mountain View" || geo.Province != "California" {
		t.Fatalf("unexpected geo: %+v", geo)
	}
	if geo.Provider != "ipapi" || geo.Ip != "8.8.8.8" {
		t.Fatalf("unexpected provider/ip: %+v", geo)
	}
}

func TestIPAPI_GetIpInfo_Non200(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer ts.Close()

	p := &IPAPI{BaseURL: ts.URL}
	_, err := p.GetIpInfo("8.8.8.8")
	if err == nil || !strings.Contains(err.Error(), "unexpected status") {
		t.Fatalf("expected unexpected status error, got %v", err)
	}
}

func TestIPAPI_GetIpInfo_BadJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{not-json`))
	}))
	defer ts.Close()

	p := &IPAPI{BaseURL: ts.URL}
	_, err := p.GetIpInfo("8.8.8.8")
	if err == nil || !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestIPAPI_GetIpInfo_TransportError(t *testing.T) {
	// Point the client at a server that's already closed so the GET fails.
	ts := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := ts.URL
	ts.Close()

	p := &IPAPI{BaseURL: url}
	_, err := p.GetIpInfo("8.8.8.8")
	if err == nil || !strings.Contains(err.Error(), "http request") {
		t.Fatalf("expected http request error, got %v", err)
	}
}

func TestIPAPI_HTTPClient_Default(t *testing.T) {
	if defaultLookupClient == nil {
		t.Fatal("expected non-nil default client")
	}
	if defaultLookupClient.Timeout != 3*time.Second {
		t.Fatalf("expected 3s timeout, got %v", defaultLookupClient.Timeout)
	}
}

func TestIPAPIConfigure(t *testing.T) {
	var config geoip.GeoIpConfig
	config.Ipapi.BaseURL = "http://127.0.0.1:9000/"
	provider := &IPAPI{}
	provider.Configure(config)
	if provider.BaseURL != "http://127.0.0.1:9000" {
		t.Fatalf("unexpected base URL %q", provider.BaseURL)
	}
}
