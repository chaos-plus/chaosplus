package iam

import (
	"context"
	"embed"
	"fmt"

	"github.com/uptrace/bun"

	"github.com/chaos-plus/chaosplus/internal/core/extension/goosex"
)

//go:embed sql/sqlite sql/mysql sql/postgres
var migrationsFS embed.FS

func Migrate(ctx context.Context, db *bun.DB) error {
	return goosex.Run(ctx, db.DB, migrationsFS, db.Dialect().Name().String(), "goose_iam")
}

func MigrateDown(ctx context.Context, db *bun.DB) error {
	return goosex.Down(ctx, db.DB, migrationsFS, db.Dialect().Name().String(), "goose_iam")
}

func MigrateDownTo(ctx context.Context, db *bun.DB, version int64) error {
	return goosex.DownTo(ctx, db.DB, migrationsFS, db.Dialect().Name().String(), "goose_iam", version)
}

// AssertMigrated fails startup before serving traffic when migrations are
// delegated to the production bootstrap job but its IAM schema is unavailable.
func AssertMigrated(ctx context.Context, db *bun.DB) error {
	if _, err := db.NewSelect().Table("iam_tenant_members").ColumnExpr("1").Limit(1).Exec(ctx); err != nil {
		return fmt.Errorf("IAM schema is not ready: %w", err)
	}
	return nil
}
