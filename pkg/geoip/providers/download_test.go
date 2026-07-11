package providers

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sha256Digest returns the GitHub-style "sha256:<hex>" digest of content.
func sha256Digest(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// withHTTPClient swaps the package httpClient for the duration of a test.
func withHTTPClient(t *testing.T, c *http.Client) {
	t.Helper()
	saved := httpClient
	httpClient = c
	t.Cleanup(func() { httpClient = saved })
}

func TestWorkDir_CreatesDir(t *testing.T) {
	dir, err := workDir("unit-test-sub")
	if err != nil {
		t.Fatalf("workDir: %v", err)
	}
	if dir == "" {
		t.Fatal("expected non-empty dir")
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		t.Fatalf("expected directory at %s: %v", dir, err)
	}
	if filepath.Base(dir) != "unit-test-sub" {
		t.Fatalf("unexpected dir tail: %s", dir)
	}
}

func TestDownloadFile_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello-db"))
	}))
	defer ts.Close()
	withHTTPClient(t, ts.Client())

	dest := filepath.Join(t.TempDir(), "nested", "out.bin")
	if err := downloadFile(ts.URL, dest); err != nil {
		t.Fatalf("downloadFile: %v", err)
	}
	b, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(b) != "hello-db" {
		t.Fatalf("unexpected content: %q", b)
	}
}

func TestDownloadFile_Non200(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()
	withHTTPClient(t, ts.Client())

	err := downloadFile(ts.URL, filepath.Join(t.TempDir(), "out.bin"))
	if err == nil || !strings.Contains(err.Error(), "http 404") {
		t.Fatalf("expected http 404 error, got %v", err)
	}
}

func TestDownloadFile_TransportError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := ts.URL
	ts.Close()
	withHTTPClient(t, ts.Client())

	if err := downloadFile(url, filepath.Join(t.TempDir(), "out.bin")); err == nil {
		t.Fatal("expected transport error")
	}
}

func makeZip(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	zipPath := filepath.Join(dir, "archive.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create entry: %v", err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("zip write: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return zipPath
}

func TestUnzipFile_ExtractsBin(t *testing.T) {
	dir := t.TempDir()
	zipPath := makeZip(t, dir, map[string]string{
		"readme.txt":   "ignore me",
		"database.BIN": "binary-content",
	})
	dest := filepath.Join(dir, "out")
	if err := unzipFile(zipPath, dest); err != nil {
		t.Fatalf("unzipFile: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "database.BIN"))
	if err != nil {
		t.Fatalf("read extracted: %v", err)
	}
	if string(got) != "binary-content" {
		t.Fatalf("unexpected content: %q", got)
	}
}

func TestUnzipFile_NoBin(t *testing.T) {
	dir := t.TempDir()
	zipPath := makeZip(t, dir, map[string]string{"readme.txt": "nope"})
	err := unzipFile(zipPath, filepath.Join(dir, "out"))
	if err != os.ErrNotExist {
		t.Fatalf("expected ErrNotExist, got %v", err)
	}
}

func TestUnzipFile_BadArchive(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.zip")
	if err := os.WriteFile(bad, []byte("not a zip"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := unzipFile(bad, filepath.Join(dir, "out")); err == nil {
		t.Fatal("expected error opening invalid zip")
	}
}

func TestGetGitHubLatestRelease_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"tag_name": "v1.2.3",
			"assets": [
				{"name": "GeoLite2-City.mmdb", "browser_download_url": "http://example/db"}
			]
		}`))
	}))
	defer ts.Close()
	withHTTPClient(t, ts.Client())

	// Point at the test server by overriding via a transport that ignores the URL host.
	httpClient.Transport = &fixedURLTransport{base: ts.URL}
	rel, err := getGitHubLatestRelease("owner", "repo")
	if err != nil {
		t.Fatalf("getGitHubLatestRelease: %v", err)
	}
	if rel.TagName != "v1.2.3" {
		t.Fatalf("unexpected tag: %s", rel.TagName)
	}
	if len(rel.Assets) != 1 || rel.Assets[0].Name != "GeoLite2-City.mmdb" {
		t.Fatalf("unexpected assets: %+v", rel.Assets)
	}
}

func TestGetGitHubLatestRelease_Non200(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer ts.Close()
	c := ts.Client()
	c.Transport = &fixedURLTransport{base: ts.URL, inner: c.Transport}
	withHTTPClient(t, c)

	if _, err := getGitHubLatestRelease("o", "r"); err == nil ||
		!strings.Contains(err.Error(), "github api status") {
		t.Fatalf("expected github api status error, got %v", err)
	}
}

func TestGetGitHubLatestRelease_BadJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{bad`))
	}))
	defer ts.Close()
	c := ts.Client()
	c.Transport = &fixedURLTransport{base: ts.URL, inner: c.Transport}
	withHTTPClient(t, c)

	if _, err := getGitHubLatestRelease("o", "r"); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestFindLatestFile(t *testing.T) {
	root := t.TempDir()
	// older subdir
	older := filepath.Join(root, "old")
	newer := filepath.Join(root, "new")
	if err := os.MkdirAll(older, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(newer, 0755); err != nil {
		t.Fatal(err)
	}
	oldFile := filepath.Join(older, "a.mmdb")
	newFile := filepath.Join(newer, "b.mmdb")
	if err := os.WriteFile(oldFile, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newFile, []byte("y"), 0644); err != nil {
		t.Fatal(err)
	}
	// Make "new" subdir modified more recently.
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(older, past, past); err != nil {
		t.Fatal(err)
	}

	got, err := findLatestFile(root, ".mmdb")
	if err != nil {
		t.Fatalf("findLatestFile: %v", err)
	}
	if !strings.HasSuffix(got, ".mmdb") {
		t.Fatalf("expected an .mmdb path, got %s", got)
	}
}

func TestFindLatestFile_NoMatch(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "x.txt"), []byte("z"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := findLatestFile(root, ".mmdb"); err == nil {
		t.Fatal("expected no db found error")
	}
}

func TestFindLatestFile_MissingDir(t *testing.T) {
	if _, err := findLatestFile(filepath.Join(t.TempDir(), "does-not-exist"), ".mmdb"); err == nil {
		t.Fatal("expected error for missing dir")
	}
}

func TestVerifyFileDigest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.bin")
	content := "geoip-db-bytes"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	t.Run("match", func(t *testing.T) {
		if err := verifyFileDigest(path, sha256Digest(content)); err != nil {
			t.Fatalf("expected match, got %v", err)
		}
	})

	t.Run("mismatch", func(t *testing.T) {
		err := verifyFileDigest(path, sha256Digest("other-bytes"))
		if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
			t.Fatalf("expected checksum mismatch, got %v", err)
		}
	})

	t.Run("uppercase hex still matches", func(t *testing.T) {
		if err := verifyFileDigest(path, strings.ToUpper(sha256Digest(content))); err != nil {
			t.Fatalf("expected case-insensitive match, got %v", err)
		}
	})

	t.Run("malformed digest", func(t *testing.T) {
		if err := verifyFileDigest(path, "deadbeef"); err == nil {
			t.Fatal("expected malformed digest error")
		}
	})

	t.Run("unsupported algorithm", func(t *testing.T) {
		err := verifyFileDigest(path, "md5:abcdef")
		if err == nil || !strings.Contains(err.Error(), "unsupported digest algorithm") {
			t.Fatalf("expected unsupported algorithm error, got %v", err)
		}
	})
}

func TestDownloadVerifiedFile_ChecksumMatch(t *testing.T) {
	content := "verified-db"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(content))
	}))
	defer ts.Close()
	withHTTPClient(t, ts.Client())

	dest := filepath.Join(t.TempDir(), "out.mmdb")
	if err := downloadVerifiedFile(ts.URL, dest, sha256Digest(content)); err != nil {
		t.Fatalf("downloadVerifiedFile: %v", err)
	}
	b, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("expected accepted file on disk: %v", err)
	}
	if string(b) != content {
		t.Fatalf("unexpected content: %q", b)
	}
}

func TestDownloadVerifiedFile_ChecksumMismatchDeletes(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serve content that does NOT match the digest we'll pass — simulating a
		// compromised/poisoned release asset.
		_, _ = w.Write([]byte("malicious-payload"))
	}))
	defer ts.Close()
	withHTTPClient(t, ts.Client())

	dest := filepath.Join(t.TempDir(), "out.mmdb")
	err := downloadVerifiedFile(ts.URL, dest, sha256Digest("the-legit-bytes"))
	if err == nil || !strings.Contains(err.Error(), "integrity check failed") {
		t.Fatalf("expected integrity check failure, got %v", err)
	}
	// The poisoned file must be deleted, not left for the DB loader.
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("expected file to be deleted on mismatch, stat err: %v", statErr)
	}
}

func TestDownloadVerifiedFile_NoDigestAccepts(t *testing.T) {
	content := "unverified-db"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(content))
	}))
	defer ts.Close()
	withHTTPClient(t, ts.Client())

	// Empty digest → accepted with a WARN log, file kept.
	dest := filepath.Join(t.TempDir(), "out.mmdb")
	if err := downloadVerifiedFile(ts.URL, dest, ""); err != nil {
		t.Fatalf("downloadVerifiedFile with no digest: %v", err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("expected file kept when no digest published: %v", err)
	}
}

// fixedURLTransport rewrites every request to point at base, preserving the path,
// so provider code that builds api.github.com URLs hits the test server instead.
type fixedURLTransport struct {
	base  string
	inner http.RoundTripper
}

func (t *fixedURLTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	host := strings.TrimPrefix(t.base, "http://")
	host = strings.TrimPrefix(host, "https://")
	req.URL.Scheme = "http"
	req.URL.Host = host
	inner := t.inner
	if inner == nil {
		inner = http.DefaultTransport
	}
	return inner.RoundTrip(req)
}
