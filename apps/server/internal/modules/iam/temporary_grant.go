package iam

import (
	"context"
	"fmt"
	"time"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

type temporaryRoleGrantRow struct {
	bun.BaseModel `bun:"table:iam_temporary_role_grants"`
	TenantID      guid.ID `bun:"tenant_id,pk"`
	ID            guid.ID `bun:"id,pk"`
	RoleID        guid.ID `bun:"role_id"`
	PrincipalID   guid.ID `bun:"principal_id"`
	SourceType    string  `bun:"source_type"`
	SourceID      guid.ID `bun:"source_id"`
	StartsAt      int64   `bun:"starts_at"`
	EndsAt        int64   `bun:"ends_at"`
	CreatedBy     guid.ID `bun:"created_by"`
	CreatedAt     int64   `bun:"created_at"`
}

func GrantTemporaryRole(ctx context.Context, db bun.IDB, tenantID, grantID, roleID, principalID, actorID guid.ID, startsAt, endsAt time.Time) error {
	if db == nil || tenantID.Zero() || grantID.Zero() || roleID.Zero() || principalID.Zero() || actorID.Zero() || !endsAt.After(startsAt) {
		return fmt.Errorf("invalid temporary role grant")
	}
	if err := ensureRole(ctx, db, tenantID, roleID); err != nil {
		return err
	}
	_, active, err := tenantMembershipState(ctx, db, tenantID, principalID)
	if err != nil {
		return err
	}
	if !active {
		return ErrMemberInactive
	}
	now := time.Now().UTC().UnixMilli()
	row := temporaryRoleGrantRow{
		TenantID: tenantID, ID: grantID, RoleID: roleID, PrincipalID: principalID,
		SourceType: "access_request", SourceID: grantID, StartsAt: startsAt.UTC().UnixMilli(), EndsAt: endsAt.UTC().UnixMilli(),
		CreatedBy: actorID, CreatedAt: now,
	}
	if _, err := db.NewInsert().Model(&row).Exec(ctx); err != nil {
		return fmt.Errorf("insert temporary role grant: %w", err)
	}
	return nil
}

func RevokeTemporaryRole(ctx context.Context, db bun.IDB, tenantID, grantID guid.ID) (bool, error) {
	if db == nil || tenantID.Zero() || grantID.Zero() {
		return false, fmt.Errorf("invalid temporary role grant")
	}
	result, err := db.NewDelete().Model((*temporaryRoleGrantRow)(nil)).Where("tenant_id = ? AND id = ?", tenantID, grantID).Exec(ctx)
	if err != nil {
		return false, fmt.Errorf("delete temporary role grant: %w", err)
	}
	affected, _ := result.RowsAffected()
	return affected > 0, nil
}
