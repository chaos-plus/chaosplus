package providers

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

func (m *Geolite2) DownloadDb(names ...string) error {
	if len(names) == 0 {
		if m.Db == "" {
			m.Db = "GeoLite2-City.mmdb"
		} else {
			names = []string{m.Db}
		}
	}
	for _, name := range names {
		err := m.downloadDb(name)
		if err != nil {
			slog.Error("failed to download geolite2 db", "owner", m.Owner, "repo", m.Repo, "name", name, "error", err)
			return err
		}
	}
	return nil
}

func (m *Geolite2) downloadDb(name string) error {
	cacheDir, err := workDir("lite2")
	if err != nil {
		return err
	}
	if m.Owner == "" || m.Repo == "" {
		m.Owner = "P3TERX"
		m.Repo = "GeoLite.mmdb"
	}

	slog.Info("download geolite2 db use cache dir", "cacheDir", cacheDir)

	latestRelease, err := getGitHubLatestRelease(m.httpClient(), m.Owner, m.Repo)
	if err != nil {
		slog.Error("failed to get latest release", "owner", m.Owner, "repo", m.Repo, "error", err)
		return err
	}
	slog.Info("checked latest release", "owner", m.Owner, "repo", m.Repo, "latestReleaseName", latestRelease.TagName)

	saveDir := filepath.Join(cacheDir, latestRelease.TagName)
	os.MkdirAll(saveDir, 0755)

	var assetURL, assetDigest string
	for _, asset := range latestRelease.Assets {
		if asset.Name == name {
			assetURL = asset.BrowserDownloadURL
			assetDigest = asset.Digest
			break
		}
	}
	if assetURL == "" {
		return fmt.Errorf("asset %s not found in release %s", name, latestRelease.TagName)
	}

	// Verify the download against GitHub's published asset digest before
	// accepting it; on mismatch downloadVerifiedFile deletes the file and errors
	// so a poisoned release asset is never used. If the release predates GitHub
	// asset digests, it logs a WARN and accepts the file (no checksum to check).
	dest := filepath.Join(saveDir, name)
	if err := downloadVerifiedFile(m.httpClient(), assetURL, dest, assetDigest); err != nil {
		slog.Error("failed to download", "owner", m.Owner, "repo", m.Repo, "name", name, "error", err)
		return err
	}
	slog.Info("downloaded", "owner", m.Owner, "repo", m.Repo, "saveDir", saveDir, "name", name)
	return nil
}

func (m *Geolite2) GetDbPath() (string, error) {
	cacheDir, err := workDir("lite2")
	if err != nil {
		return "", err
	}
	return findLatestFile(cacheDir, ".mmdb")
}
