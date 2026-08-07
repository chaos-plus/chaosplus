// Package authzsql applies structured authorization constraints to business
// queries without accepting caller-provided SQL fragments.
package authzsql

import (
	"github.com/uptrace/bun"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
)

// ApplyEntityConstraint enforces the standard tenant_id/entity_id ownership
// columns used by business resources below tenant -> entity.
func ApplyEntityConstraint(query *bun.SelectQuery, tenantID string, constraint authz.DataConstraint) *bun.SelectQuery {
	query = query.Where("tenant_id = ?", tenantID)
	if !constraint.AllowAll {
		if len(constraint.ResourceIDs) == 0 {
			return query.Where("1 = 0")
		}
		query = query.Where("entity_id IN (?)", bun.List(constraint.ResourceIDs))
	}
	if len(constraint.DeniedIDs) > 0 {
		query = query.Where("entity_id NOT IN (?)", bun.List(constraint.DeniedIDs))
	}
	return query
}
