package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/app"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func executeCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var output bytes.Buffer
	root, err := newRootCommand(args)
	if err != nil {
		return output.String(), err
	}
	root.SetOut(&output)
	root.SetErr(&output)
	err = root.Execute()
	return output.String(), err
}

func writeMigrationConfig(t *testing.T, dsn string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := fmt.Appendf(nil, "bootstrap:\n  lock_timeout: 1s\n  database:\n    type: sqlite\n    dsn: %q\n", filepath.ToSlash(dsn))
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

func TestConfigGenerateAndValidateCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generated.yaml")
	_, err := executeCommand(t, "config", "generate", "--output", path)
	require.NoError(t, err)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "timezone: UTC")

	_, err = executeCommand(t, "config", "validate", "--config", path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("unknown_key: true\n"), 0o600))
	_, err = executeCommand(t, "config", "validate", "--config", path)
	assert.ErrorContains(t, err, "is invalid")
	_, err = executeCommand(t, "config", "generate", "--output", t.TempDir())
	assert.Error(t, err)
}

func TestMigrationCommandValidation(t *testing.T) {
	_, err := executeCommand(t, "migration", "down")
	assert.Error(t, err)
	_, err = executeCommand(t, "migration", "down-to", "iam", "invalid")
	assert.ErrorContains(t, err, "invalid migration version")
	_, err = executeCommand(t, "migration", "down-to", "iam", "--", "-1")
	assert.ErrorContains(t, err, "invalid migration version")
}

func TestRootHelpUsesCobraWithoutLoadingRuntime(t *testing.T) {
	output, err := executeCommand(t, "--help")
	require.NoError(t, err)
	assert.Contains(t, output, "Usage:")
	assert.Contains(t, output, "migration")
	assert.Contains(t, output, "--config")
}

func TestRootCommandPropagatesStartupFailures(t *testing.T) {
	cfg := app.Config{Migrations: app.Migrations{Auto: true}, Bootstrap: app.BootstrapConfig{
		Database: bunx.Datasource{Type: "unsupported", Dsn: "value"},
	}}
	assert.ErrorContains(t, runServer(cfg), "database migration failed")

	cfg = app.Config{Bootstrap: app.BootstrapConfig{
		Auto: true, Database: bunx.Datasource{Type: "unsupported", Dsn: "value"},
	}}
	assert.ErrorContains(t, runServer(cfg), "deployment provisioning failed")

	cfg = app.Config{Timezone: "Invalid/Timezone"}
	assert.ErrorContains(t, runServer(cfg), "run app")
}

func TestMigrationCommandsWithRealSQLite(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "migration-before.db")
	configPath := writeMigrationConfig(t, dsn)

	_, err := executeCommand(t, "-c", configPath, "migration", "up")
	require.NoError(t, err)
	_, err = executeCommand(t, "-c", configPath, "migration", "down", "iam")
	require.NoError(t, err)
	_, err = executeCommand(t, "-c", configPath, "migration", "up")
	require.NoError(t, err)
	_, err = executeCommand(t, "-c", configPath, "migration", "down-to", "iam", "0")
	require.NoError(t, err)

	afterDSN := filepath.Join(t.TempDir(), "migration-after.db")
	afterConfigPath := writeMigrationConfig(t, afterDSN)
	_, err = executeCommand(t, "migration", "up", "-c", afterConfigPath)
	require.NoError(t, err)
	db, err := (&bunx.Datasource{Type: "sqlite", Dsn: afterDSN}).Open()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.AssertMigrated(t.Context(), db))
	require.NoError(t, organization.AssertMigrated(t.Context(), db))
}

func TestExecuteSurfacesParseAndCommandErrors(t *testing.T) {
	assert.Error(t, Execute("--definitely-not-a-flag"))
	assert.Error(t, Execute("config", "validate", "--config", filepath.Join(t.TempDir(), "missing.yaml")))
}
