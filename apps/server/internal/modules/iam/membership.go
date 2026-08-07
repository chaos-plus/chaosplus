package iam

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

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

func (m *MembershipChecker) IsMemberActive(ctx context.Context, tenantID, subject string) (bool, error) {
	return m.IsMemberActiveOn(ctx, m.db, tenantID, subject)
}

func (m *MembershipChecker) IsMemberActiveOn(ctx context.Context, executor bun.IDB, tenantID, subject string) (bool, error) {
	_, active, err := tenantMembershipState(ctx, executor, tenantID, subject)
	return active, err
}

type tenantAdmission struct {
	Status      string `bun:"status"`
	MemberCount int    `bun:"member_count"`
}

func (m *MembershipChecker) stateOn(ctx context.Context, executor bun.IDB, tenantID, subject string) (tenantActive, memberActive bool, err error) {
	return tenantMembershipState(ctx, executor, tenantID, subject)
}

func tenantMembershipState(ctx context.Context, executor bun.IDB, tenantID, subject string) (tenantActive, memberActive bool, err error) {
	var state tenantAdmission
	err = executor.NewSelect().TableExpr("iam_tenants AS t").
		ColumnExpr("t.status").
		ColumnExpr("COUNT(tm.user_subject) AS member_count").
		Join("LEFT JOIN iam_tenant_members AS tm ON tm.tenant_id = t.id AND tm.user_subject = ? AND tm.status = ?", subject, MemberActive).
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
