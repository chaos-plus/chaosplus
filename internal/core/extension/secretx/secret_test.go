package secretx

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolve(t *testing.T) {
	assert.Equal(t, "inline", mustResolve(t, "inline", ""))
	assert.Empty(t, mustResolve(t, "", ""))

	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	require.NoError(t, os.WriteFile(path, []byte("from-file\r\n"), 0o600))
	assert.Equal(t, "from-file", mustResolve(t, "", path))

	_, err := Resolve("test", "inline", path, 100)
	assert.ErrorContains(t, err, "mutually exclusive")
	_, err = Resolve("test", "", dir, 100)
	assert.ErrorContains(t, err, "not a regular file")
	_, err = Resolve("test", "", filepath.Join(dir, "missing"), 100)
	assert.Error(t, err)

	require.NoError(t, os.WriteFile(path, nil, 0o600))
	_, err = Resolve("test", "", path, 100)
	assert.ErrorContains(t, err, "empty")
	require.NoError(t, os.WriteFile(path, []byte("too-long"), 0o600))
	_, err = Resolve("test", "", path, 3)
	assert.ErrorContains(t, err, "exceeds")
}

func mustResolve(t *testing.T, value, path string) string {
	t.Helper()
	resolved, err := Resolve("test", value, path, 100)
	require.NoError(t, err)
	return resolved
}
