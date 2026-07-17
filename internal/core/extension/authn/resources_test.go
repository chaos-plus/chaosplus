package authn

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRuntimeResourcesRoundTripAndResolve(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "resources.json")
	want := RuntimeResources{Version: RuntimeResourcesVersion, ProjectID: "project", ClientID: "client"}
	require.NoError(t, WriteRuntimeResources(path, want))
	got, err := LoadRuntimeResources(path)
	require.NoError(t, err)
	assert.Equal(t, want, got)

	keyPath := filepath.Join(t.TempDir(), "session-key")
	require.NoError(t, os.WriteFile(keyPath, []byte("0123456789abcdef0123456789abcdef\n"), 0o600))
	cfg, err := ResolveConfig(Config{ResourcesFile: path, Web: WebConfig{EncryptionKeyFile: keyPath}})
	require.NoError(t, err)
	assert.Equal(t, []string{"project"}, cfg.Audience)
	assert.Equal(t, "client", cfg.Web.ClientID)
	assert.Equal(t, "0123456789abcdef0123456789abcdef", cfg.Web.EncryptionKey)
	info, err := os.Stat(path)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o640), info.Mode().Perm())
	}

	unchanged, err := ResolveConfig(Config{})
	require.NoError(t, err)
	assert.Empty(t, unchanged.Audience)
	matching, err := ResolveConfig(Config{ResourcesFile: path, Audience: []string{"project"}, Web: WebConfig{ClientID: "client"}})
	require.NoError(t, err)
	assert.Equal(t, "client", matching.Web.ClientID)

	_, err = ResolveConfig(Config{ResourcesFile: path, Audience: []string{"other"}})
	assert.ErrorContains(t, err, "audience conflicts")
	_, err = ResolveConfig(Config{ResourcesFile: path, Web: WebConfig{ClientID: "other"}})
	assert.ErrorContains(t, err, "client_id conflicts")
	_, err = ResolveConfig(Config{Web: WebConfig{EncryptionKey: "inline", EncryptionKeyFile: keyPath}})
	assert.ErrorContains(t, err, "mutually exclusive")
	_, err = ResolveConfig(Config{ResourcesFile: filepath.Join(t.TempDir(), "missing")})
	assert.ErrorContains(t, err, "open authn resources")
}

func TestRuntimeResourcesRejectInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"unknown":  `{"version":1,"project_id":"p","client_id":"c","extra":true}`,
		"missing":  `{"version":1,"project_id":"p"}`,
		"version":  `{"version":2,"project_id":"p","client_id":"c"}`,
		"trailing": `{"version":1,"project_id":"p","client_id":"c"} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name)
			require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
			_, err := LoadRuntimeResources(path)
			assert.Error(t, err)
		})
	}
	valid := RuntimeResources{Version: 1, ProjectID: "p", ClientID: "c"}
	assert.Error(t, WriteRuntimeResources("", valid))
	assert.Error(t, WriteRuntimeResources(filepath.Join(dir, "invalid.json"), RuntimeResources{}))
	parentFile := filepath.Join(dir, "parent-file")
	require.NoError(t, os.WriteFile(parentFile, []byte("x"), 0o600))
	assert.Error(t, WriteRuntimeResources(filepath.Join(parentFile, "resources.json"), valid))
}
