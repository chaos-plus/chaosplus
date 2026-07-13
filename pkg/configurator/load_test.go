package configurator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	return p
}

func TestLoadStrict_OK(t *testing.T) {
	p := writeTemp(t, "name: svc\nport: 9000\ntimeout: 15m\n")
	var cfg genSample
	require.NoError(t, LoadStrict(p, &cfg))
	assert.Equal(t, "svc", cfg.Name)
	assert.Equal(t, 9000, cfg.Port)
}

func TestLoadStrict_UnknownKeyErrors(t *testing.T) {
	p := writeTemp(t, "name: svc\nnope: 1\n")
	err := LoadStrict(p, &genSample{})
	require.Error(t, err, "an unknown key must fail validation")
	assert.Contains(t, err.Error(), "nope")
}

func TestLoadStrict_MissingFileErrors(t *testing.T) {
	err := LoadStrict(filepath.Join(t.TempDir(), "absent.yaml"), &genSample{})
	assert.Error(t, err)
}

func TestGenerateThenLoadStrict(t *testing.T) {
	data, err := GenerateYAML(&genSample{})
	require.NoError(t, err)
	p := writeTemp(t, string(data))

	var cfg genSample
	require.NoError(t, LoadStrict(p, &cfg), "a generated template must pass strict validation")
	assert.Equal(t, "app", cfg.Name)
}
