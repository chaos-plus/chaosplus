package iam

import (
	"context"
	"fmt"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

// RemoveGroupMembership removes a principal from a static group, severing every
// role that was derived through that group. It is used by access reviews to
// revoke derived access at its source.
func RemoveGroupMembership(ctx context.Context, db bun.IDB, tenantID, groupID, principalID guid.ID) (bool, error) {
	if db == nil || tenantID.Zero() || groupID.Zero() || principalID.Zero() {
		return false, fmt.Errorf("remove group membership: %w", ErrInvalidArgument)
	}
	result, err := db.NewDelete().Table("iam_group_members").
		Where("tenant_id = ? AND group_id = ? AND principal_id = ?", tenantID, groupID, principalID).Exec(ctx)
	if err != nil {
		return false, fmt.Errorf("remove group membership: %w", err)
	}
	affected, _ := result.RowsAffected()
	return affected > 0, nil
}

// RemovePositionMembership removes a principal from a position, severing every
// role that was derived through that position.
func RemovePositionMembership(ctx context.Context, db bun.IDB, tenantID, positionID, principalID guid.ID) (bool, error) {
	if db == nil || tenantID.Zero() || positionID.Zero() || principalID.Zero() {
		return false, fmt.Errorf("remove position membership: %w", ErrInvalidArgument)
	}
	result, err := db.NewDelete().Table("iam_position_members").
		Where("tenant_id = ? AND position_id = ? AND principal_id = ?", tenantID, positionID, principalID).Exec(ctx)
	if err != nil {
		return false, fmt.Errorf("remove position membership: %w", err)
	}
	affected, _ := result.RowsAffected()
	return affected > 0, nil
}

// RemoveEntityRoleBinding removes one entity-scoped role binding, severing the
// scoped permissions the principal held on that entity.
func RemoveEntityRoleBinding(ctx context.Context, db bun.IDB, tenantID, entityID, roleID, principalID guid.ID) (bool, error) {
	if db == nil || tenantID.Zero() || entityID.Zero() || roleID.Zero() || principalID.Zero() {
		return false, fmt.Errorf("remove entity role binding: %w", ErrInvalidArgument)
	}
	result, err := db.NewDelete().Table("iam_role_bindings").
		Where("tenant_id = ? AND scope_type = 'entity' AND scope_id = ? AND role_id = ? AND principal_id = ?", tenantID, entityID, roleID, principalID).Exec(ctx)
	if err != nil {
		return false, fmt.Errorf("remove entity role binding: %w", err)
	}
	affected, _ := result.RowsAffected()
	return affected > 0, nil
}
