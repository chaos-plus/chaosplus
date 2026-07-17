package configurator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseExplicitConfigFile(t *testing.T) {
	type config struct {
		Name string `mapstructure:"name" default:"default"`
	}
	path := filepath.Join(t.TempDir(), "runtime.yaml")
	require.NoError(t, os.WriteFile(path, []byte("name: from-file\n"), 0o600))
	flags := New()
	flags.UseConfigFileArgDefault()
	var cfg config
	require.NoError(t, flags.Parse(&cfg, "--config", path))
	assert.Equal(t, "from-file", cfg.Name)

	missing := New()
	missing.UseConfigFileArgDefault()
	err := missing.Parse(&config{}, "--config", filepath.Join(t.TempDir(), "missing.yaml"))
	assert.Error(t, err)
}

func TestParseCommaSeparatedStringSliceFromEnvironment(t *testing.T) {
	t.Setenv("ALLOWED_URLS", "https://app.example.com,https://app.example.com/")
	type config struct {
		AllowedURLs []string `mapstructure:"allowed_urls"`
	}

	var cfg config
	require.NoError(t, New().Parse(&cfg, "--"))
	assert.Equal(t, []string{
		"https://app.example.com",
		"https://app.example.com/",
	}, cfg.AllowedURLs)
}

func TestParseEnvironmentPrefixExactlyOnce(t *testing.T) {
	t.Setenv("APP_APPLE_NAME", "configured")
	type config struct {
		AppleName string `mapstructure:"apple_name"`
	}

	flags := New()
	flags.UseEnvPrefix("APP")
	var cfg config
	require.NoError(t, flags.Parse(&cfg, "--"))
	assert.Equal(t, "configured", cfg.AppleName)
}
