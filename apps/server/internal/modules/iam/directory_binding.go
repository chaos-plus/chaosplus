package iam

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

type groupRoleBindingRow struct {
	bun.BaseModel `bun:"table:iam_group_role_bindings"`
	TenantID      guid.ID `bun:"tenant_id,pk"`
	RoleID        guid.ID `bun:"role_id,pk"`
	GroupID       guid.ID `bun:"group_id,pk"`
	CreatedAt     int64
}

type positionRoleBindingRow struct {
	bun.BaseModel `bun:"table:iam_position_role_bindings"`
	TenantID      guid.ID `bun:"tenant_id,pk"`
	RoleID        guid.ID `bun:"role_id,pk"`
	PositionID    guid.ID `bun:"position_id,pk"`
	CreatedAt     int64
}

type directoryStatusRow struct {
	Status string `bun:"status"`
}

func (r *Repository) ListDirectoryBindings(ctx context.Context, tenantID, roleID guid.ID) ([]RoleDirectoryBinding, error) {
	if err := ensureRole(ctx, r.executor, tenantID, roleID); err != nil {
		return nil, err
	}
	bindings := make([]RoleDirectoryBinding, 0)
	var groups []groupRoleBindingRow
	if err := r.executor.NewSelect().Model(&groups).Where("tenant_id = ? AND role_id = ?", tenantID, roleID).Scan(ctx); err != nil {
		return nil, fmt.Errorf("list group role bindings: %w", err)
	}
	for _, row := range groups {
		bindings = append(bindings, RoleDirectoryBinding{RoleID: row.RoleID, AssigneeType: DirectoryAssigneeGroup, AssigneeID: row.GroupID, CreatedAt: time.UnixMilli(row.CreatedAt).UTC()})
	}
	var positions []positionRoleBindingRow
	if err := r.executor.NewSelect().Model(&positions).Where("tenant_id = ? AND role_id = ?", tenantID, roleID).Scan(ctx); err != nil {
		return nil, fmt.Errorf("list position role bindings: %w", err)
	}
	for _, row := range positions {
		bindings = append(bindings, RoleDirectoryBinding{RoleID: row.RoleID, AssigneeType: DirectoryAssigneePosition, AssigneeID: row.PositionID, CreatedAt: time.UnixMilli(row.CreatedAt).UTC()})
	}
	sort.Slice(bindings, func(i, j int) bool {
		if bindings[i].AssigneeType == bindings[j].AssigneeType {
			return bindings[i].AssigneeID < bindings[j].AssigneeID
		}
		return bindings[i].AssigneeType < bindings[j].AssigneeType
	})
	return bindings, nil
}

func (r *Repository) ChangeDirectoryBinding(ctx context.Context, tenantID, roleID guid.ID, assigneeType DirectoryAssigneeType, assigneeID guid.ID, add bool) (bool, error) {
	var changed bool
	err := r.runInTx(ctx, func(ctx context.Context, tx bun.IDB) error {
		if err := ensureRole(ctx, tx, tenantID, roleID); err != nil {
			return err
		}
		status, err := directoryAssigneeStatus(ctx, tx, tenantID, assigneeType, assigneeID)
		if err != nil {
			return err
		}
		if add && status != "active" {
			return ErrDirectoryAssigneeInactive
		}
		now := r.now().UTC().UnixMilli()
		switch assigneeType {
		case DirectoryAssigneeGroup:
			if add {
				result, err := tx.NewInsert().Model(&groupRoleBindingRow{TenantID: tenantID, RoleID: roleID, GroupID: assigneeID, CreatedAt: now}).Ignore().Exec(ctx)
				if err != nil {
					return fmt.Errorf("insert group role binding: %w", err)
				}
				affected, _ := result.RowsAffected()
				changed = affected > 0
				return nil
			}
			result, err := tx.NewDelete().Model((*groupRoleBindingRow)(nil)).Where("tenant_id = ? AND role_id = ? AND group_id = ?", tenantID, roleID, assigneeID).Exec(ctx)
			if err != nil {
				return fmt.Errorf("delete group role binding: %w", err)
			}
			affected, _ := result.RowsAffected()
			changed = affected > 0
		case DirectoryAssigneePosition:
			if add {
				result, err := tx.NewInsert().Model(&positionRoleBindingRow{TenantID: tenantID, RoleID: roleID, PositionID: assigneeID, CreatedAt: now}).Ignore().Exec(ctx)
				if err != nil {
					return fmt.Errorf("insert position role binding: %w", err)
				}
				affected, _ := result.RowsAffected()
				changed = affected > 0
				return nil
			}
			result, err := tx.NewDelete().Model((*positionRoleBindingRow)(nil)).Where("tenant_id = ? AND role_id = ? AND position_id = ?", tenantID, roleID, assigneeID).Exec(ctx)
			if err != nil {
				return fmt.Errorf("delete position role binding: %w", err)
			}
			affected, _ := result.RowsAffected()
			changed = affected > 0
		default:
			return fmt.Errorf("%w: invalid directory assignee type", ErrInvalidArgument)
		}
		return nil
	})
	return changed, err
}

func directoryAssigneeStatus(ctx context.Context, db bun.IDB, tenantID guid.ID, assigneeType DirectoryAssigneeType, assigneeID guid.ID) (string, error) {
	var row directoryStatusRow
	var err error
	switch assigneeType {
	case DirectoryAssigneeGroup:
		err = db.NewSelect().Table("iam_groups").Column("status").Where("tenant_id = ? AND id = ?", tenantID, assigneeID).Scan(ctx, &row)
	case DirectoryAssigneePosition:
		err = db.NewSelect().Table("iam_positions").Column("status").Where("tenant_id = ? AND id = ?", tenantID, assigneeID).Scan(ctx, &row)
	default:
		return "", fmt.Errorf("%w: invalid directory assignee type", ErrInvalidArgument)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrDirectoryAssigneeNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get directory assignee: %w", err)
	}
	return row.Status, nil
}

func (s *Service) ListDirectoryBindings(ctx context.Context, tenantID, roleID guid.ID) ([]RoleDirectoryBinding, error) {
	if err := validateRoleRef(tenantID, roleID); err != nil {
		return nil, err
	}
	return s.repo.ListDirectoryBindings(ctx, tenantID, roleID)
}

func (s *Service) AddDirectoryBinding(ctx context.Context, tenantID, roleID guid.ID, assigneeType DirectoryAssigneeType, assigneeID guid.ID) (bool, error) {
	return s.changeDirectoryBinding(ctx, tenantID, roleID, assigneeType, assigneeID, true)
}

func (s *Service) RemoveDirectoryBinding(ctx context.Context, tenantID, roleID guid.ID, assigneeType DirectoryAssigneeType, assigneeID guid.ID) (bool, error) {
	return s.changeDirectoryBinding(ctx, tenantID, roleID, assigneeType, assigneeID, false)
}

func (s *Service) changeDirectoryBinding(ctx context.Context, tenantID, roleID guid.ID, assigneeType DirectoryAssigneeType, assigneeID guid.ID, add bool) (bool, error) {
	if err := validateDirectoryBindingRef(tenantID, roleID, assigneeType, assigneeID); err != nil {
		return false, err
	}
	if add {
		if err := s.requireGrantableRole(ctx, tenantID, roleID); err != nil {
			return false, err
		}
	}
	eventType := "role_directory_binding_removed"
	if add {
		eventType = "role_directory_binding_added"
	}
	record := newAuditRecord(ctx, tenantID, eventType, "role", roleID)
	record.Detail["assignee_type"] = assigneeType
	record.Detail["assignee_id"] = assigneeID
	var changed bool
	err := s.writes.Run(ctx, record, func(repo *Repository) error {
		var verify func() error
		var err error
		if !add {
			verify, err = s.administrators.Protect(ctx, repo.executor, repo.dialect, tenantID)
			if err != nil {
				return err
			}
		}
		changed, err = repo.ChangeDirectoryBinding(ctx, tenantID, roleID, assigneeType, assigneeID, add)
		if err == nil && verify != nil {
			err = verify()
		}
		record.PolicyChanged = changed
		record.SkipAudit = !changed
		return err
	})
	if err != nil {
		return false, err
	}
	return changed, err
}

func validateDirectoryBindingRef(tenantID, roleID guid.ID, assigneeType DirectoryAssigneeType, assigneeID guid.ID) error {
	if err := validateRoleRef(tenantID, roleID); err != nil {
		return err
	}
	if assigneeType != DirectoryAssigneeGroup && assigneeType != DirectoryAssigneePosition {
		return fmt.Errorf("%w: invalid directory assignee type", ErrInvalidArgument)
	}
	if assigneeID.Zero() {
		return fmt.Errorf("%w: invalid directory assignee id", ErrInvalidArgument)
	}
	return nil
}
