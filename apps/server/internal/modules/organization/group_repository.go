package organization

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

type groupRow struct {
	bun.BaseModel `bun:"table:iam_groups"`
	TenantID      guid.ID `bun:"tenant_id,pk"`
	ID            guid.ID `bun:"id,pk"`
	Name          string
	NameKey       string `bun:"name_key"`
	GroupType     string `bun:"group_type"`
	RuleJSON      string `bun:"rule_json"`
	Description   string
	Status        string
	SortOrder     int
	Version       int64
	CreatedAt     int64
	UpdatedAt     int64
}

type groupMemberRow struct {
	bun.BaseModel `bun:"table:iam_group_members"`
	TenantID      guid.ID `bun:"tenant_id,pk"`
	GroupID       guid.ID `bun:"group_id,pk"`
	PrincipalID   guid.ID `bun:"principal_id,pk"`
	StartsAt      int64
	EndsAt        int64
	CreatedAt     int64
	UpdatedAt     int64
	DisplayName   string  `bun:"display_name,scanonly"`
	Email         string  `bun:"email,scanonly"`
	DepartmentID  guid.ID `bun:"department_id,scanonly"`
	Status        string  `bun:"status,scanonly"`
}

type GroupRepository struct {
	db       *bun.DB
	executor bun.IDB
	dialect  string
}

func NewGroupRepository(db *bun.DB) *GroupRepository {
	if db == nil {
		panic("group repository requires database")
	}
	dialect := db.Dialect().Name().String()
	if dialect == "pg" {
		dialect = "postgres"
	}
	return &GroupRepository{db: db, executor: db, dialect: dialect}
}

func (r *GroupRepository) withExecutor(executor bun.IDB) *GroupRepository {
	return &GroupRepository{db: r.db, executor: executor, dialect: r.dialect}
}

func (r *GroupRepository) list(ctx context.Context, tenantID guid.ID) ([]groupRow, error) {
	rows := make([]groupRow, 0)
	if err := r.executor.NewSelect().Model(&rows).Where("tenant_id = ?", tenantID).Order("sort_order ASC", "name_key ASC", "id ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	return rows, nil
}

func (r *GroupRepository) get(ctx context.Context, tenantID, id guid.ID) (groupRow, error) {
	var row groupRow
	if err := r.executor.NewSelect().Model(&row).Where("tenant_id = ? AND id = ?", tenantID, id).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return groupRow{}, ErrGroupNotFound
		}
		return groupRow{}, fmt.Errorf("get group: %w", err)
	}
	return row, nil
}

func (r *GroupRepository) insert(ctx context.Context, row *groupRow) error {
	if _, err := r.executor.NewInsert().Model(row).Exec(ctx); err != nil {
		if isGroupNameViolation(err) {
			return ErrGroupNameConflict
		}
		return fmt.Errorf("insert group: %w", err)
	}
	return nil
}

func (r *GroupRepository) update(ctx context.Context, row *groupRow, expectedVersion int64) error {
	result, err := r.executor.NewUpdate().Model(row).
		Column("name", "name_key", "rule_json", "description", "status", "sort_order", "version", "updated_at").
		Where("tenant_id = ? AND id = ? AND version = ?", row.TenantID, row.ID, expectedVersion).Exec(ctx)
	if err != nil {
		if isGroupNameViolation(err) {
			return ErrGroupNameConflict
		}
		return fmt.Errorf("update group: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrGroupVersionConflict
	}
	return nil
}

func (r *GroupRepository) listDynamicMemberCandidates(ctx context.Context, tenantID, groupID guid.ID) ([]groupMemberRow, error) {
	rows := make([]groupMemberRow, 0)
	err := r.executor.NewSelect().
		TableExpr("iam_tenant_members AS tm").
		ColumnExpr("? AS group_id", groupID).
		ColumnExpr("tm.principal_id, tm.display_name, tm.email, COALESCE(md.department_id, 0) AS department_id, tm.status, tm.created_at, tm.updated_at").
		Join("LEFT JOIN iam_member_departments AS md ON md.tenant_id = tm.tenant_id AND md.principal_id = tm.principal_id").
		Where("tm.tenant_id = ? AND tm.status = ?", tenantID, StatusActive).
		OrderExpr("tm.display_name ASC, tm.principal_id ASC").Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("list dynamic group member candidates: %w", err)
	}
	return rows, nil
}

func (r *GroupRepository) delete(ctx context.Context, tenantID, id guid.ID, version int64) error {
	count, err := r.executor.NewSelect().Model((*groupMemberRow)(nil)).Where("tenant_id = ? AND group_id = ?", tenantID, id).Count(ctx)
	if err != nil {
		return fmt.Errorf("count group members: %w", err)
	}
	if count > 0 {
		return ErrGroupHasMembers
	}
	count, err = r.executor.NewSelect().Table("iam_group_role_bindings").Where("tenant_id = ? AND group_id = ?", tenantID, id).Count(ctx)
	if err != nil {
		return fmt.Errorf("count group role bindings: %w", err)
	}
	if count > 0 {
		return ErrGroupRoleBound
	}
	count, err = countRelationshipSubject(ctx, r.executor, tenantID, "group", id)
	if err != nil {
		return fmt.Errorf("count group relationships: %w", err)
	}
	if count > 0 {
		return ErrGroupRelationshipBound
	}
	result, err := r.executor.NewDelete().Model((*groupRow)(nil)).Where("tenant_id = ? AND id = ? AND version = ?", tenantID, id, version).Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete group: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrGroupVersionConflict
	}
	return nil
}

func (r *GroupRepository) listMembers(ctx context.Context, tenantID, groupID guid.ID) ([]groupMemberRow, error) {
	rows := make([]groupMemberRow, 0)
	err := r.executor.NewSelect().
		TableExpr("iam_group_members AS gm").
		ColumnExpr("gm.tenant_id, gm.group_id, gm.principal_id, gm.starts_at, gm.ends_at, gm.created_at, gm.updated_at").
		ColumnExpr("tm.display_name, tm.email").
		Join("JOIN iam_tenant_members AS tm ON tm.tenant_id = gm.tenant_id AND tm.principal_id = gm.principal_id").
		Where("gm.tenant_id = ? AND gm.group_id = ?", tenantID, groupID).
		OrderExpr("tm.display_name ASC, gm.principal_id ASC").Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("list group members: %w", err)
	}
	return rows, nil
}

func (r *GroupRepository) getMember(ctx context.Context, tenantID, groupID, principalID guid.ID) (groupMemberRow, error) {
	var row groupMemberRow
	err := r.executor.NewSelect().
		TableExpr("iam_group_members AS gm").
		ColumnExpr("gm.tenant_id, gm.group_id, gm.principal_id, gm.starts_at, gm.ends_at, gm.created_at, gm.updated_at").
		ColumnExpr("tm.display_name, tm.email").
		Join("JOIN iam_tenant_members AS tm ON tm.tenant_id = gm.tenant_id AND tm.principal_id = gm.principal_id").
		Where("gm.tenant_id = ? AND gm.group_id = ? AND gm.principal_id = ?", tenantID, groupID, principalID).Scan(ctx, &row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return groupMemberRow{}, ErrGroupMemberNotFound
		}
		return groupMemberRow{}, fmt.Errorf("get group member: %w", err)
	}
	return row, nil
}

func (r *GroupRepository) putMember(ctx context.Context, row *groupMemberRow) error {
	query := groupMemberUpsertSQL(r.dialect)
	if query == "" {
		return fmt.Errorf("unsupported organization database dialect %q", r.dialect)
	}
	if _, err := r.executor.ExecContext(ctx, query, row.TenantID, row.GroupID, row.PrincipalID, row.StartsAt, row.EndsAt, row.CreatedAt, row.UpdatedAt); err != nil {
		return fmt.Errorf("upsert group member: %w", err)
	}
	return nil
}

func (r *GroupRepository) deleteMember(ctx context.Context, tenantID, groupID, principalID guid.ID) (bool, error) {
	result, err := r.executor.NewDelete().Model((*groupMemberRow)(nil)).Where("tenant_id = ? AND group_id = ? AND principal_id = ?", tenantID, groupID, principalID).Exec(ctx)
	if err != nil {
		return false, fmt.Errorf("delete group member: %w", err)
	}
	affected, _ := result.RowsAffected()
	return affected == 1, nil
}

func groupMemberUpsertSQL(dialect string) string {
	const insert = `INSERT INTO iam_group_members
 (tenant_id, group_id, principal_id, starts_at, ends_at, created_at, updated_at)
 VALUES (?, ?, ?, ?, ?, ?, ?)`
	if dialect == "mysql" {
		return insert + ` ON DUPLICATE KEY UPDATE starts_at = VALUES(starts_at), ends_at = VALUES(ends_at), updated_at = VALUES(updated_at)`
	}
	if dialect == "sqlite" || dialect == "postgres" {
		return insert + ` ON CONFLICT (tenant_id, group_id, principal_id) DO UPDATE SET starts_at = excluded.starts_at, ends_at = excluded.ends_at, updated_at = excluded.updated_at`
	}
	return ""
}

func isGroupNameViolation(err error) bool {
	message := strings.ToLower(err.Error())
	return bunx.IsUniqueViolation(err) && (strings.Contains(message, "groups_name") || strings.Contains(message, "tenant_id") && strings.Contains(message, "name_key"))
}
