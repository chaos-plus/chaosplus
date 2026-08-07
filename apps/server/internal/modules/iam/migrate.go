package iam

import (
	"context"
	"database/sql"
	"embed"
	"errors"
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
	for _, table := range []string{"iam_tenant_members", "iam_policy_revisions", "iam_platform_administrators", "iam_platform_grants", "iam_service_accounts", "iam_relationships", "iam_resource_relationships", "iam_temporary_role_grants", "iam_audit_retention_policies", "iam_stepup_challenges"} {
		if _, err := db.NewSelect().Table(table).ColumnExpr("1").Limit(1).Exec(ctx); err != nil {
			return fmt.Errorf("IAM schema is not ready (%s): %w", table, err)
		}
	}
	if _, err := db.NewSelect().Table("iam_principals").ColumnExpr("activation_required").Limit(1).Exec(ctx); err != nil {
		return fmt.Errorf("IAM schema is not ready (iam_principals.activation_required): %w", err)
	}
	for _, table := range []string{"iam_relationships", "iam_resource_relationships"} {
		if _, err := db.NewSelect().Table(table).ColumnExpr("starts_at + ends_at").Column("condition_json").Limit(1).Exec(ctx); err != nil {
			return fmt.Errorf("IAM schema is not ready (%s relationship policy): %w", table, err)
		}
	}
	for _, table := range []string{"iam_sessions", "iam_oauth_codes", "iam_refresh_tokens"} {
		if _, err := db.NewSelect().Table(table).ColumnExpr("auth_time + acr").Column("amr").Limit(1).Exec(ctx); err != nil {
			return fmt.Errorf("IAM schema is not ready (%s authentication assurance): %w", table, err)
		}
	}
	var condition string
	if err := db.NewSelect().Table("iam_role_permissions").ColumnExpr("condition_json").Limit(1).Scan(ctx, &condition); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("IAM schema is not ready (iam_role_permissions.condition_json): %w", err)
	}
	return nil
}
