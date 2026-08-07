package goosex

import (
	"context"
	"embed"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
)

//go:embed testdata/sql
var testdataFS embed.FS

// migrationsRoot re-roots the embedded FS at "sql" so it matches what Run
// expects (it resolves the "sql/<dialect>" subdirectory internally).
func migrationsRoot(t *testing.T) fs.FS {
	t.Helper()
	root, err := fs.Sub(testdataFS, "testdata")
	require.NoError(t, err)
	return root
}

func TestRun_AppliesMigrationsAndIsIdempotent(t *testing.T) {
	ctx := context.Background()

	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	root := migrationsRoot(t)

	// First run applies 00001_widgets: the widgets table should now exist and
	// be writable.
	require.NoError(t, Run(ctx, db.DB, root, "sqlite", "goose_widgets_test"))

	_, err = db.DB.ExecContext(ctx, `INSERT INTO widgets (id, name) VALUES (1, 'w')`)
	require.NoError(t, err)

	var n int
	require.NoError(t, db.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM widgets`).Scan(&n))
	assert.Equal(t, 1, n)

	// Re-running is a no-op (goose records applied versions) and must not error
	// or wipe the row.
	require.NoError(t, Run(ctx, db.DB, root, "sqlite", "goose_widgets_test"))

	require.NoError(t, db.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM widgets`).Scan(&n))
	assert.Equal(t, 1, n, "second Run should be idempotent")
}

func TestDownAndDownTo(t *testing.T) {
	ctx := context.Background()
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	root := migrationsRoot(t)

	require.NoError(t, Run(ctx, db.DB, root, "sqlite", "goose_widgets_down_test"))
	require.NoError(t, Down(ctx, db.DB, root, "sqlite", "goose_widgets_down_test"))
	_, err = db.DB.ExecContext(ctx, `INSERT INTO widgets (id, name) VALUES (1, 'gone')`)
	assert.Error(t, err)

	require.NoError(t, Run(ctx, db.DB, root, "sqlite", "goose_widgets_down_test"))
	require.NoError(t, DownTo(ctx, db.DB, root, "sqlite", "goose_widgets_down_test", 0))
	_, err = db.DB.ExecContext(ctx, `INSERT INTO widgets (id, name) VALUES (2, 'gone')`)
	assert.Error(t, err)
}

func TestUpToDate(t *testing.T) {
	ctx := context.Background()
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	root := migrationsRoot(t)

	upToDate, err := UpToDate(ctx, db.DB, root, "sqlite", "goose_widgets_uptodate_test")
	require.NoError(t, err)
	assert.False(t, upToDate, "fresh database must not be up to date")

	require.NoError(t, Run(ctx, db.DB, root, "sqlite", "goose_widgets_uptodate_test"))
	upToDate, err = UpToDate(ctx, db.DB, root, "sqlite", "goose_widgets_uptodate_test")
	require.NoError(t, err)
	assert.True(t, upToDate, "fully migrated database must be up to date")

	require.NoError(t, Down(ctx, db.DB, root, "sqlite", "goose_widgets_uptodate_test"))
	upToDate, err = UpToDate(ctx, db.DB, root, "sqlite", "goose_widgets_uptodate_test")
	require.NoError(t, err)
	assert.False(t, upToDate, "database with a rolled-back migration must not be up to date")
}

func TestRun_UnsupportedDialect(t *testing.T) {
	ctx := context.Background()

	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	err = Run(ctx, db.DB, migrationsRoot(t), "oracle", "goose_bad_dialect")
	assert.Error(t, err)
}

func TestResolveDialect(t *testing.T) {
	for _, d := range []string{"sqlite", "sqlite3", "mysql", "postgres", "postgresql", "pgsql", "pg", " PGSQL "} {
		_, subdir, err := ResolveDialect(d)
		require.NoError(t, err, "dialect %q", d)
		assert.NotEmpty(t, subdir)
	}
	_, _, err := ResolveDialect("oracle")
	assert.Error(t, err)
}

func TestMigrationFailuresArePropagated(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	require.NoError(t, db.Close())
	root := migrationsRoot(t)

	assert.ErrorContains(t, Run(t.Context(), db.DB, root, "sqlite", "goose_closed_run"), "run migrations")
	assert.ErrorContains(t, Down(t.Context(), db.DB, root, "sqlite", "goose_closed_down"), "rollback migration")
	assert.ErrorContains(t, DownTo(t.Context(), db.DB, root, "sqlite", "goose_closed_down_to", 0), "rollback migrations")

	empty := fstest.MapFS{"unrelated.txt": {Data: []byte("value")}}
	_, err = newProvider(db.DB, empty, "sqlite", "goose_missing_fs")
	assert.ErrorContains(t, err, "create provider")
}
