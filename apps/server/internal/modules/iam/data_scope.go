package iam

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/uptrace/bun"
)

type roleDataScopeRow struct {
	bun.BaseModel `bun:"table:iam_role_data_scopes"`
	TenantID      string `bun:"tenant_id,pk"`
	RoleID        string `bun:"role_id,pk"`
	ScopeType     string
	UpdatedAt     int64
}

type roleScopeDepartmentRow struct {
	bun.BaseModel `bun:"table:iam_role_scope_departments"`
	TenantID      string `bun:"tenant_id,pk"`
	RoleID        string `bun:"role_id,pk"`
	DepartmentID  string `bun:"department_id,pk"`
	CreatedAt     int64
}

type memberDepartmentRow struct {
	bun.BaseModel `bun:"table:iam_member_departments"`
	TenantID      string `bun:"tenant_id,pk"`
	PrincipalID   string `bun:"principal_id,pk"`
	DepartmentID  string
	UpdatedAt     int64
}

func (r *Repository) GetRoleDataScope(ctx context.Context, tenantID, roleID string) (RoleDataScope, error) {
	if err := ensureRole(ctx, r.executor, tenantID, roleID); err != nil {
		return RoleDataScope{}, err
	}
	var row roleDataScopeRow
	err := r.executor.NewSelect().Model(&row).Where("tenant_id = ? AND role_id = ?", tenantID, roleID).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return RoleDataScope{RoleID: roleID, Scope: DataScopeAll, DepartmentIDs: []string{}}, nil
	}
	if err != nil {
		return RoleDataScope{}, fmt.Errorf("get role data scope: %w", err)
	}
	departmentIDs := make([]string, 0)
	if err := r.executor.NewSelect().Model((*roleScopeDepartmentRow)(nil)).Column("department_id").
		Where("tenant_id = ? AND role_id = ?", tenantID, roleID).Order("department_id ASC").Scan(ctx, &departmentIDs); err != nil {
		return RoleDataScope{}, fmt.Errorf("list role scope departments: %w", err)
	}
	return RoleDataScope{RoleID: roleID, Scope: DataScope(row.ScopeType), DepartmentIDs: departmentIDs, UpdatedAt: time.UnixMilli(row.UpdatedAt).UTC()}, nil
}

func (r *Repository) replaceRoleDataScope(ctx context.Context, tenantID, roleID string, scope DataScope, departmentIDs []string) error {
	if _, err := r.executor.NewDelete().Model((*roleDataScopeRow)(nil)).Where("tenant_id = ? AND role_id = ?", tenantID, roleID).Exec(ctx); err != nil {
		return fmt.Errorf("delete role data scope: %w", err)
	}
	if scope == DataScopeAll {
		return nil
	}
	now := r.now().UTC().UnixMilli()
	row := roleDataScopeRow{TenantID: tenantID, RoleID: roleID, ScopeType: string(scope), UpdatedAt: now}
	if _, err := r.executor.NewInsert().Model(&row).Exec(ctx); err != nil {
		return fmt.Errorf("insert role data scope: %w", err)
	}
	rows := make([]roleScopeDepartmentRow, 0, len(departmentIDs))
	for _, departmentID := range departmentIDs {
		rows = append(rows, roleScopeDepartmentRow{TenantID: tenantID, RoleID: roleID, DepartmentID: departmentID, CreatedAt: now})
	}
	if len(rows) > 0 {
		if _, err := r.executor.NewInsert().Model(&rows).Exec(ctx); err != nil {
			return fmt.Errorf("insert role scope departments: %w", err)
		}
	}
	return nil
}

func (r *Repository) setMemberDepartment(ctx context.Context, tenantID, principalID, departmentID string) error {
	if _, err := r.executor.NewDelete().Model((*memberDepartmentRow)(nil)).Where("tenant_id = ? AND principal_id = ?", tenantID, principalID).Exec(ctx); err != nil {
		return fmt.Errorf("clear member department: %w", err)
	}
	if departmentID == "" {
		return nil
	}
	row := memberDepartmentRow{TenantID: tenantID, PrincipalID: principalID, DepartmentID: departmentID, UpdatedAt: r.now().UTC().UnixMilli()}
	if _, err := r.executor.NewInsert().Model(&row).Exec(ctx); err != nil {
		return fmt.Errorf("set member department: %w", err)
	}
	return nil
}

func (r *Repository) departmentStatus(ctx context.Context, tenantID, departmentID string) (string, error) {
	var status string
	err := r.executor.NewSelect().Table("iam_departments").Column("status").Where("tenant_id = ? AND id = ?", tenantID, departmentID).Scan(ctx, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get department status: %w", err)
	}
	return status, nil
}

func (s *Service) GetRoleDataScope(ctx context.Context, tenantID, roleID string) (RoleDataScope, error) {
	if err := validateRoleRef(tenantID, roleID); err != nil {
		return RoleDataScope{}, err
	}
	return s.repo.GetRoleDataScope(ctx, tenantID, roleID)
}

func (s *Service) SetRoleDataScope(ctx context.Context, tenantID, roleID string, scope DataScope, departmentIDs []string) (RoleDataScope, bool, error) {
	if err := validateRoleRef(tenantID, roleID); err != nil {
		return RoleDataScope{}, false, err
	}
	departmentIDs, err := normalizeRoleDataScope(scope, departmentIDs)
	if err != nil {
		return RoleDataScope{}, false, err
	}
	var result RoleDataScope
	var changed bool
	record := newAuditRecord(ctx, tenantID, "role_data_scope_updated", "role", roleID)
	record.Detail["scope"] = scope
	record.Detail["department_count"] = len(departmentIDs)
	err = s.writes.Run(ctx, record, func(repo *Repository) error {
		current, err := repo.GetRoleDataScope(ctx, tenantID, roleID)
		if err != nil {
			return err
		}
		if current.Scope == scope && slices.Equal(current.DepartmentIDs, departmentIDs) {
			result = current
			record.SkipAudit = true
			return nil
		}
		for _, departmentID := range departmentIDs {
			status, err := repo.departmentStatus(ctx, tenantID, departmentID)
			if err != nil {
				return err
			}
			if status == "" {
				return ErrRoleScopeDepartmentMissing
			}
			if status != "active" {
				return ErrRoleScopeDepartmentInactive
			}
		}
		if err := repo.replaceRoleDataScope(ctx, tenantID, roleID, scope, departmentIDs); err != nil {
			return err
		}
		changed, record.PolicyChanged = true, true
		result, err = repo.GetRoleDataScope(ctx, tenantID, roleID)
		return err
	})
	return result, changed, err
}

func normalizeRoleDataScope(scope DataScope, departmentIDs []string) ([]string, error) {
	switch scope {
	case DataScopeAll, DataScopeSelf, DataScopeDepartment, DataScopeDepartmentAndDescendants, DataScopeSelectedDepartments:
	default:
		return nil, ErrInvalidRoleDataScope
	}
	set := make(map[string]struct{}, len(departmentIDs))
	for _, id := range departmentIDs {
		id = strings.TrimSpace(id)
		if id == "" || len(id) > 128 {
			return nil, ErrInvalidRoleDataScope
		}
		set[id] = struct{}{}
	}
	normalized := make([]string, 0, len(set))
	for id := range set {
		normalized = append(normalized, id)
	}
	sort.Strings(normalized)
	if scope == DataScopeSelectedDepartments && len(normalized) == 0 {
		return nil, ErrRoleScopeDepartmentNeeded
	}
	if scope != DataScopeSelectedDepartments && len(normalized) > 0 {
		return nil, ErrRoleScopeDepartmentsExtra
	}
	return normalized, nil
}
