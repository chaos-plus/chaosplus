package iam

import (
	"context"
	"fmt"

	"github.com/uptrace/bun"

	iamdomain "github.com/chaos-plus/chaosplus/internal/modules/iam/domain"
)

// maxPageLimit bounds a single page so one request cannot pull an entire
// tenant into memory.
const maxPageLimit = 200

// normalizePage clamps caller-supplied paging. A limit of zero keeps the
// historical "return everything" contract for small collections, but callers
// that pass a limit never receive more than maxPageLimit rows.
func normalizePage(offset, limit int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if limit < 0 {
		limit = 0
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}
	return offset, limit
}

// applyPage pushes offset and limit into the query instead of slicing a fully
// materialized result set in Go.
func applyPage(query *bun.SelectQuery, offset, limit int) *bun.SelectQuery {
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

// ListEntitiesPage returns one page of tenant entities plus the total count.
// The previous path loaded every entity and sliced it in memory, which grows
// without bound as a tenant onboards more stores.
func (r *Repository) ListEntitiesPage(ctx context.Context, tenantID string, offset, limit int) ([]iamdomain.Entity, int64, error) {
	offset, limit = normalizePage(offset, limit)
	query := r.executor.NewSelect().Model((*entityRow)(nil)).Where("tenant_id = ?", tenantID)
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count entities: %w", err)
	}
	var rows []entityRow
	ordered := query.Order("parent_id ASC", "type ASC", "name ASC", "id ASC")
	if err := applyPage(ordered, offset, limit).Scan(ctx, &rows); err != nil {
		return nil, 0, fmt.Errorf("list entities: %w", err)
	}
	entities := make([]iamdomain.Entity, 0, len(rows))
	for _, row := range rows {
		entity, err := entityFromRow(row)
		if err != nil {
			return nil, 0, err
		}
		entities = append(entities, entity)
	}
	return entities, int64(total), nil
}

// ListRolesPage returns one page of tenant roles plus the total count.
func (r *Repository) ListRolesPage(ctx context.Context, tenantID string, offset, limit int) ([]Role, int64, error) {
	offset, limit = normalizePage(offset, limit)
	query := r.executor.NewSelect().Model((*roleRow)(nil)).Where("tenant_id = ?", tenantID)
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count roles: %w", err)
	}
	var rows []roleRow
	ordered := query.Order("name ASC", "id ASC")
	if err := applyPage(ordered, offset, limit).Scan(ctx, &rows); err != nil {
		return nil, 0, fmt.Errorf("list roles: %w", err)
	}
	roles := make([]Role, 0, len(rows))
	for _, row := range rows {
		roles = append(roles, roleFromRow(row))
	}
	return roles, int64(total), nil
}

// ListEntitiesPage exposes database-side entity paging to the API layer.
func (s *Service) ListEntitiesPage(ctx context.Context, tenantID string, offset, limit int) ([]iamdomain.Entity, int64, error) {
	if err := validateTenant(tenantID); err != nil {
		return nil, 0, err
	}
	return s.repo.ListEntitiesPage(ctx, tenantID, offset, limit)
}

// ListRolesPage exposes database-side role paging to the API layer.
func (s *Service) ListRolesPage(ctx context.Context, tenantID string, offset, limit int) ([]Role, int64, error) {
	if err := validateTenant(tenantID); err != nil {
		return nil, 0, err
	}
	return s.repo.ListRolesPage(ctx, tenantID, offset, limit)
}
