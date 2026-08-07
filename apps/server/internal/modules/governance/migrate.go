package governance

import (
	"context"
	"embed"
	"fmt"

	"github.com/chaos-plus/chaosplus/internal/core/extension/goosex"
	"github.com/uptrace/bun"
)

//go:embed sql/sqlite sql/mysql sql/postgres
var migrationsFS embed.FS

func Migrate(ctx context.Context, db *bun.DB) error {
	if db == nil {
		return fmt.Errorf("governance migration requires database")
	}
	return goosex.Run(ctx, db.DB, migrationsFS, db.Dialect().Name().String(), "goose_governance")
}

func MigrateDown(ctx context.Context, db *bun.DB) error {
	if db == nil {
		return fmt.Errorf("governance migration requires database")
	}
	return goosex.Down(ctx, db.DB, migrationsFS, db.Dialect().Name().String(), "goose_governance")
}

func MigrateDownTo(ctx context.Context, db *bun.DB, version int64) error {
	if db == nil {
		return fmt.Errorf("governance migration requires database")
	}
	return goosex.DownTo(ctx, db.DB, migrationsFS, db.Dialect().Name().String(), "goose_governance", version)
}

func AssertMigrated(ctx context.Context, db *bun.DB) error {
	if db == nil {
		return fmt.Errorf("governance schema is not ready: missing database")
	}
	for _, table := range []string{"iam_access_requests", "iam_approval_steps", "iam_access_reviews", "iam_access_review_items"} {
		if _, err := db.NewSelect().Table(table).ColumnExpr("1").Limit(1).Exec(ctx); err != nil {
			return fmt.Errorf("governance schema is not ready (%s): %w", table, err)
		}
	}
	upToDate, err := goosex.UpToDate(ctx, db.DB, migrationsFS, db.Dialect().Name().String(), "goose_governance")
	if err != nil {
		return fmt.Errorf("governance schema is not ready (migration state): %w", err)
	}
	if !upToDate {
		return fmt.Errorf("governance schema is not ready: migrations are pending")
	}
	return nil
}
