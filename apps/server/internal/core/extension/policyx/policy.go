// Package policyx maintains tenant authorization revision counters.
package policyx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

func Advance(ctx context.Context, db bun.IDB, dialect string, tenantID guid.ID, updatedAt int64) error {
	query := upsertSQL(normalizeDialect(dialect))
	if db == nil || tenantID.Zero() || query == "" {
		return fmt.Errorf("invalid policy revision store configuration")
	}
	if _, err := db.ExecContext(ctx, query, tenantID, updatedAt); err != nil {
		return fmt.Errorf("advance IAM policy revision: %w", err)
	}
	return nil
}

// Lock serializes tenant policy mutations without changing the revision. The
// no-op update acquires a row lock on MySQL and PostgreSQL and a write lock on
// SQLite for the lifetime of the caller's transaction.
func Lock(ctx context.Context, db bun.IDB, dialect string, tenantID guid.ID) error {
	dialect = normalizeDialect(dialect)
	query := ensureSQL(dialect)
	if db == nil || tenantID.Zero() || query == "" {
		return fmt.Errorf("invalid policy revision lock configuration")
	}
	if _, err := db.ExecContext(ctx, query, tenantID); err != nil {
		return fmt.Errorf("ensure IAM policy revision lock row: %w", err)
	}
	_, err := db.ExecContext(ctx, `UPDATE iam_policy_revisions SET revision = revision WHERE tenant_id = ?`, tenantID)
	if err != nil {
		return fmt.Errorf("lock IAM policy revision: %w", err)
	}
	return nil
}

func Current(ctx context.Context, db bun.IDB, tenantID guid.ID) (int64, error) {
	if db == nil || tenantID.Zero() {
		return 0, fmt.Errorf("invalid policy revision query")
	}
	var revision int64
	if err := db.NewSelect().Table("iam_policy_revisions").Column("revision").Where("tenant_id = ?", tenantID).Scan(ctx, &revision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("get IAM policy revision: %w", err)
	}
	return revision, nil
}

func normalizeDialect(dialect string) string {
	if dialect == "pg" {
		return "postgres"
	}
	return dialect
}

func upsertSQL(dialect string) string {
	const insert = `INSERT INTO iam_policy_revisions (tenant_id, revision, updated_at) VALUES (?, 1, ?)`
	switch dialect {
	case "mysql":
		return insert + ` ON DUPLICATE KEY UPDATE revision = revision + 1, updated_at = VALUES(updated_at)`
	case "postgres", "sqlite":
		return insert + ` ON CONFLICT (tenant_id) DO UPDATE SET revision = iam_policy_revisions.revision + 1, updated_at = excluded.updated_at`
	default:
		return ""
	}
}

func ensureSQL(dialect string) string {
	const insert = `INSERT INTO iam_policy_revisions (tenant_id, revision, updated_at) VALUES (?, 0, 0)`
	switch dialect {
	case "mysql":
		return insert + ` ON DUPLICATE KEY UPDATE tenant_id = VALUES(tenant_id)`
	case "postgres", "sqlite":
		return insert + ` ON CONFLICT (tenant_id) DO NOTHING`
	default:
		return ""
	}
}
