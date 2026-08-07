package providers

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
)

func (m *IP2Region) DownloadDb() error {
	cacheDir, err := workDir("ip2region")
	if err != nil {
		return err
	}

	repoURL := "https://github.com/lionsoul2014/ip2region"
	repoPath := filepath.Join(cacheDir, "repo")

	if _, err := os.Stat(filepath.Join(repoPath, ".git")); os.IsNotExist(err) {
		slog.Info("Cloning ip2region repo...")
		cmd := exec.Command("git", "clone", repoURL, repoPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			slog.Error("git clone failed", "err", err)
			return err
		}
	} else {
		slog.Info("Pulling ip2region repo...")
		cmd := exec.Command("git", "-C", repoPath, "pull")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			slog.Error("git pull failed", "err", err)
			return err
		}
	}

	// build the xdb file if maker is available
	makerPath := filepath.Join(repoPath, "maker", "go")
	if _, err := os.Stat(makerPath); err == nil {
		cmd := exec.Command("go", "run", ".", "gen")
		cmd.Dir = makerPath
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			slog.Warn("ip2region maker build failed", "err", err)
		}
	}

	return nil
}

func (m *IP2Region) GetDbPath() (string, error) {
	cacheDir, err := workDir("ip2region")
	if err != nil {
		return "", err
	}

	// try built xdb first
	built := filepath.Join(cacheDir, "repo", "maker", "go", ip2regionDatabase)
	if fi, err := os.Stat(built); err == nil && !fi.IsDir() {
		return built, nil
	}

	// fallback to data dir
	data := filepath.Join(cacheDir, "repo", "data", ip2regionDatabase)
	if fi, err := os.Stat(data); err == nil && !fi.IsDir() {
		return data, nil
	}

	return "", fmt.Errorf("ip2region db not found")
}
