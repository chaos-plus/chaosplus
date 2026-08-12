package organization

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
		return fmt.Errorf("organization migration requires database")
	}
	if err := goosex.Run(ctx, db.DB, migrationsFS, db.Dialect().Name().String(), "goose_organization"); err != nil {
		return err
	}
	return nil
}

func MigrateDown(ctx context.Context, db *bun.DB) error {
	if db == nil {
		return fmt.Errorf("organization migration requires database")
	}
	return goosex.Down(ctx, db.DB, migrationsFS, db.Dialect().Name().String(), "goose_organization")
}

func MigrateDownTo(ctx context.Context, db *bun.DB, version int64) error {
	if db == nil {
		return fmt.Errorf("organization migration requires database")
	}
	return goosex.DownTo(ctx, db.DB, migrationsFS, db.Dialect().Name().String(), "goose_organization", version)
}

func AssertMigrated(ctx context.Context, db *bun.DB) error {
	if db == nil {
		return fmt.Errorf("organization schema is not ready: missing database")
	}
	for _, table := range []string{"iam_tenants", "iam_departments", "iam_department_closure", "iam_positions", "iam_position_members", "iam_groups", "iam_group_members", "iam_group_role_bindings", "iam_position_role_bindings", "iam_member_departments", "iam_role_data_scopes", "iam_role_scope_departments", "iam_invitations", "iam_invitation_roles"} {
		if _, err := db.NewSelect().Table(table).ColumnExpr("1").Limit(1).Exec(ctx); err != nil {
			return fmt.Errorf("organization schema is not ready (%s): %w", table, err)
		}
	}
	return nil
}
