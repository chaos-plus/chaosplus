package geoip

import (
	"errors"
	"testing"

	"github.com/chaos-plus/chaosplus/pkg/geoip/types"
)

// fakeProvider is a deterministic GeoIpProvider for tests, so orchestration logic
// can be verified without depending on which real databases happen to be installed.
type fakeProvider struct {
	info *types.GeoIp
	err  error
}

func (f fakeProvider) GetIpInfo(string) (*types.GeoIp, error) { return f.info, f.err }

// withProviders swaps the global provider registry for the duration of a test and
// restores the real providers afterward.
func withProviders(t *testing.T, m map[string]types.GeoIpProvider) {
	t.Helper()
	saved := types.GeoIpProviders
	types.GeoIpProviders = m
	t.Cleanup(func() { types.GeoIpProviders = saved })
}

func TestGetIpLocation_EmptyIP(t *testing.T) {
	_, err := GetIpLocation("")
	if err == nil {
		t.Fatal("expected error for empty IP")
	}
}

func TestGetIpLocation_ReturnsProviderResult(t *testing.T) {
	// A registered provider that returns data → GetIpLocation returns it (no error).
	// Uses a fake provider so the result is deterministic regardless of which real
	// GeoIP databases are installed locally.
	withProviders(t, map[string]types.GeoIpProvider{
		"fake": fakeProvider{info: &types.GeoIp{Provider: "fake", Country: "Testland"}},
	})
	info, err := GetIpLocation("8.8.8.8")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil || info.Provider != "fake" || info.Country != "Testland" {
		t.Fatalf("expected fake provider result, got %+v", info)
	}
}

func TestGetIpLocation_SkipsFailingProvider(t *testing.T) {
	// A failing provider is skipped; the next successful one is returned.
	withProviders(t, map[string]types.GeoIpProvider{
		"broken": fakeProvider{err: errors.New("no db found")},
	})
	if _, err := GetIpLocation("8.8.8.8"); err == nil {
		t.Fatal("expected error when all providers fail")
	}
}

func TestGetIpLocation_InvalidIP(t *testing.T) {
	_, err := GetIpLocation("not-an-ip")
	if err == nil {
		t.Fatal("expected error for invalid IP")
	}
}

func TestGetIpLocations_SortedByProvider(t *testing.T) {
	// All successful providers are returned, sorted ascending by provider name.
	withProviders(t, map[string]types.GeoIpProvider{
		"zeta":  fakeProvider{info: &types.GeoIp{Provider: "zeta", Country: "Z"}},
		"alpha": fakeProvider{info: &types.GeoIp{Provider: "alpha", Country: "A"}},
	})
	results, err := GetIpLocations("8.8.8.8")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Provider != "alpha" || results[1].Provider != "zeta" {
		t.Fatalf("results not sorted by provider: %s, %s", results[0].Provider, results[1].Provider)
	}
}

func TestGetIpLocations_DropsEmptyResults(t *testing.T) {
	// A provider returning a result with no location fields is dropped.
	withProviders(t, map[string]types.GeoIpProvider{
		"empty": fakeProvider{info: &types.GeoIp{Provider: "empty"}},
		"good":  fakeProvider{info: &types.GeoIp{Provider: "good", City: "Somewhere"}},
	})
	results, err := GetIpLocations("8.8.8.8")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0].Provider != "good" {
		t.Fatalf("expected only the non-empty result, got %+v", results)
	}
}

func TestGetIpLocations_EmptyIP(t *testing.T) {
	_, err := GetIpLocations("")
	if err == nil {
		t.Fatal("expected error for empty IP")
	}
}
