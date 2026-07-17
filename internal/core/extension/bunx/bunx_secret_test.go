package bunx_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "github.com/uptrace/bun/driver/sqliteshim"
)

func TestDatasourceDSNFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dsn")
	require.NoError(t, os.WriteFile(path, []byte(":memory:\n"), 0o600))
	db, err := (&bunx.Datasource{Type: "sqlite", DsnFile: path}).Open()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.PingContext(context.Background()))

	_, err = (&bunx.Datasource{Type: "sqlite", Dsn: ":memory:", DsnFile: path}).Open()
	assert.ErrorContains(t, err, "mutually exclusive")
	_, err = (&bunx.Datasource{Type: "unknown", Dsn: "value"}).Open()
	assert.ErrorContains(t, err, "unsupported")
}
