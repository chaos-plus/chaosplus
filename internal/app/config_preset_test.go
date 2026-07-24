package app_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chaos-plus/chaosplus/internal/app"
	"github.com/chaos-plus/chaosplus/pkg/configurator"
)

func TestComposeConfigPreset(t *testing.T) {
	data, err := app.ConfigPreset("compose")
	require.NoError(t, err)
	flags := configurator.New()
	flags.UseDefaultConfig(data, "yaml")
	var cfg app.Config
	require.NoError(t, flags.Parse(&cfg, "--"))
	assert.Equal(t, []string{"redis:6379"}, cfg.Redis.Addrs)
	assert.Equal(t, "/run/secrets/chaosplus_runtime_dsn", cfg.Database["primary"].DsnFile)
	assert.True(t, cfg.Authn.Web.DirectLoginEnabled)
	assert.Equal(t, "/var/lib/chaosplus/runtime/login-client.pat", cfg.Authn.Web.LoginClientTokenFile)
	assert.True(t, cfg.Migrations.Auto)
	assert.True(t, cfg.Bootstrap.Auto)
	assert.True(t, cfg.Bootstrap.Zitadel.Enabled)
	assert.Error(t, func() error { _, err := app.ConfigPreset("missing"); return err }())
}
