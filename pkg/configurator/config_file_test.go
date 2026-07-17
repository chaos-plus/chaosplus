package configurator

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type runtimeConfigSample struct {
	Name    string        `mapstructure:"name" default:"default"`
	Timeout time.Duration `mapstructure:"timeout" default:"5s"`
}

func TestParseAppliesExplicitConfigFile(t *testing.T) {
	path := writeTemp(t, "name: from-file\ntimeout: 12s\n")
	flagger := New()
	flagger.UseConfigFileArgDefault()
	var cfg runtimeConfigSample
	require.NoError(t, flagger.Parse(&cfg, "--config", path))
	assert.Equal(t, "from-file", cfg.Name)
	assert.Equal(t, 12*time.Second, cfg.Timeout)
}

func TestParseAppliesShortConfigFlag(t *testing.T) {
	path := writeTemp(t, "name: short-flag\n")
	flagger := New()
	flagger.UseConfigFileArgDefault()
	var cfg runtimeConfigSample
	require.NoError(t, flagger.Parse(&cfg, "-c", path))
	assert.Equal(t, "short-flag", cfg.Name)
}

func TestParseExplicitMissingConfigFails(t *testing.T) {
	flagger := New()
	flagger.UseConfigFileArgDefault()
	var cfg runtimeConfigSample
	err := flagger.Parse(&cfg, "--config", filepath.Join(t.TempDir(), "missing.yaml"))
	assert.Error(t, err)
}
