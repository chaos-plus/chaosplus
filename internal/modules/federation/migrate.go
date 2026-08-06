package federation

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
		return fmt.Errorf("federation migration requires database")
	}
	return goosex.Run(ctx, db.DB, migrationsFS, db.Dialect().Name().String(), "goose_federation")
}

func MigrateDown(ctx context.Context, db *bun.DB) error {
	if db == nil {
		return fmt.Errorf("federation migration requires database")
	}
	return goosex.Down(ctx, db.DB, migrationsFS, db.Dialect().Name().String(), "goose_federation")
}

func MigrateDownTo(ctx context.Context, db *bun.DB, version int64) error {
	if db == nil {
		return fmt.Errorf("federation migration requires database")
	}
	return goosex.DownTo(ctx, db.DB, migrationsFS, db.Dialect().Name().String(), "goose_federation", version)
}

func AssertMigrated(ctx context.Context, db *bun.DB) error {
	if db == nil {
		return fmt.Errorf("federation schema is not ready: missing database")
	}
	for _, table := range []string{"iam_identity_providers", "iam_identity_links"} {
		if _, err := db.NewSelect().Table(table).ColumnExpr("1").Limit(1).Exec(ctx); err != nil {
			return fmt.Errorf("federation schema is not ready (%s): %w", table, err)
		}
	}
	return nil
}
