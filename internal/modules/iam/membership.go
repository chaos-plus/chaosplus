package iam

import (
	"context"
	"fmt"

	"github.com/uptrace/bun"
)

// MembershipChecker is the fail-closed tenant admission gate used before every
// SpiceDB permission check. It intentionally has no write capabilities.
type MembershipChecker struct{ db *bun.DB }

func NewMembershipChecker(db *bun.DB) *MembershipChecker {
	if db == nil {
		panic("iam membership checker requires database")
	}
	return &MembershipChecker{db: db}
}

func (m *MembershipChecker) IsMemberActive(ctx context.Context, tenantID, subject string) (bool, error) {
	count, err := m.db.NewSelect().Model((*tenantMemberRow)(nil)).Where("tenant_id = ? AND user_subject = ? AND status = ?", tenantID, subject, MemberActive).Count(ctx)
	if err != nil {
		return false, fmt.Errorf("check active tenant membership: %w", err)
	}
	return count == 1, nil
}
