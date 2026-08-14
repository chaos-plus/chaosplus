package defect

import (
	"context"
	"embed"
	"fmt"
	"github.com/chaos-plus/chaosplus/internal/core/extension/goosex"
	"github.com/uptrace/bun"
)

//go:embed sql/sqlite sql/mysql sql/postgres
var migrations embed.FS

func Migrate(ctx context.Context, db *bun.DB) error {
	if db == nil {
		return fmt.Errorf("defect migration requires database")
	}
	d := db.Dialect().Name().String()
	if d == "pg" {
		d = "postgres"
	}
	return goosex.Run(ctx, db.DB, migrations, d, "goose_ai_workspace_defect")
}

func MigrateDownTo(ctx context.Context, db *bun.DB, version int64) error {
	if db == nil {
		return fmt.Errorf("defect migration requires database")
	}
	d := db.Dialect().Name().String()
	if d == "pg" {
		d = "postgres"
	}
	return goosex.DownTo(ctx, db.DB, migrations, d, "goose_ai_workspace_defect", version)
}
