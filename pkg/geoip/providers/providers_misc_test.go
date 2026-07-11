package providers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- ip2region ---

func TestIP2Region_GetIpInfo_EmptyIP(t *testing.T) {
	m := &IP2Region{}
	if _, err := m.GetIpInfo(""); err == nil {
		t.Fatal("expected error for empty ip")
	}
}

func TestIP2Region_GetDbPath_NotFound(t *testing.T) {
	m := &IP2Region{}
	// On a clean machine no ip2region xdb is present, so GetDbPath returns an error.
	// If a db happens to exist locally we just assert no panic and a usable result.
	path, err := m.GetDbPath()
	if err != nil {
		if !strings.Contains(err.Error(), "not found") {
			t.Fatalf("unexpected error: %v", err)
		}
		return
	}
	if path == "" {
		t.Fatal("GetDbPath returned empty path without error")
	}
}

// --- geolite2 ---

func TestGeolite2_GetIpInfo_EmptyIP(t *testing.T) {
	m := &Geolite2{}
	if _, err := m.GetIpInfo(""); err == nil {
		t.Fatal("expected error for empty ip")
	}
}

func TestGeolite2_DownloadDb_AssetNotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Release JSON that does NOT contain the requested asset name.
		_, _ = w.Write([]byte(`{"tag_name":"v9.9","assets":[{"name":"other.mmdb","browser_download_url":"http://x/y"}]}`))
	}))
	defer ts.Close()
	c := ts.Client()
	c.Transport = &fixedURLTransport{base: ts.URL, inner: c.Transport}

	m := &Geolite2{Owner: "o", Repo: "r", Db: "GeoLite2-City.mmdb", client: c}
	err := m.DownloadDb("GeoLite2-City.mmdb")
	if err == nil || !strings.Contains(err.Error(), "not found in release") {
		t.Fatalf("expected asset-not-found error, got %v", err)
	}
}

func TestGeolite2_DownloadDb_ReleaseError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()
	c := ts.Client()
	c.Transport = &fixedURLTransport{base: ts.URL, inner: c.Transport}

	m := &Geolite2{Owner: "o", Repo: "r", client: c}
	if err := m.DownloadDb("GeoLite2-City.mmdb"); err == nil {
		t.Fatal("expected error when release fetch fails")
	}
}

func TestGeolite2_DownloadDb_NoArgsEmptyDb(t *testing.T) {
	// With no names and an empty m.Db, DownloadDb sets the default db name but the
	// loop body never executes (names stays empty), so it returns nil without any
	// network access. This documents the current (quirky) no-op behavior.
	m := &Geolite2{Owner: "o", Repo: "r"}
	if err := m.DownloadDb(); err != nil {
		t.Fatalf("expected nil from no-arg empty-Db call, got %v", err)
	}
	if m.Db != "GeoLite2-City.mmdb" {
		t.Fatalf("expected default Db name to be set, got %q", m.Db)
	}
}

func TestGeolite2_DownloadDb_NoArgsWithDb(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// When m.Db is preset, no-arg DownloadDb downloads names=[m.Db]; respond with
		// a release missing that asset to exercise the loop + asset-not-found path.
		_, _ = w.Write([]byte(`{"tag_name":"v1","assets":[]}`))
	}))
	defer ts.Close()
	c := ts.Client()
	c.Transport = &fixedURLTransport{base: ts.URL, inner: c.Transport}

	m := &Geolite2{Owner: "o", Repo: "r", Db: "Custom.mmdb", client: c}
	err := m.DownloadDb()
	if err == nil || !strings.Contains(err.Error(), "not found in release") {
		t.Fatalf("expected asset-not-found for preset Db, got %v", err)
	}
}

func TestGeolite2_GetDbPath_NotFound(t *testing.T) {
	m := &Geolite2{}
	path, err := m.GetDbPath()
	if err == nil && path == "" {
		t.Fatal("expected error or a path")
	}
}

// --- ip2location ---

func TestIP2Location_GetIpInfo_EmptyIP(t *testing.T) {
	m := &IP2Location{}
	if _, err := m.GetIpInfo(""); err == nil {
		t.Fatal("expected error for empty ip")
	}
}

func TestIP2Location_DownloadDb_EmptyCode(t *testing.T) {
	m := &IP2Location{}
	if _, err := m.downloadDb(""); err == nil {
		t.Fatal("expected error for empty code")
	}
}

func TestIP2Location_DownloadDb_DownloadError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer ts.Close()
	c := ts.Client()
	c.Transport = &fixedURLTransport{base: ts.URL, inner: c.Transport}

	m := &IP2Location{Token: "tok", client: c}
	if err := m.DownloadDb("DB11LITEBIN"); err == nil {
		t.Fatal("expected download error")
	}
}

func TestIP2Location_GetDbPath_NotFound(t *testing.T) {
	m := &IP2Location{}
	path, err := m.GetDbPath()
	if err == nil && path == "" {
		t.Fatal("expected error or a path")
	}
}
