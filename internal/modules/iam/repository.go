package iam

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/uptrace/bun"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
)

type roleRow struct {
	bun.BaseModel `bun:"table:iam_roles"`
	TenantID      string `bun:"tenant_id,pk"`
	ID            string `bun:"id,pk"`
	Name          string
	Description   string
	CreatedAt     int64
	UpdatedAt     int64
}

type permissionRow struct {
	bun.BaseModel  `bun:"table:iam_role_permissions"`
	TenantID       string `bun:"tenant_id,pk"`
	RoleID         string `bun:"role_id,pk"`
	PermissionCode string `bun:"permission_code,pk"`
	ConditionJSON  string `bun:"condition_json"`
	CreatedAt      int64
}

type memberRow struct {
	bun.BaseModel `bun:"table:iam_role_members"`
	TenantID      string `bun:"tenant_id,pk"`
	RoleID        string `bun:"role_id,pk"`
	UserSubject   string `bun:"user_subject,pk"`
	CreatedAt     int64
}

type platformAdministratorRow struct {
	bun.BaseModel `bun:"table:iam_platform_administrators"`
	PrincipalID   string `bun:"principal_id,pk"`
	CreatedAt     int64
}

type Repository struct {
	db            *bun.DB
	executor      bun.IDB
	inTransaction bool
	nextID        IDGenerator
	dialect       string
	now           func() time.Time
}

func NewRepository(db *bun.DB, nextID IDGenerator) *Repository {
	if db == nil || nextID == nil {
		panic("iam repository requires database and id generator")
	}
	dialect := db.Dialect().Name().String()
	if dialect == "pg" {
		dialect = "postgres"
	}
	return &Repository{db: db, executor: db, nextID: nextID, dialect: dialect, now: time.Now}
}

func (r *Repository) withExecutor(executor bun.IDB) *Repository {
	transactional := *r
	transactional.executor = executor
	transactional.inTransaction = true
	return &transactional
}

func (r *Repository) runInTx(ctx context.Context, fn func(context.Context, bun.IDB) error) error {
	if r.inTransaction {
		return fn(ctx, r.executor)
	}
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		return fn(ctx, tx)
	})
}

func (r *Repository) CreateRole(ctx context.Context, tenantID, name, description string) (Role, error) {
	id, err := r.nextID()
	if err != nil {
		return Role{}, fmt.Errorf("generate role id: %w", err)
	}
	now := r.now().UTC().UnixMilli()
	row := roleRow{TenantID: tenantID, ID: id, Name: name, Description: description, CreatedAt: now, UpdatedAt: now}
	if _, err := r.executor.NewInsert().Model(&row).Exec(ctx); err != nil {
		if bunx.IsUniqueViolation(err) {
			return Role{}, ErrRoleNameConflict
		}
		return Role{}, fmt.Errorf("insert role: %w", err)
	}
	return roleFromRow(row), nil
}

// GrantPlatformAdministrator idempotently grants the built-in platform role.
// Platform permissions are deliberately not stored in tenant role tables.
func (r *Repository) GrantPlatformAdministrator(ctx context.Context, principalID string) (bool, error) {
	principalID = strings.TrimSpace(principalID)
	if principalID == "" || len(principalID) > 64 {
		return false, fmt.Errorf("invalid platform administrator principal")
	}
	row := platformAdministratorRow{PrincipalID: principalID, CreatedAt: r.now().UTC().UnixMilli()}
	result, err := r.executor.NewInsert().Model(&row).Ignore().Exec(ctx)
	if err != nil {
		return false, fmt.Errorf("grant platform administrator: %w", err)
	}
	affected, _ := result.RowsAffected()
	return affected > 0, nil
}

func (r *Repository) ListRoles(ctx context.Context, tenantID string) ([]Role, error) {
	var rows []roleRow
	if err := r.executor.NewSelect().Model(&rows).Where("tenant_id = ?", tenantID).Order("name ASC", "id ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	roles := make([]Role, 0, len(rows))
	for _, row := range rows {
		roles = append(roles, roleFromRow(row))
	}
	return roles, nil
}

func (r *Repository) GetRole(ctx context.Context, tenantID, roleID string) (Role, error) {
	row, err := getRoleRow(ctx, r.executor, tenantID, roleID)
	if err != nil {
		return Role{}, err
	}
	return roleFromRow(row), nil
}

func (r *Repository) UpdateRole(ctx context.Context, tenantID, roleID, name, description string) (Role, error) {
	now := r.now().UTC().UnixMilli()
	result, err := r.executor.NewUpdate().Model((*roleRow)(nil)).
		Set("name = ?", name).
		Set("description = ?", description).
		Set("updated_at = ?", now).
		Where("tenant_id = ? AND id = ?", tenantID, roleID).
		Exec(ctx)
	if err != nil {
		if bunx.IsUniqueViolation(err) {
			return Role{}, ErrRoleNameConflict
		}
		return Role{}, fmt.Errorf("update role: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return Role{}, ErrRoleNotFound
	}
	return r.GetRole(ctx, tenantID, roleID)
}

func (r *Repository) DeleteRole(ctx context.Context, tenantID, roleID string) error {
	return r.runInTx(ctx, func(ctx context.Context, tx bun.IDB) error {
		if err := ensureRole(ctx, tx, tenantID, roleID); err != nil {
			return err
		}
		if _, err := tx.NewDelete().Model((*permissionRow)(nil)).Where("tenant_id = ? AND role_id = ?", tenantID, roleID).Exec(ctx); err != nil {
			return fmt.Errorf("delete role permissions: %w", err)
		}
		if _, err := tx.NewDelete().Model((*memberRow)(nil)).Where("tenant_id = ? AND role_id = ?", tenantID, roleID).Exec(ctx); err != nil {
			return fmt.Errorf("delete role members: %w", err)
		}
		if _, err := tx.NewDelete().Model((*groupRoleBindingRow)(nil)).Where("tenant_id = ? AND role_id = ?", tenantID, roleID).Exec(ctx); err != nil {
			return fmt.Errorf("delete group role bindings: %w", err)
		}
		if _, err := tx.NewDelete().Model((*positionRoleBindingRow)(nil)).Where("tenant_id = ? AND role_id = ?", tenantID, roleID).Exec(ctx); err != nil {
			return fmt.Errorf("delete position role bindings: %w", err)
		}
		if _, err := tx.NewDelete().Model((*roleRow)(nil)).Where("tenant_id = ? AND id = ?", tenantID, roleID).Exec(ctx); err != nil {
			return fmt.Errorf("delete role: %w", err)
		}
		return nil
	})
}

func (r *Repository) GrantPermission(ctx context.Context, tenantID, roleID, code string) (bool, error) {
	return r.changePermission(ctx, tenantID, roleID, code, true)
}

func (r *Repository) RevokePermission(ctx context.Context, tenantID, roleID, code string) (bool, error) {
	return r.changePermission(ctx, tenantID, roleID, code, false)
}

func (r *Repository) changePermission(ctx context.Context, tenantID, roleID, code string, grant bool) (bool, error) {
	var changed bool
	err := r.runInTx(ctx, func(ctx context.Context, tx bun.IDB) error {
		if err := ensureRole(ctx, tx, tenantID, roleID); err != nil {
			return err
		}
		now := r.now().UTC().UnixMilli()
		if grant {
			row := permissionRow{TenantID: tenantID, RoleID: roleID, PermissionCode: code, CreatedAt: now}
			result, err := tx.NewInsert().Model(&row).Ignore().Exec(ctx)
			if err != nil {
				return fmt.Errorf("insert role permission: %w", err)
			}
			if affected, _ := result.RowsAffected(); affected > 0 {
				changed = true
			}
		} else {
			result, err := tx.NewDelete().Model((*permissionRow)(nil)).Where("tenant_id = ? AND role_id = ? AND permission_code = ?", tenantID, roleID, code).Exec(ctx)
			if err != nil {
				return fmt.Errorf("delete role permission: %w", err)
			}
			if affected, _ := result.RowsAffected(); affected > 0 {
				changed = true
			}
		}
		return nil
	})
	return changed, err
}

func (r *Repository) ListPermissions(ctx context.Context, tenantID, roleID string) ([]string, error) {
	grants, err := r.ListPermissionGrants(ctx, tenantID, roleID)
	if err != nil {
		return nil, err
	}
	codes := make([]string, 0, len(grants))
	for _, grant := range grants {
		codes = append(codes, grant.PermissionCode)
	}
	return codes, nil
}

func (r *Repository) ListPermissionGrants(ctx context.Context, tenantID, roleID string) ([]RolePermissionGrant, error) {
	if err := ensureRole(ctx, r.executor, tenantID, roleID); err != nil {
		return nil, err
	}
	rows := make([]permissionRow, 0)
	if err := r.executor.NewSelect().Model(&rows).Where("tenant_id = ? AND role_id = ?", tenantID, roleID).Order("permission_code ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list role permissions: %w", err)
	}
	grants := make([]RolePermissionGrant, 0, len(rows))
	for _, row := range rows {
		grants = append(grants, rolePermissionGrantFromRow(row))
	}
	return grants, nil
}

func (r *Repository) SetPermissionCondition(ctx context.Context, tenantID, roleID, code string, condition json.RawMessage) (RolePermissionGrant, bool, error) {
	var grant RolePermissionGrant
	var changed bool
	err := r.runInTx(ctx, func(ctx context.Context, tx bun.IDB) error {
		if err := ensureRole(ctx, tx, tenantID, roleID); err != nil {
			return err
		}
		var row permissionRow
		if err := tx.NewSelect().Model(&row).Where("tenant_id = ? AND role_id = ? AND permission_code = ?", tenantID, roleID, code).Scan(ctx); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrRolePermissionNotGranted
			}
			return fmt.Errorf("get role permission: %w", err)
		}
		if row.ConditionJSON == string(condition) {
			grant = rolePermissionGrantFromRow(row)
			return nil
		}
		if _, err := tx.NewUpdate().Model((*permissionRow)(nil)).Set("condition_json = ?", string(condition)).
			Where("tenant_id = ? AND role_id = ? AND permission_code = ?", tenantID, roleID, code).Exec(ctx); err != nil {
			return fmt.Errorf("update role permission condition: %w", err)
		}
		row.ConditionJSON = string(condition)
		grant, changed = rolePermissionGrantFromRow(row), true
		return nil
	})
	return grant, changed, err
}

func rolePermissionGrantFromRow(row permissionRow) RolePermissionGrant {
	var condition json.RawMessage
	if row.ConditionJSON != "" {
		condition = json.RawMessage(row.ConditionJSON)
	}
	return RolePermissionGrant{PermissionCode: row.PermissionCode, Condition: condition, CreatedAt: time.UnixMilli(row.CreatedAt).UTC()}
}

func (r *Repository) AddMember(ctx context.Context, tenantID, roleID, subject string) (bool, error) {
	return r.changeMember(ctx, tenantID, roleID, subject, true)
}

func (r *Repository) RemoveMember(ctx context.Context, tenantID, roleID, subject string) (bool, error) {
	return r.changeMember(ctx, tenantID, roleID, subject, false)
}

func (r *Repository) changeMember(ctx context.Context, tenantID, roleID, subject string, add bool) (bool, error) {
	var changed bool
	err := r.runInTx(ctx, func(ctx context.Context, tx bun.IDB) error {
		if err := ensureRole(ctx, tx, tenantID, roleID); err != nil {
			return err
		}
		now := r.now().UTC().UnixMilli()
		if add {
			row := memberRow{TenantID: tenantID, RoleID: roleID, UserSubject: subject, CreatedAt: now}
			result, err := tx.NewInsert().Model(&row).Ignore().Exec(ctx)
			if err != nil {
				return fmt.Errorf("insert role member: %w", err)
			}
			if affected, _ := result.RowsAffected(); affected > 0 {
				changed = true
			}
		} else {
			result, err := tx.NewDelete().Model((*memberRow)(nil)).Where("tenant_id = ? AND role_id = ? AND user_subject = ?", tenantID, roleID, subject).Exec(ctx)
			if err != nil {
				return fmt.Errorf("delete role member: %w", err)
			}
			if affected, _ := result.RowsAffected(); affected > 0 {
				changed = true
			}
		}
		return nil
	})
	return changed, err
}

func (r *Repository) ListMembers(ctx context.Context, tenantID, roleID string) ([]string, error) {
	if err := ensureRole(ctx, r.executor, tenantID, roleID); err != nil {
		return nil, err
	}
	subjects := make([]string, 0)
	if err := r.executor.NewSelect().Model((*memberRow)(nil)).Column("user_subject").Where("tenant_id = ? AND role_id = ?", tenantID, roleID).Order("user_subject ASC").Scan(ctx, &subjects); err != nil {
		return nil, fmt.Errorf("list role members: %w", err)
	}
	return subjects, nil
}

func getRoleRow(ctx context.Context, db bun.IDB, tenantID, roleID string) (roleRow, error) {
	var row roleRow
	if err := db.NewSelect().Model(&row).Where("tenant_id = ? AND id = ?", tenantID, roleID).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return roleRow{}, ErrRoleNotFound
		}
		return roleRow{}, fmt.Errorf("get role: %w", err)
	}
	return row, nil
}

func ensureRole(ctx context.Context, db bun.IDB, tenantID, roleID string) error {
	_, err := getRoleRow(ctx, db, tenantID, roleID)
	return err
}

func roleFromRow(row roleRow) Role {
	return Role{
		ID: row.ID, TenantID: row.TenantID, Name: row.Name, Description: row.Description,
		CreatedAt: time.UnixMilli(row.CreatedAt).UTC(), UpdatedAt: time.UnixMilli(row.UpdatedAt).UTC(),
	}
}

