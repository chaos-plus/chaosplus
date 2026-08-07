package providers

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
)

const defaultIP2LocationDownloadBaseURL = "https://www.ip2location.com/download/"

func (m *IP2Location) DownloadDb(codes ...string) error {
	cacheDir, err := workDir("ip2location")
	if err != nil {
		return err
	}
	if len(codes) == 0 {
		codes = []string{"DB11LITEBIN"}
	}
	for _, c := range codes {
		target, err := m.downloadDb(c)
		if err != nil {
			slog.Error("ip2location download db error", "code", c, "err", err)
			return err
		}
		fi, err := os.Stat(target)
		if err != nil {
			slog.Error("ip2location check db error", "code", c, "err", err, "target", target)
			return err
		}
		if err := unzipFile(target, filepath.Join(cacheDir, fi.ModTime().Format("20060102150405"))); err != nil {
			return err
		}
	}
	return nil
}

func (m *IP2Location) downloadDb(code string) (string, error) {
	if code == "" {
		return "", errors.New("code is empty")
	}
	cacheDir, err := workDir("ip2location")
	if err != nil {
		return "", err
	}

	slog.Info("ip2location download db use cache dir", "cacheDir", cacheDir)

	downloadURL, err := ip2locationDownloadURL(m.DownloadBaseURL, m.Token, code)
	if err != nil {
		return "", err
	}

	target := filepath.Join(cacheDir, code+".zip")
	if err := downloadFile(defaultDownloadClient, downloadURL, target); err != nil {
		slog.Error("ip2location download db error", "code", code, "err", err)
		return "", err
	}
	slog.Info("ip2location download db success", "code", code, "target", target)
	return target, nil
}

func ip2locationDownloadURL(baseURL, token, code string) (string, error) {
	if baseURL == "" {
		baseURL = defaultIP2LocationDownloadBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse ip2location download URL: %w", err)
	}
	query := parsed.Query()
	query.Set("token", token)
	query.Set("file", code)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func (m *IP2Location) GetDbPath() (string, error) {
	cacheDir, err := workDir("ip2location")
	if err != nil {
		return "", err
	}
	return findLatestFile(cacheDir, ".bin")
}
