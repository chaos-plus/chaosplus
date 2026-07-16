package iam

import (
	"context"
	"embed"

	"github.com/uptrace/bun"

	"github.com/chaos-plus/chaosplus/internal/core/extension/goosex"
)

//go:embed sql/sqlite sql/mysql sql/postgres
var migrationsFS embed.FS

func Migrate(ctx context.Context, db *bun.DB) error {
	return goosex.Run(ctx, db.DB, migrationsFS, db.Dialect().Name().String(), "goose_iam")
}
