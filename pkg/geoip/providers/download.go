package providers

import (
	"archive/zip"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// workDir returns the cache directory for geoip databases.
func workDir(parts ...string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(append([]string{base, "geoip"}, parts...)...)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

// httpClient bounds GeoIP DB downloads so a network hang can't block the goroutine
// indefinitely (http.DefaultClient has no timeout).
var httpClient = &http.Client{Timeout: 60 * time.Second}

// downloadFile downloads a URL to the specified path.
func downloadFile(url, dest string) error {
	resp, err := httpClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}

// verifyFileDigest checks that the file at path hashes to the expected digest.
//
// digest is GitHub's "<algo>:<hex>" form (only "sha256" is supported, which is
// what GitHub emits today). The comparison is constant-time to avoid leaking
// timing information about how much of the hash matched. An empty digest is a
// caller error and must be handled before calling this.
func verifyFileDigest(path, digest string) error {
	algo, want, ok := strings.Cut(digest, ":")
	if !ok {
		return fmt.Errorf("malformed digest %q", digest)
	}
	if !strings.EqualFold(algo, "sha256") {
		return fmt.Errorf("unsupported digest algorithm %q", algo)
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))

	if subtle.ConstantTimeCompare([]byte(strings.ToLower(got)), []byte(strings.ToLower(want))) != 1 {
		return fmt.Errorf("checksum mismatch: want %s got %s", want, got)
	}
	return nil
}

// downloadVerifiedFile downloads url to dest and, when a non-empty digest is
// provided, verifies the downloaded bytes against it. On a checksum mismatch
// the partial/poisoned file is removed and an error is returned, so a
// compromised release asset is never left on disk for the DB loader to pick up.
//
// When digest is empty (older releases without GitHub asset digests, or
// providers whose source publishes no checksum), the download is accepted but
// the absence of integrity verification is logged at WARN so the operational
// risk is visible. This is a deliberate availability/security trade-off: we
// have no trustworthy checksum to compare against in that case.
func downloadVerifiedFile(url, dest, digest string) error {
	if err := downloadFile(url, dest); err != nil {
		return err
	}
	if digest == "" {
		slog.Warn("geoip asset has no published checksum; integrity not verified",
			"url", url, "dest", dest)
		return nil
	}
	if err := verifyFileDigest(dest, digest); err != nil {
		// Remove the unverified file so it can't be used downstream.
		if rmErr := os.Remove(dest); rmErr != nil {
			slog.Error("failed to remove file after checksum mismatch", "dest", dest, "error", rmErr)
		}
		return fmt.Errorf("integrity check failed for %s: %w", dest, err)
	}
	return nil
}

// unzipFile extracts the first .bin file from a zip archive to the destination directory.
func unzipFile(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()
	if err := os.MkdirAll(dest, 0755); err != nil {
		return err
	}
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if strings.HasSuffix(strings.ToLower(f.Name), ".bin") {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			defer rc.Close()
			outPath := filepath.Join(dest, filepath.Base(f.Name))
			outFile, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
			if err != nil {
				return err
			}
			defer outFile.Close()
			if _, err := io.Copy(outFile, rc); err != nil {
				return err
			}
			return nil
		}
	}
	return os.ErrNotExist
}

// githubRelease represents minimal GitHub release JSON.
type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	// Digest is GitHub's server-computed content digest, formatted as
	// "<algo>:<hex>" (e.g. "sha256:dca4ada7..."). Present on releases created
	// after GitHub rolled out asset digests; empty otherwise. We use it as the
	// integrity source because the P3TERX/GeoLite.mmdb mirror publishes no
	// separate .sha256 sidecar assets — the digest travels in the API record.
	Digest string `json:"digest"`
}

// getGitHubLatestRelease fetches the latest release for owner/repo.
func getGitHubLatestRelease(owner, repo string) (*githubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api status %d", resp.StatusCode)
	}
	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// findLatestFile scans subdirectories and returns the most recently modified file matching suffix.
func findLatestFile(dir string, suffix string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	sort.Slice(entries, func(i, j int) bool {
		fi, _ := entries[i].Info()
		fj, _ := entries[j].Info()
		return fi.ModTime().Before(fj.ModTime())
	})
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			if strings.HasSuffix(strings.ToLower(f.Name()), suffix) {
				return filepath.Join(dir, e.Name(), f.Name()), nil
			}
		}
	}
	return "", fmt.Errorf("no db found")
}

// timeNow returns the current time (replaceable in tests).
var timeNow = time.Now
