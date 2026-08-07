package iam

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/uptrace/bun"
)

type temporaryRoleGrantRow struct {
	bun.BaseModel `bun:"table:iam_temporary_role_grants"`
	TenantID      string `bun:"tenant_id,pk"`
	ID            string `bun:"id,pk"`
	RoleID        string `bun:"role_id"`
	PrincipalID   string `bun:"principal_id"`
	SourceType    string `bun:"source_type"`
	SourceID      string `bun:"source_id"`
	StartsAt      int64  `bun:"starts_at"`
	EndsAt        int64  `bun:"ends_at"`
	CreatedBy     string `bun:"created_by"`
	CreatedAt     int64  `bun:"created_at"`
}

func GrantTemporaryRole(ctx context.Context, db bun.IDB, tenantID, grantID, roleID, principalID, actorID string, startsAt, endsAt time.Time) error {
	if db == nil || !validTemporaryGrantID(tenantID, 128) || !validTemporaryGrantID(grantID, 64) || !validTemporaryGrantID(roleID, 32) ||
		!validTemporaryGrantID(principalID, 255) || !validTemporaryGrantID(actorID, 255) || !endsAt.After(startsAt) {
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

func RevokeTemporaryRole(ctx context.Context, db bun.IDB, tenantID, grantID string) (bool, error) {
	if db == nil || !validTemporaryGrantID(tenantID, 128) || !validTemporaryGrantID(grantID, 64) {
		return false, fmt.Errorf("invalid temporary role grant")
	}
	result, err := db.NewDelete().Model((*temporaryRoleGrantRow)(nil)).Where("tenant_id = ? AND id = ?", tenantID, grantID).Exec(ctx)
	if err != nil {
		return false, fmt.Errorf("delete temporary role grant: %w", err)
	}
	affected, _ := result.RowsAffected()
	return affected > 0, nil
}

func validTemporaryGrantID(value string, limit int) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= limit
}
