package configurator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
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

func TestParseEmbeddedDefaultsAllowFileAndEnvironmentOverrides(t *testing.T) {
	path := writeTemp(t, "name: from-file\ntimeout: 12s\n")
	t.Setenv("TIMEOUT", "18s")
	flagger := New()
	flagger.UseConfigFileArgDefault()
	flagger.UseDefaultConfig([]byte("name: embedded\ntimeout: 7s\n"), "yaml")
	var cfg runtimeConfigSample
	require.NoError(t, flagger.Parse(&cfg, "--config", path))
	assert.Equal(t, "from-file", cfg.Name)
	assert.Equal(t, 18*time.Second, cfg.Timeout)
}

type flagTypeSample struct {
	Enabled bool      `mapstructure:"enabled" default:"true"`
	Count   int       `mapstructure:"count" default:"2"`
	Size    uint      `mapstructure:"size" default:"3"`
	Ratio   float64   `mapstructure:"ratio" default:"1.5"`
	Names   []string  `mapstructure:"names"`
	Counts  []int     `mapstructure:"counts"`
	Ratios  []float64 `mapstructure:"ratios"`
	Flags   []bool    `mapstructure:"flags"`
	Sizes   []uint    `mapstructure:"sizes"`
}

func TestParseAllSupportedFlagTypes(t *testing.T) {
	flags := pflag.NewFlagSet("configurator-test", pflag.ContinueOnError)
	flagger := New()
	flagger.UseFlags(flags)
	var cfg flagTypeSample
	require.NoError(t, flagger.Parse(&cfg,
		"--enabled=false", "--count=7", "--size=8", "--ratio=2.5",
		"--names=alpha,beta", "--counts=4,5", "--ratios=3.5,4.5", "--flags=true,false",
		"--sizes=6,7",
	))
	assert.False(t, cfg.Enabled)
	assert.Equal(t, 7, cfg.Count)
	assert.Equal(t, uint(8), cfg.Size)
	assert.Equal(t, 2.5, cfg.Ratio)
	assert.Equal(t, []string{"alpha", "beta"}, cfg.Names)
	assert.Equal(t, []int{4, 5}, cfg.Counts)
	assert.Equal(t, []float64{3.5, 4.5}, cfg.Ratios)
	assert.Equal(t, []bool{true, false}, cfg.Flags)
	assert.Equal(t, []uint{6, 7}, cfg.Sizes)
}

func TestConfiguratorCustomSourcesAndMapKey(t *testing.T) {
	type tenant struct {
		Enabled bool `mapstructure:"enabled" default:"false"`
	}
	type config struct {
		Name    string            `mapstructure:"name" default:"default"`
		Tenants map[string]tenant `mapstructure:"tenants" mapkey:"<tenant>"`
		Labels  map[string]string `mapstructure:"labels" mapkey:"<label>"`
	}
	directory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(directory, "settings.yaml"), []byte("name: from-path\n"), 0o600))
	t.Setenv("CUSTOM_NAME", "from-environment")

	vip := viper.New()
	flagger := New()
	flagger.UseViper(vip)
	flagger.UseMapKey("tenant")
	assert.Equal(t, "<tenant>", flagger.GetMapkey())
	flagger.UseConfigFileName("settings")
	flagger.UseConfigTypeYaml()
	flagger.UseConfigPath(directory)
	flagger.UseConfigPathDefault()
	flagger.UseEnvPrefix("CUSTOM")
	flagger.UseEnvKeyReplacer(strings.NewReplacer(".", "_"))
	require.NoError(t, flagger.BindEnv("name"))

	var cfg config
	require.NoError(t, flagger.Parse(&cfg, "--tenants.acme.enabled=true", "--labels.region=west"))
	assert.Equal(t, "from-environment", cfg.Name)
	require.Contains(t, cfg.Tenants, "acme")
	assert.True(t, cfg.Tenants["acme"].Enabled)
	assert.Equal(t, "west", cfg.Labels["region"])
	assert.Same(t, vip, flagger.GetViper())
	assert.NotNil(t, flagger.GetFlags())
}

func TestFlagSchemaRejectsUnsupportedTypes(t *testing.T) {
	type unsupported struct {
		Channel chan int `mapstructure:"channel"`
	}
	_, err := parseFlagsMap(&unsupported{}, DefaultMapKey)
	assert.ErrorContains(t, err, "unsupport type")

	type unsupportedSlice struct {
		Channels []chan int `mapstructure:"channels"`
	}
	flags, err := parseFlagsMap(&unsupportedSlice{}, DefaultMapKey)
	require.NoError(t, err)
	for key, value := range flags {
		assert.ErrorContains(t, bindFlags(pflag.NewFlagSet("invalid", pflag.ContinueOnError), key, value), "unsupport slice type")
	}
	assert.False(t, isMapKey("tenants.<tenant>.name", "tenants.name", "<tenant>"))
	assert.False(t, isMapKey("tenants.<tenant>.name", "tenants..name", "<tenant>"))
	assert.False(t, isMapKey("plain.name", "plain.name", "<tenant>"))
}

func TestParsePreservesNonZeroStructValues(t *testing.T) {
	cfg := flagTypeSample{Enabled: true, Count: 9, Size: 10, Ratio: 2.25}
	require.NoError(t, New().Parse(&cfg, "--"))
	assert.True(t, cfg.Enabled)
	assert.Equal(t, 9, cfg.Count)
	assert.Equal(t, uint(10), cfg.Size)
	assert.Equal(t, 2.25, cfg.Ratio)
}

func TestParseReportsFlagAndDefaultConfigurationErrors(t *testing.T) {
	var cfg runtimeConfigSample
	assert.Error(t, New().Parse(&cfg, "--unknown=value"))
	flagger := New()
	flagger.UseDefaultConfig([]byte("name: [invalid"), "yaml")
	assert.Error(t, flagger.Parse(&cfg, "--"))

	type arrayConfig struct {
		Values [2]int `mapstructure:"values"`
	}
	assert.ErrorContains(t, New().Parse(&arrayConfig{}, "--"), "unsupport type")
}

func TestParseProcessArgsPointerMapsAndHiddenFields(t *testing.T) {
	type item struct {
		Enabled bool `mapstructure:"enabled" default:"false"`
	}
	type config struct {
		Name    string           `mapstructure:"name"`
		Hidden  string           `mapstructure:"hidden" hidden:"true"`
		Items   map[string]*item `mapstructure:"items" mapkey:"<item>"`
		private string
	}

	originalArgs := os.Args
	os.Args = []string{"configurator-test", "--items.acme.enabled=true"}
	t.Cleanup(func() { os.Args = originalArgs })
	cfg := config{Name: "preserved", Hidden: "not-a-flag"}
	require.NoError(t, New().Parse(&cfg))
	assert.Equal(t, "preserved", cfg.Name)
	assert.Empty(t, cfg.private)
	require.Contains(t, cfg.Items, "acme")
	assert.True(t, cfg.Items["acme"].Enabled)
}
