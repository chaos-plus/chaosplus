package iam

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

// MembershipChecker is the fail-closed tenant admission gate used before every
// authorization check. It intentionally has no write capabilities.
type MembershipChecker struct{ db *bun.DB }

func NewMembershipChecker(db *bun.DB) *MembershipChecker {
	if db == nil {
		panic("iam membership checker requires database")
	}
	return &MembershipChecker{db: db}
}

func (m *MembershipChecker) IsMemberActive(ctx context.Context, tenantID, principalID guid.ID) (bool, error) {
	return m.IsMemberActiveOn(ctx, m.db, tenantID, principalID)
}

func (m *MembershipChecker) IsMemberActiveOn(ctx context.Context, executor bun.IDB, tenantID, principalID guid.ID) (bool, error) {
	_, active, err := tenantMembershipState(ctx, executor, tenantID, principalID)
	return active, err
}

type tenantAdmission struct {
	Status      string `bun:"status"`
	MemberCount int    `bun:"member_count"`
}

func (m *MembershipChecker) stateOn(ctx context.Context, executor bun.IDB, tenantID, principalID guid.ID) (tenantActive, memberActive bool, err error) {
	return tenantMembershipState(ctx, executor, tenantID, principalID)
}

func tenantMembershipState(ctx context.Context, executor bun.IDB, tenantID, principalID guid.ID) (tenantActive, memberActive bool, err error) {
	var state tenantAdmission
	err = executor.NewSelect().TableExpr("iam_tenants AS t").
		ColumnExpr("t.status").
		ColumnExpr("COUNT(tm.principal_id) AS member_count").
		Join("LEFT JOIN iam_tenant_members AS tm ON tm.tenant_id = t.id AND tm.principal_id = ? AND tm.status = ?", principalID, MemberActive).
		Where("t.id = ?", tenantID).
		Group("t.status").
		Scan(ctx, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("check active tenant membership: %w", err)
	}
	tenantActive = state.Status == "active"
	return tenantActive, tenantActive && state.MemberCount == 1, nil
}
