package iam

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/policyx"
	"github.com/uptrace/bun"
)

// AdministratorGuard preserves a recoverable tenant administration path
// across direct, group, and position role assignments.
type AdministratorGuard struct {
	now func() time.Time
}

func NewAdministratorGuard() *AdministratorGuard {
	return &AdministratorGuard{now: time.Now}
}

// RemoveRoleMember removes the exact role membership captured by a caller's
// transaction while preserving at least one durable tenant administrator.
func (g *AdministratorGuard) RemoveRoleMember(ctx context.Context, db bun.IDB, dialect, tenantID, roleID, principalID string, createdAt int64) (bool, error) {
	if db == nil || strings.TrimSpace(roleID) == "" || strings.TrimSpace(principalID) == "" || createdAt < 0 {
		return false, fmt.Errorf("invalid role membership removal")
	}
	verify, err := g.Protect(ctx, db, dialect, tenantID)
	if err != nil {
		return false, err
	}
	result, err := db.NewDelete().Table("iam_role_members").
		Where("tenant_id = ? AND role_id = ? AND user_subject = ? AND created_at = ?", tenantID, roleID, principalID, createdAt).
		Exec(ctx)
	if err != nil {
		return false, fmt.Errorf("delete reviewed role member: %w", err)
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return false, nil
	}
	if err := verify(); err != nil {
		return false, err
	}
	return true, nil
}

// Protect locks one tenant and snapshots whether it currently has a durable
// administrator. The returned verifier must run after the mutation and before
// the transaction commits.
func (g *AdministratorGuard) Protect(ctx context.Context, db bun.IDB, dialect, tenantID string) (func() error, error) {
	if g == nil || g.now == nil || db == nil || strings.TrimSpace(tenantID) == "" {
		return nil, fmt.Errorf("invalid tenant administrator guard configuration")
	}
	if err := policyx.Lock(ctx, db, dialect, tenantID); err != nil {
		return nil, err
	}
	hadAdministrator, err := g.hasDurableAdministrator(ctx, db, tenantID)
	if err != nil {
		return nil, err
	}
	return func() error {
		if !hadAdministrator {
			return nil
		}
		hasAdministrator, err := g.hasDurableAdministrator(ctx, db, tenantID)
		if err != nil {
			return err
		}
		if !hasAdministrator {
			return ErrLastTenantAdministrator
		}
		return nil
	}, nil
}

func (g *AdministratorGuard) hasDurableAdministrator(ctx context.Context, db bun.IDB, tenantID string) (bool, error) {
	var count int64
	now := g.now().UTC().UnixMilli()
	err := db.NewRaw(`
SELECT COUNT(DISTINCT administrators.principal_id)
FROM (
    SELECT members.user_subject AS principal_id
    FROM iam_role_members AS members
    JOIN iam_role_permissions AS permissions
      ON permissions.tenant_id = members.tenant_id AND permissions.role_id = members.role_id
    WHERE members.tenant_id = ? AND permissions.permission_code IN (?, ?) AND permissions.condition_json = ''
    UNION
    SELECT group_members.principal_id
    FROM iam_group_role_bindings AS bindings
    JOIN iam_role_permissions AS permissions
      ON permissions.tenant_id = bindings.tenant_id AND permissions.role_id = bindings.role_id
    JOIN iam_groups AS grp
      ON grp.tenant_id = bindings.tenant_id AND grp.id = bindings.group_id AND grp.status = 'active' AND grp.group_type = 'static'
    JOIN iam_group_members AS group_members
      ON group_members.tenant_id = bindings.tenant_id AND group_members.group_id = bindings.group_id
    WHERE bindings.tenant_id = ? AND permissions.permission_code IN (?, ?) AND permissions.condition_json = ''
      AND (group_members.starts_at = 0 OR group_members.starts_at <= ?)
      AND group_members.ends_at = 0
    UNION
    SELECT position_members.principal_id
    FROM iam_position_role_bindings AS bindings
    JOIN iam_role_permissions AS permissions
      ON permissions.tenant_id = bindings.tenant_id AND permissions.role_id = bindings.role_id
    JOIN iam_positions AS positions
      ON positions.tenant_id = bindings.tenant_id AND positions.id = bindings.position_id AND positions.status = 'active'
    JOIN iam_position_members AS position_members
      ON position_members.tenant_id = bindings.tenant_id AND position_members.position_id = bindings.position_id
    WHERE bindings.tenant_id = ? AND permissions.permission_code IN (?, ?) AND permissions.condition_json = ''
      AND (position_members.starts_at = 0 OR position_members.starts_at <= ?)
      AND position_members.ends_at = 0
) AS administrators
JOIN iam_principals AS principals
  ON principals.id = administrators.principal_id AND principals.status = 'active'
JOIN iam_tenant_members AS tenant_members
  ON tenant_members.tenant_id = ? AND tenant_members.user_subject = administrators.principal_id AND tenant_members.status = 'active'`,
		tenantID, "tenant_administer", "platform_administer",
		tenantID, "tenant_administer", "platform_administer", now,
		tenantID, "tenant_administer", "platform_administer", now,
		tenantID,
	).Scan(ctx, &count)
	if err != nil {
		return false, fmt.Errorf("count durable tenant administrators: %w", err)
	}
	return count > 0, nil
}
