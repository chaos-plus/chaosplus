package app

import (
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupDebugDatabaseRules(t *testing.T) {
	disabled := &App{}
	disabled.SetupDebug()
	assert.Empty(t, disabled.cfg.Database)

	existing := &App{cfg: Config{Debug: true, Database: map[string]bunx.Datasource{"primary": {Type: "sqlite", Dsn: ":memory:"}}}}
	existing.SetupDebug()
	require.Len(t, existing.cfg.Database, 1)
	assert.Contains(t, existing.cfg.Database, "primary")

	debug := &App{cfg: Config{Debug: true}}
	debug.SetupDebug()
	require.Contains(t, debug.cfg.Database, "debug")
	assert.Equal(t, "sqlite", debug.cfg.Database["debug"].Type)
}
