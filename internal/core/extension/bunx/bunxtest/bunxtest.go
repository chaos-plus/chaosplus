// Package bunxtest provides bun databases for unit tests. It sits beside bunx
// (which owns dialect and driver selection) rather than in each caller, and is a
// separate package so the SQLite test driver never links into production
// binaries.
package bunxtest

import (
	"errors"

	"github.com/uptrace/bun"
	_ "github.com/uptrace/bun/driver/sqliteshim" // registers the "sqliteshim" driver used by bunx for Type "sqlite"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
)

// Memory opens a fresh, isolated in-memory SQLite database through bunx, so the
// dialect wiring under test is exactly the one production uses.
//
// A private ":memory:" database is scoped to a single connection and dropped
// when that connection closes, so the pool is pinned to one connection: this
// keeps the database alive for the whole test and gives each call its own
// database with no cross-test bleed. Callers Close it when done (t.Cleanup).
func Memory() (*bun.DB, error) {
	ds := bunx.Datasource{Type: "sqlite", Dsn: ":memory:"}
	db := ds.NewDB()
	if db == nil {
		return nil, errors.New("bunxtest: open in-memory sqlite failed")
	}
	db.DB.SetMaxOpenConns(1)
	db.DB.SetMaxIdleConns(1)
	return db, nil
}
