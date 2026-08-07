package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chaos-plus/chaosplus/pkg/geoip"
	"github.com/stretchr/testify/assert"
)

func TestGeolite2_Configure(t *testing.T) {
	m := &Geolite2{}

	var c geoip.GeoIpConfig
	c.Geolite2.Owner = "o"
	c.Geolite2.Repo = "r"
	c.Geolite2.Db = "d"
	c.Geolite2.APIBaseURL = "http://127.0.0.1:9000/"
	m.Configure(c)

	assert.Equal(t, "o", m.Owner)
	assert.Equal(t, "r", m.Repo)
	assert.Equal(t, "d", m.Db)
	assert.Equal(t, "http://127.0.0.1:9000", m.APIBaseURL)
}

func TestGeolite2CancelledStartAndDefaultClient(t *testing.T) {
	provider := &Geolite2{}
	assert.NotNil(t, defaultDownloadClient)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.NoError(t, provider.Start(ctx))
	assert.NoError(t, provider.Stop(t.Context()))
}

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
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Release JSON that does NOT contain the requested asset name.
		_, _ = w.Write([]byte(`{"tag_name":"v9.9","assets":[{"name":"other.mmdb","browser_download_url":"http://x/y"}]}`))
	}))
	defer ts.Close()
	m := &Geolite2{Owner: "o", Repo: "r", Db: "GeoLite2-City.mmdb", APIBaseURL: ts.URL}
	err := m.DownloadDb("GeoLite2-City.mmdb")
	if err == nil || !strings.Contains(err.Error(), "not found in release") {
		t.Fatalf("expected asset-not-found error, got %v", err)
	}
}

func TestGeolite2_DownloadDb_ReleaseError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()
	m := &Geolite2{Owner: "o", Repo: "r", APIBaseURL: ts.URL}
	if err := m.DownloadDb("GeoLite2-City.mmdb"); err == nil {
		t.Fatal("expected error when release fetch fails")
	}
}

func TestGeolite2_DownloadDb_NoArgsEmptyDb(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v1","assets":[]}`))
	}))
	defer ts.Close()
	m := &Geolite2{Owner: "o", Repo: "r", APIBaseURL: ts.URL}
	err := m.DownloadDb()
	if err == nil || !strings.Contains(err.Error(), "GeoLite2-City.mmdb not found") {
		t.Fatalf("expected the default database to enter the download flow, got %v", err)
	}
	if m.Db != "GeoLite2-City.mmdb" {
		t.Fatalf("expected default Db name to be set, got %q", m.Db)
	}
}

func TestGeolite2_DownloadDb_NoArgsWithDb(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// When m.Db is preset, no-arg DownloadDb downloads names=[m.Db]; respond with
		// a release missing that asset to exercise the loop + asset-not-found path.
		_, _ = w.Write([]byte(`{"tag_name":"v1","assets":[]}`))
	}))
	defer ts.Close()
	m := &Geolite2{Owner: "o", Repo: "r", Db: "Custom.mmdb", APIBaseURL: ts.URL}
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

func TestGeolite2_GetIpInfo_InvalidDatabase(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("LOCALAPPDATA", cache)
	t.Setenv("XDG_CACHE_HOME", cache)
	cacheDir, err := workDir("lite2")
	if err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(cacheDir, "v1", "GeoLite2-City.mmdb")
	if err := os.MkdirAll(filepath.Dir(database), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(database, []byte("not-an-mmdb"), 0o600); err != nil {
		t.Fatal(err)
	}
	provider := &Geolite2{}
	_, err = provider.GetIpInfo("8.8.8.8")
	assert.Error(t, err)
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
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer ts.Close()
	m := &IP2Location{Token: "tok", DownloadBaseURL: ts.URL}
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

func TestIP2Location_InvalidDatabaseAndCancelledStart(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("LOCALAPPDATA", cache)
	t.Setenv("XDG_CACHE_HOME", cache)
	cacheDir, err := workDir("ip2location")
	if err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(cacheDir, "v1", "IP2LOCATION.BIN")
	if err := os.MkdirAll(filepath.Dir(database), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(database, []byte("not-an-ip2location-database"), 0o600); err != nil {
		t.Fatal(err)
	}
	provider := &IP2Location{Token: "configured"}
	_, err = provider.GetIpInfo("8.8.8.8")
	assert.Error(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	assert.NoError(t, provider.Start(ctx))
	assert.NoError(t, provider.Stop(t.Context()))
}
