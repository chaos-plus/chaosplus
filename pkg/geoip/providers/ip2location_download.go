package providers

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
)

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

	url := "https://www.ip2location.com/download/?token=" + m.Token + "&file=" + code
	slog.Info("ip2location download db url", "url", url)

	target := filepath.Join(cacheDir, code+".zip")
	if err := downloadFile(m.httpClient(), url, target); err != nil {
		slog.Error("ip2location download db error", "code", code, "err", err)
		return "", err
	}
	slog.Info("ip2location download db success", "code", code, "target", target)
	return target, nil
}

func (m *IP2Location) GetDbPath() (string, error) {
	cacheDir, err := workDir("ip2location")
	if err != nil {
		return "", err
	}
	return findLatestFile(cacheDir, ".bin")
}
