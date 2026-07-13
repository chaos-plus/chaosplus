package app

import (
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExampleConfigParses guards config.example.yaml against drift from the
// Config struct: it must load and decode into Config, with representative keys
// (including durations, the redis addrs slice, and the database map) mapping to
// the expected fields.
func TestExampleConfigParses(t *testing.T) {
	v := viper.New()
	v.SetConfigFile("../../config.example.yaml")
	require.NoError(t, v.ReadInConfig())

	var cfg Config
	require.NoError(t, v.Unmarshal(&cfg))

	assert.Equal(t, "chaosplus", cfg.Name)
	assert.Equal(t, 8080, cfg.RestServer.Port)
	assert.Equal(t, 9090, cfg.GrpcServer.Port)
	assert.Equal(t, "info", cfg.Log.Level)

	// Redis + rate limiting (durations decode from strings like "1m").
	require.NotEmpty(t, cfg.Redis.Addrs)
	assert.Equal(t, "127.0.0.1:6379", cfg.Redis.Addrs[0])
	assert.Equal(t, "rl", cfg.RateLimit.Prefix)
	assert.Equal(t, time.Minute, cfg.RateLimit.IP.Period)
	assert.Equal(t, "X-Account-Id", cfg.RateLimit.Account.Header)

	// Database map + Datasource fields.
	require.Contains(t, cfg.Database, "primary")
	primary := cfg.Database["primary"]
	assert.Equal(t, "mysql", primary.Type)
	assert.True(t, primary.Writable)
	assert.Equal(t, 30*time.Minute, primary.ConnMaxLifetime)
}
