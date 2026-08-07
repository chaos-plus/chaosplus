package iam

import (
	"context"
	"fmt"

	"github.com/chaos-plus/chaosplus/internal/core/extension/policyx"
	"github.com/uptrace/bun"
)

type dynamicGroupRuleRow struct {
	ID           string
	RuleJSON     string `bun:"rule_json"`
	Subject      string `bun:"subject"`
	Email        string
	DepartmentID string `bun:"department_id"`
	Status       string
}

func matchingDynamicGroupIDs(ctx context.Context, db bun.IDB, tenantID, subject string) ([]string, error) {
	rows := make([]dynamicGroupRuleRow, 0)
	if err := db.NewSelect().TableExpr("iam_groups AS g").
		ColumnExpr("g.id, g.rule_json, tm.user_subject AS subject, tm.email, COALESCE(md.department_id, '') AS department_id, tm.status").
		Join("JOIN iam_tenant_members AS tm ON tm.tenant_id = g.tenant_id AND tm.user_subject = ? AND tm.status = ?", subject, MemberActive).
		Join("LEFT JOIN iam_member_departments AS md ON md.tenant_id = tm.tenant_id AND md.principal_id = tm.user_subject").
		Where("g.tenant_id = ? AND g.group_type = 'dynamic' AND g.status = 'active'", tenantID).
		OrderExpr("g.id ASC").Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("list dynamic group candidates: %w", err)
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		matched, err := policyx.EvaluateMemberRule([]byte(row.RuleJSON), policyx.MemberFacts{Subject: row.Subject, Email: row.Email, DepartmentID: row.DepartmentID, Status: row.Status})
		if err != nil {
			return nil, fmt.Errorf("evaluate dynamic group %q rule: %w", row.ID, err)
		}
		if matched {
			ids = append(ids, row.ID)
		}
	}
	return ids, nil
}
