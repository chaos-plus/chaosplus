package providers

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIP2RegionDatabaseDiscoveryAndInvalidDatabase(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("LOCALAPPDATA", cache)
	t.Setenv("XDG_CACHE_HOME", cache)
	cacheDir, err := workDir("ip2region")
	require.NoError(t, err)

	dataPath := filepath.Join(cacheDir, "repo", "data", ip2regionDatabase)
	require.NoError(t, os.MkdirAll(filepath.Dir(dataPath), 0o755))
	require.NoError(t, os.WriteFile(dataPath, []byte("invalid-xdb"), 0o600))
	provider := &IP2Region{}
	path, err := provider.GetDbPath()
	require.NoError(t, err)
	assert.Equal(t, dataPath, path)
	_, err = provider.GetIpInfo("8.8.8.8")
	assert.Error(t, err)

	builtPath := filepath.Join(cacheDir, "repo", "maker", "go", ip2regionDatabase)
	require.NoError(t, os.MkdirAll(filepath.Dir(builtPath), 0o755))
	require.NoError(t, os.WriteFile(builtPath, []byte("built-xdb"), 0o600))
	path, err = provider.GetDbPath()
	require.NoError(t, err)
	assert.Equal(t, builtPath, path)
}

func TestIP2RegionDownloadUpdatesRealLocalRepository(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("LOCALAPPDATA", cache)
	t.Setenv("XDG_CACHE_HOME", cache)
	source := t.TempDir()
	runGit(t, source, "init")
	runGit(t, source, "config", "user.email", "test@chaosplus.local")
	runGit(t, source, "config", "user.name", "Chaosplus Test")
	require.NoError(t, os.WriteFile(filepath.Join(source, "README.md"), []byte("local ip2region source"), 0o600))
	runGit(t, source, "add", "README.md")
	runGit(t, source, "commit", "-m", "initial")

	cacheDir, err := workDir("ip2region")
	require.NoError(t, err)
	repoPath := filepath.Join(cacheDir, "repo")
	runGit(t, "", "clone", "--quiet", source, repoPath)
	provider := &IP2Region{}
	require.NoError(t, provider.DownloadDb())

	makerPath := filepath.Join(repoPath, "maker", "go")
	require.NoError(t, os.MkdirAll(makerPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(makerPath, "go.mod"), []byte("module example.test/ip2region-maker\n\ngo 1.26\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(makerPath, "main.go"), []byte("package main\nfunc main() { invalid syntax }\n"), 0o600))
	require.NoError(t, provider.DownloadDb(), "a maker failure must not discard an existing database checkout")

	require.NoError(t, os.RemoveAll(source))
	assert.Error(t, provider.DownloadDb(), "a failed repository update must be reported")
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	require.NoErrorf(t, err, "git %v: %s", args, output)
}
