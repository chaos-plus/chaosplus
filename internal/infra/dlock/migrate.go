package dlock

import (
	"context"
	"embed"

	"github.com/uptrace/bun"

	"github.com/chaos-plus/chaosplus/internal/core/extension/goosex"
)

//go:embed sql/sqlite sql/mysql sql/postgres
var migrationsFS embed.FS

// Migrate applies this module's schema migrations using its own goose version
// table, so it migrates independently of other modules.
func Migrate(ctx context.Context, db *bun.DB) error {
	return goosex.Run(ctx, db.DB, migrationsFS, db.Dialect().Name().String(), "goose_dlock")
}

func MigrateDown(ctx context.Context, db *bun.DB) error {
	return goosex.Down(ctx, db.DB, migrationsFS, db.Dialect().Name().String(), "goose_dlock")
}

func MigrateDownTo(ctx context.Context, db *bun.DB, version int64) error {
	return goosex.DownTo(ctx, db.DB, migrationsFS, db.Dialect().Name().String(), "goose_dlock", version)
}
