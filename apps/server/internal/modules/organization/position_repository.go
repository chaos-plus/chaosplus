package organization

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/uptrace/bun"
)

type positionRow struct {
	bun.BaseModel `bun:"table:iam_positions"`
	TenantID      string `bun:"tenant_id,pk"`
	ID            string `bun:"id,pk"`
	Code          string
	Name          string
	Status        string
	SortOrder     int
	Version       int64
	CreatedAt     int64
	UpdatedAt     int64
}

type positionMemberRow struct {
	bun.BaseModel `bun:"table:iam_position_members"`
	TenantID      string `bun:"tenant_id,pk"`
	PositionID    string `bun:"position_id,pk"`
	PrincipalID   string `bun:"principal_id,pk"`
	StartsAt      int64
	EndsAt        int64
	CreatedAt     int64
	UpdatedAt     int64
	DisplayName   string `bun:"display_name,scanonly"`
	Email         string `bun:"email,scanonly"`
}

type PositionRepository struct {
	db       *bun.DB
	executor bun.IDB
	dialect  string
}

func NewPositionRepository(db *bun.DB) *PositionRepository {
	if db == nil {
		panic("position repository requires database")
	}
	dialect := db.Dialect().Name().String()
	if dialect == "pg" {
		dialect = "postgres"
	}
	return &PositionRepository{db: db, executor: db, dialect: dialect}
}

func (r *PositionRepository) withExecutor(executor bun.IDB) *PositionRepository {
	return &PositionRepository{db: r.db, executor: executor, dialect: r.dialect}
}

func (r *PositionRepository) list(ctx context.Context, tenantID string) ([]positionRow, error) {
	rows := make([]positionRow, 0)
	if err := r.executor.NewSelect().Model(&rows).Where("tenant_id = ?", tenantID).Order("sort_order ASC", "code ASC", "id ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list positions: %w", err)
	}
	return rows, nil
}

func (r *PositionRepository) get(ctx context.Context, tenantID, id string) (positionRow, error) {
	var row positionRow
	if err := r.executor.NewSelect().Model(&row).Where("tenant_id = ? AND id = ?", tenantID, id).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return positionRow{}, ErrPositionNotFound
		}
		return positionRow{}, fmt.Errorf("get position: %w", err)
	}
	return row, nil
}

func (r *PositionRepository) insert(ctx context.Context, row *positionRow) error {
	if _, err := r.executor.NewInsert().Model(row).Exec(ctx); err != nil {
		if isPositionCodeViolation(err) {
			return ErrPositionCodeConflict
		}
		return fmt.Errorf("insert position: %w", err)
	}
	return nil
}

func (r *PositionRepository) update(ctx context.Context, row *positionRow, expectedVersion int64) error {
	result, err := r.executor.NewUpdate().Model(row).
		Column("code", "name", "status", "sort_order", "version", "updated_at").
		Where("tenant_id = ? AND id = ? AND version = ?", row.TenantID, row.ID, expectedVersion).Exec(ctx)
	if err != nil {
		if isPositionCodeViolation(err) {
			return ErrPositionCodeConflict
		}
		return fmt.Errorf("update position: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrPositionVersionConflict
	}
	return nil
}

func (r *PositionRepository) delete(ctx context.Context, tenantID, id string, version int64) error {
	count, err := r.executor.NewSelect().Model((*positionMemberRow)(nil)).Where("tenant_id = ? AND position_id = ?", tenantID, id).Count(ctx)
	if err != nil {
		return fmt.Errorf("count position members: %w", err)
	}
	if count > 0 {
		return ErrPositionHasMembers
	}
	count, err = r.executor.NewSelect().Table("iam_position_role_bindings").Where("tenant_id = ? AND position_id = ?", tenantID, id).Count(ctx)
	if err != nil {
		return fmt.Errorf("count position role bindings: %w", err)
	}
	if count > 0 {
		return ErrPositionRoleBound
	}
	count, err = countRelationshipSubject(ctx, r.executor, tenantID, "position", id)
	if err != nil {
		return fmt.Errorf("count position relationships: %w", err)
	}
	if count > 0 {
		return ErrPositionRelationshipBound
	}
	result, err := r.executor.NewDelete().Model((*positionRow)(nil)).Where("tenant_id = ? AND id = ? AND version = ?", tenantID, id, version).Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete position: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrPositionVersionConflict
	}
	return nil
}

func (r *PositionRepository) listMembers(ctx context.Context, tenantID, positionID string) ([]positionMemberRow, error) {
	rows := make([]positionMemberRow, 0)
	err := r.executor.NewSelect().
		TableExpr("iam_position_members AS pm").
		ColumnExpr("pm.tenant_id, pm.position_id, pm.principal_id, pm.starts_at, pm.ends_at, pm.created_at, pm.updated_at").
		ColumnExpr("tm.display_name, tm.email").
		Join("JOIN iam_tenant_members AS tm ON tm.tenant_id = pm.tenant_id AND tm.user_subject = pm.principal_id").
		Where("pm.tenant_id = ? AND pm.position_id = ?", tenantID, positionID).
		OrderExpr("tm.display_name ASC, pm.principal_id ASC").Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("list position members: %w", err)
	}
	return rows, nil
}

func (r *PositionRepository) getMember(ctx context.Context, tenantID, positionID, principalID string) (positionMemberRow, error) {
	var row positionMemberRow
	err := r.executor.NewSelect().
		TableExpr("iam_position_members AS pm").
		ColumnExpr("pm.tenant_id, pm.position_id, pm.principal_id, pm.starts_at, pm.ends_at, pm.created_at, pm.updated_at").
		ColumnExpr("tm.display_name, tm.email").
		Join("JOIN iam_tenant_members AS tm ON tm.tenant_id = pm.tenant_id AND tm.user_subject = pm.principal_id").
		Where("pm.tenant_id = ? AND pm.position_id = ? AND pm.principal_id = ?", tenantID, positionID, principalID).Scan(ctx, &row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return positionMemberRow{}, ErrPositionMemberNotFound
		}
		return positionMemberRow{}, fmt.Errorf("get position member: %w", err)
	}
	return row, nil
}

func (r *PositionRepository) putMember(ctx context.Context, row *positionMemberRow) error {
	query := positionMemberUpsertSQL(r.dialect)
	if query == "" {
		return fmt.Errorf("unsupported organization database dialect %q", r.dialect)
	}
	if _, err := r.executor.ExecContext(ctx, query, row.TenantID, row.PositionID, row.PrincipalID, row.StartsAt, row.EndsAt, row.CreatedAt, row.UpdatedAt); err != nil {
		return fmt.Errorf("upsert position member: %w", err)
	}
	return nil
}

func (r *PositionRepository) deleteMember(ctx context.Context, tenantID, positionID, principalID string) (bool, error) {
	result, err := r.executor.NewDelete().Model((*positionMemberRow)(nil)).Where("tenant_id = ? AND position_id = ? AND principal_id = ?", tenantID, positionID, principalID).Exec(ctx)
	if err != nil {
		return false, fmt.Errorf("delete position member: %w", err)
	}
	affected, _ := result.RowsAffected()
	return affected == 1, nil
}

func positionMemberUpsertSQL(dialect string) string {
	const insert = `INSERT INTO iam_position_members
 (tenant_id, position_id, principal_id, starts_at, ends_at, created_at, updated_at)
 VALUES (?, ?, ?, ?, ?, ?, ?)`
	if dialect == "mysql" {
		return insert + ` ON DUPLICATE KEY UPDATE starts_at = VALUES(starts_at), ends_at = VALUES(ends_at), updated_at = VALUES(updated_at)`
	}
	if dialect == "sqlite" || dialect == "postgres" {
		return insert + ` ON CONFLICT (tenant_id, position_id, principal_id) DO UPDATE SET starts_at = excluded.starts_at, ends_at = excluded.ends_at, updated_at = excluded.updated_at`
	}
	return ""
}

func isPositionCodeViolation(err error) bool {
	message := strings.ToLower(err.Error())
	return bunx.IsUniqueViolation(err) && (strings.Contains(message, "positions_code") || strings.Contains(message, "tenant_id") && strings.Contains(message, "code"))
}
