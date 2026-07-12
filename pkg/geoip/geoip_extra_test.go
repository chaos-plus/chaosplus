package geoip

import (
	"errors"
	"testing"
)

// --- GetIpLocation: empty registry returns the "no geoip provider" error ---

func TestGetIpLocation_NoProviders(t *testing.T) {
	withProviders(t, map[string]GeoIpProvider{})
	_, err := GetIpLocation("8.8.8.8")
	if err == nil {
		t.Fatal("expected error when no providers are registered")
	}
}

// --- GetIpLocation: a provider returning (nil, nil) is skipped ---

func TestGetIpLocation_SkipsNilResult(t *testing.T) {
	// First provider yields nil data with no error (the geoip == nil branch); the
	// second provider supplies the answer.
	withProviders(t, map[string]GeoIpProvider{
		"nil":  fakeProvider{info: nil, err: nil},
		"good": fakeProvider{info: &GeoIp{Provider: "good", Country: "US"}},
	})
	info, err := GetIpLocation("8.8.8.8")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil || info.Provider != "good" {
		t.Fatalf("expected the non-nil provider result, got %+v", info)
	}
}

// --- GetIpLocation: all providers yield nil → "no geoip provider found" ---

func TestGetIpLocation_AllNil(t *testing.T) {
	withProviders(t, map[string]GeoIpProvider{
		"nil": fakeProvider{info: nil, err: nil},
	})
	if _, err := GetIpLocation("8.8.8.8"); err == nil {
		t.Fatal("expected error when every provider returns nil data")
	}
}

// --- GetIpLocations: empty registry returns the "no geoip provider" error ---

func TestGetIpLocations_NoProviders(t *testing.T) {
	withProviders(t, map[string]GeoIpProvider{})
	_, err := GetIpLocations("8.8.8.8")
	if err == nil {
		t.Fatal("expected error when no providers are registered")
	}
}

// --- GetIpLocations: a (nil, nil) provider result is skipped ---

func TestGetIpLocations_SkipsNilResult(t *testing.T) {
	withProviders(t, map[string]GeoIpProvider{
		"nil":  fakeProvider{info: nil, err: nil},
		"good": fakeProvider{info: &GeoIp{Provider: "good", City: "Town"}},
	})
	results, err := GetIpLocations("8.8.8.8")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0].Provider != "good" {
		t.Fatalf("expected only the non-nil result, got %+v", results)
	}
}

// --- GetIpLocations: a failing provider is skipped, error ones dropped silently ---

func TestGetIpLocations_SkipsFailingProvider(t *testing.T) {
	withProviders(t, map[string]GeoIpProvider{
		"broken": fakeProvider{err: errors.New("db missing")},
		"good":   fakeProvider{info: &GeoIp{Provider: "good", Country: "US"}},
	})
	results, err := GetIpLocations("8.8.8.8")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0].Provider != "good" {
		t.Fatalf("expected only the successful result, got %+v", results)
	}
}

// --- GetIpLocations: when all providers fail, the result slice is empty (not nil) ---

func TestGetIpLocations_AllFail(t *testing.T) {
	withProviders(t, map[string]GeoIpProvider{
		"broken": fakeProvider{err: errors.New("db missing")},
	})
	results, err := GetIpLocations("8.8.8.8")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected an empty result slice, got %+v", results)
	}
}
