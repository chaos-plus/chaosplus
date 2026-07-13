package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/pkg/configurator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateAndValidateConfig exercises both `config` subcommands against the
// real schema: the generated template must pass strict validation (unknown keys
// would fail) and decode to the documented defaults. This ties generator,
// validator, and Config struct together so none can drift.
func TestGenerateAndValidateConfig(t *testing.T) {
	data, err := configurator.GenerateYAML(&Config{})
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, data, 0o644))

	var cfg Config
	require.NoError(t, configurator.LoadStrict(path, &cfg), "generated config must be valid")

	assert.Equal(t, "UTC", cfg.Timezone)
	assert.Equal(t, 3600, cfg.WorkerLease)
	assert.Equal(t, 8080, cfg.RestServer.Port)
	assert.Equal(t, 9090, cfg.GrpcServer.Port)
	assert.Equal(t, "info", cfg.Log.Level)
	assert.Equal(t, "rl", cfg.RateLimit.Prefix)
	assert.Equal(t, time.Minute, cfg.RateLimit.IP.Period)
	assert.Equal(t, "X-Account-Id", cfg.RateLimit.Account.Header)

	require.Len(t, cfg.Database, 1)
	for _, ds := range cfg.Database {
		assert.Equal(t, "mysql", ds.Type)
		assert.True(t, ds.Writable)
		assert.Equal(t, 30*time.Minute, ds.ConnMaxLifetime)
	}
}
