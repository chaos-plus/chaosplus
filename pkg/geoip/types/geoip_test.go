package types

import "testing"

// stubProvider is a minimal GeoIpProvider used to exercise the registry.
type stubProvider struct{ name string }

func (s stubProvider) GetIpInfo(string) (*GeoIp, error) {
	return &GeoIp{Provider: s.name}, nil
}

func TestRegisterGeoIpProvider_AddsAndOverwrites(t *testing.T) {
	// Swap the global registry so this test does not leak into others.
	saved := GeoIpProviders
	GeoIpProviders = make(map[string]GeoIpProvider)
	t.Cleanup(func() { GeoIpProviders = saved })

	RegisterGeoIpProvider("a", stubProvider{name: "first"})
	if len(GeoIpProviders) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(GeoIpProviders))
	}
	got, ok := GeoIpProviders["a"]
	if !ok {
		t.Fatal("provider 'a' not registered")
	}
	info, err := got.GetIpInfo("1.2.3.4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Provider != "first" {
		t.Fatalf("expected provider 'first', got %q", info.Provider)
	}

	// Registering the same name overwrites the prior entry.
	RegisterGeoIpProvider("a", stubProvider{name: "second"})
	if len(GeoIpProviders) != 1 {
		t.Fatalf("expected registry size to stay 1 after overwrite, got %d", len(GeoIpProviders))
	}
	info, _ = GeoIpProviders["a"].GetIpInfo("1.2.3.4")
	if info.Provider != "second" {
		t.Fatalf("expected overwrite to 'second', got %q", info.Provider)
	}
}

func TestGeoIp_FieldsAssignable(t *testing.T) {
	g := GeoIp{
		Provider: "p",
		Ip:       "8.8.8.8",
		Country:  "US",
		Province: "CA",
		City:     "Mountain View",
	}
	if g.Provider != "p" || g.Ip != "8.8.8.8" || g.Country != "US" ||
		g.Province != "CA" || g.City != "Mountain View" {
		t.Fatalf("unexpected struct contents: %+v", g)
	}
}
