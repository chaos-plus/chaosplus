package compose_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type composeFile struct {
	Services map[string]composeService `yaml:"services"`
}

type composeService struct {
	DependsOn map[string]any `yaml:"depends_on"`
	Secrets   []string       `yaml:"secrets"`
}

func TestChaosplusStartsAfterInfrastructureAndMigratesInProcess(t *testing.T) {
	data, err := os.ReadFile("compose.yaml")
	require.NoError(t, err)

	var cfg composeFile
	require.NoError(t, yaml.Unmarshal(data, &cfg))
	chaosplus, ok := cfg.Services["chaosplus"]
	require.True(t, ok)
	assert.NotContains(t, cfg.Services, "chaosplus-migrate")
	assert.NotContains(t, cfg.Services, "bootstrap")
	assert.NotContains(t, cfg.Services, "api")

	for _, dependency := range []string{"postgres", "spicedb", "zitadel-api", "redis"} {
		assert.Contains(t, chaosplus.DependsOn, dependency)
	}
	assert.Contains(t, chaosplus.Secrets, "chaosplus_migration_dsn")
}
