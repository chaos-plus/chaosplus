package goosex

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"

	goosev3 "github.com/pressly/goose/v3"
)

// ResolveDialect maps a dialect string to a goose dialect and the SQL
// subdirectory holding that dialect's migrations.
func ResolveDialect(dialect string) (goosev3.Dialect, string, error) {
	switch dialect {
	case "sqlite", "sqlite3":
		return goosev3.DialectSQLite3, "sql/sqlite", nil
	case "mysql":
		return goosev3.DialectMySQL, "sql/mysql", nil
	case "postgres", "postgresql", "pg":
		return goosev3.DialectPostgres, "sql/postgres", nil
	default:
		return "", "", fmt.Errorf("goose: unsupported dialect %q", dialect)
	}
}

// Run applies all pending migrations from fsys (rooted at sql/<dialect>) using a
// module-private goose version table. Global registry is disabled so multiple
// modules can run independently in one process.
func Run(ctx context.Context, db *sql.DB, fsys fs.FS, dialect, tableName string) error {
	d, subdir, err := ResolveDialect(dialect)
	if err != nil {
		return err
	}

	subFS, err := fs.Sub(fsys, subdir)
	if err != nil {
		return fmt.Errorf("goose: resolve migrations fs: %w", err)
	}

	provider, err := goosev3.NewProvider(d, db, subFS,
		goosev3.WithTableName(tableName),
		goosev3.WithDisableGlobalRegistry(true),
	)
	if err != nil {
		return fmt.Errorf("goose: create provider: %w", err)
	}

	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("goose: run migrations: %w", err)
	}
	return nil
}
