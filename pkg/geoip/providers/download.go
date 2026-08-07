package providers

import (
	"archive/zip"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const maxExtractedDatabaseBytes int64 = 1 << 30

// workDir returns the cache directory for geoip databases.
func workDir(parts ...string) (string, error) {
	// Resolution order: GEOIP_CACHE_DIR (hermetic tests/deployments), then
	// XDG_CACHE_HOME (os.UserCacheDir ignores it on macOS), then the user cache
	// directory.
	base := os.Getenv("GEOIP_CACHE_DIR")
	if base == "" {
		base = os.Getenv("XDG_CACHE_HOME")
	}
	if base == "" {
		var err error
		base, err = os.UserCacheDir()
		if err != nil {
			base = os.TempDir()
		}
	}
	dir := filepath.Join(append([]string{base, "geoip"}, parts...)...)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// defaultDownloadClient bounds GeoIP DB downloads so a network hang can't block a
// refresh goroutine indefinitely (http.DefaultClient has no timeout). It is
// assigned once and only ever read, so it is safe for concurrent use; callers
// that need to override it (notably tests) pass their own *http.Client instead.
var defaultDownloadClient = &http.Client{Timeout: 60 * time.Second}

// downloadFile downloads a URL to dest using the given HTTP client.
func downloadFile(client *http.Client, url, dest string) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
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
func downloadVerifiedFile(client *http.Client, url, dest, digest string) error {
	if err := downloadFile(client, url, dest); err != nil {
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
	return unzipFileLimit(src, dest, maxExtractedDatabaseBytes)
}

func unzipFileLimit(src, dest string, maxBytes int64) error {
	if maxBytes <= 0 {
		return fmt.Errorf("database archive size limit must be positive")
	}
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return err
	}
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if strings.HasSuffix(strings.ToLower(f.Name), ".bin") {
			if f.UncompressedSize64 > uint64(maxBytes) {
				return fmt.Errorf("database archive entry exceeds %d bytes", maxBytes)
			}
			rc, err := f.Open()
			if err != nil {
				return err
			}
			outPath := filepath.Join(dest, filepath.Base(f.Name))
			outFile, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
			if err != nil {
				return err
			}
			written, copyErr := io.Copy(outFile, io.LimitReader(rc, maxBytes+1))
			closeErr := errors.Join(outFile.Close(), rc.Close())
			if copyErr != nil || closeErr != nil {
				return errors.Join(copyErr, closeErr, os.Remove(outPath))
			}
			if written > maxBytes {
				return errors.Join(fmt.Errorf("database archive entry exceeds %d bytes", maxBytes), os.Remove(outPath))
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

const defaultGitHubAPIBaseURL = "https://api.github.com"

// getGitHubLatestRelease fetches the latest release for owner/repo.
func getGitHubLatestRelease(client *http.Client, apiBaseURL, owner, repo string) (*githubRelease, error) {
	if apiBaseURL == "" {
		apiBaseURL = defaultGitHubAPIBaseURL
	}
	releaseURL := strings.TrimRight(apiBaseURL, "/") + "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/releases/latest"
	resp, err := client.Get(releaseURL)
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
	// Sort newest-first so the first match below is the most recently
	// downloaded database, not the oldest.
	sort.Slice(entries, func(i, j int) bool {
		fi, _ := entries[i].Info()
		fj, _ := entries[j].Info()
		return fi.ModTime().After(fj.ModTime())
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
