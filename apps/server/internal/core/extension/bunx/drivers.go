package bunx

// Register the database/sql drivers that NewDB opens by name. Without these
// blank imports a "sqlite"/"mysql" datasource fails at runtime with "unknown
// driver". PostgreSQL needs no entry here because NewDB uses bun's pgdriver
// connector directly rather than a name-registered driver.
import (
	_ "github.com/go-sql-driver/mysql"           // registers the "mysql" driver
	_ "github.com/uptrace/bun/driver/sqliteshim" // registers the "sqliteshim" driver
)
