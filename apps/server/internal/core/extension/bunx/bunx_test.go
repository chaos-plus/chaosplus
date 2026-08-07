package bunx_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "github.com/uptrace/bun/driver/sqliteshim"
)

// TestIsUniqueViolationAcrossDialectMessages pins the exact driver messages the
// three supported engines emit. MySQL capitalizes "Duplicate entry", which the
// original lower-case-only match missed, silently downgrading every MySQL
// conflict from 409 to 500.
func TestIsUniqueViolationAcrossDialectMessages(t *testing.T) {
	unique := map[string]string{
		"mysql":    `Error 1062 (23000): Duplicate entry 'tenant-a-Operators' for key 'iam_roles.uq_iam_roles_name'`,
		"postgres": `ERROR: duplicate key value violates unique constraint "uq_iam_roles_name" (SQLSTATE=23505)`,
		"sqlite":   `UNIQUE constraint failed: iam_roles.tenant_id, iam_roles.name`,
	}
	for dialect, message := range unique {
		assert.True(t, bunx.IsUniqueViolation(errors.New(message)), "%s: %s", dialect, message)
	}

	other := map[string]string{
		"foreign key":   `FOREIGN KEY constraint failed`,
		"not null":      `Error 1048 (23000): Column 'name' cannot be null`,
		"check":         `ERROR: new row violates check constraint "ck_status" (SQLSTATE=23514)`,
		"connection":    `dial tcp 10.0.0.1:3306: connect: connection refused`,
		"missing table": `SQL logic error: no such table: iam_roles (1)`,
		"deadlock":      `Error 1213 (40001): Deadlock found when trying to get lock`,
		"serialization": `ERROR: could not serialize access due to concurrent update (SQLSTATE=40001)`,
		"lock wait":     `Error 1205 (HY000): Lock wait timeout exceeded`,
		"syntax":        `ERROR: syntax error at or near "SELCT" (SQLSTATE=42601)`,
	}
	for name, message := range other {
		assert.False(t, bunx.IsUniqueViolation(errors.New(message)), "%s: %s", name, message)
	}
	assert.False(t, bunx.IsUniqueViolation(nil))
}

func TestDatasourceDSNFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dsn")
	require.NoError(t, os.WriteFile(path, []byte(":memory:\n"), 0o600))
	db, err := (&bunx.Datasource{Type: "sqlite", DsnFile: path}).Open()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.PingContext(context.Background()))
	var busyTimeout, foreignKeys int
	require.NoError(t, db.QueryRowContext(t.Context(), "PRAGMA busy_timeout").Scan(&busyTimeout))
	require.NoError(t, db.QueryRowContext(t.Context(), "PRAGMA foreign_keys").Scan(&foreignKeys))
	assert.Equal(t, 5000, busyTimeout)
	assert.Equal(t, 1, foreignKeys)

	_, err = (&bunx.Datasource{Type: "sqlite", Dsn: ":memory:", DsnFile: path}).Open()
	assert.ErrorContains(t, err, "mutually exclusive")
	_, err = (&bunx.Datasource{Type: "unknown", Dsn: "value"}).Open()
	assert.ErrorContains(t, err, "unsupported")
}

func TestNormalizeDialect(t *testing.T) {
	tests := map[string]string{
		"sqlite":     "sqlite",
		"sqlite3":    "sqlite",
		"mysql":      "mysql",
		"pg":         "postgres",
		"pgsql":      "postgres",
		"postgres":   "postgres",
		"postgresql": "postgres",
	}
	for input, expected := range tests {
		t.Run(input, func(t *testing.T) {
			actual, err := bunx.NormalizeDialect(input)
			require.NoError(t, err)
			assert.Equal(t, expected, actual)
		})
	}
	_, err := bunx.NormalizeDialect("oracle")
	assert.ErrorContains(t, err, "unsupported datasource type")
}

func TestDatasourceFailureLogDoesNotExposeDSN(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	bunx.NewDatasourceRouter("test", false, map[string]bunx.Datasource{
		"primary": {Type: "unsupported", Dsn: "password=do-not-log-this"},
	})
	assert.NotContains(t, output.String(), "do-not-log-this")
	assert.Contains(t, output.String(), "unsupported")
}

func TestDatasourceDriversAndPoolConfiguration(t *testing.T) {
	_, err := (&bunx.Datasource{Type: "sqlite"}).Open()
	assert.ErrorContains(t, err, "DSN is required")
	assert.Nil(t, (&bunx.Datasource{Type: "unsupported", Dsn: "secret"}).NewDB())

	for name, datasource := range map[string]bunx.Datasource{
		"sqlite": {Type: "sqlite", Dsn: ":memory:"},
		"mysql":  {Type: "mysql", Dsn: "user:password@tcp(127.0.0.1:1)/database"},
		"pg":     {Type: "postgres", Dsn: "postgres://user:password@127.0.0.1:1/database?sslmode=disable"},
	} {
		t.Run(name, func(t *testing.T) {
			datasource.MaxOpenConns = 3
			datasource.MaxIdleConns = 2
			datasource.ConnMaxLifetime = time.Minute
			datasource.ConnMaxIdleTime = time.Second
			db, err := datasource.Open()
			require.NoError(t, err)
			assert.Equal(t, name, db.Dialect().Name().String())
			assert.Equal(t, 3, db.DB.Stats().MaxOpenConnections)
			require.NoError(t, db.Close())
		})
	}
}

func TestSQLiteConcurrentConnections(t *testing.T) {
	db, err := (&bunx.Datasource{
		Type: "sqlite", Dsn: filepath.Join(t.TempDir(), "concurrent.db"),
		Writable: true, MaxOpenConns: 8, MaxIdleConns: 8,
	}).Open()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.PingContext(t.Context()))

	var journalMode string
	var busyTimeout, foreignKeys int
	require.NoError(t, db.QueryRowContext(t.Context(), "PRAGMA journal_mode").Scan(&journalMode))
	require.NoError(t, db.QueryRowContext(t.Context(), "PRAGMA busy_timeout").Scan(&busyTimeout))
	require.NoError(t, db.QueryRowContext(t.Context(), "PRAGMA foreign_keys").Scan(&foreignKeys))
	assert.Equal(t, "wal", journalMode)
	assert.Equal(t, 5000, busyTimeout)
	assert.Equal(t, 1, foreignKeys)

	_, err = db.ExecContext(t.Context(), "CREATE TABLE counters (id INTEGER PRIMARY KEY, value INTEGER NOT NULL)")
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), "INSERT INTO counters (id, value) VALUES (1, 0)")
	require.NoError(t, err)

	const workers, increments = 12, 20
	errors := make(chan error, workers)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for range increments {
				if _, updateErr := db.ExecContext(t.Context(), "UPDATE counters SET value = value + 1 WHERE id = 1"); updateErr != nil {
					errors <- updateErr
					return
				}
			}
		}()
	}
	group.Wait()
	close(errors)
	for updateErr := range errors {
		require.NoError(t, updateErr)
	}
	var value int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT value FROM counters WHERE id = 1").Scan(&value))
	assert.Equal(t, workers*increments, value)
}

func TestDatasourceRouterReadWriteAndClose(t *testing.T) {
	router := bunx.NewDatasourceRouter("router-test", true, map[string]bunx.Datasource{
		"writer": {Type: "sqlite", Dsn: ":memory:", Writable: true},
		"reader": {Type: "sqlite", Dsn: ":memory:", Readable: true},
		"bad":    {Type: "unsupported", Dsn: "not-logged", Writable: true},
	})
	require.Len(t, router.Writer, 1)
	require.Len(t, router.Reader, 1)
	assert.Same(t, router.Writer[0], router.Write())
	assert.Same(t, router.Reader[0], router.Read())
	require.NoError(t, router.Close())

	fallback := bunx.NewDatasourceRouter("fallback", false, map[string]bunx.Datasource{
		"primary": {Type: "sqlite", Dsn: ":memory:", Writable: true},
	})
	assert.Same(t, fallback.Writer[0], fallback.Read())
	require.NoError(t, fallback.Close())
	assert.NoError(t, (&bunx.DatasourceRouter{}).Close())
}
