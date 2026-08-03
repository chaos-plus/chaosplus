package organization

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/uptrace/bun"
)

type departmentRow struct {
	bun.BaseModel `bun:"table:iam_departments"`
	TenantID      string `bun:"tenant_id,pk"`
	ID            string `bun:"id,pk"`
	ParentID      string
	Name          string
	NameKey       string
	Status        string
	SortOrder     int
	Version       int64
	CreatedAt     int64
	UpdatedAt     int64
}

type closureRow struct {
	bun.BaseModel `bun:"table:iam_department_closure"`
	TenantID      string `bun:"tenant_id,pk"`
	AncestorID    string `bun:"ancestor_id,pk"`
	DescendantID  string `bun:"descendant_id,pk"`
	Depth         int
}

type Repository struct {
	db       *bun.DB
	executor bun.IDB
	dialect  string
}

func NewRepository(db *bun.DB) *Repository {
	if db == nil {
		panic("organization repository requires database")
	}
	dialect := db.Dialect().Name().String()
	if dialect == "pg" {
		dialect = "postgres"
	}
	return &Repository{db: db, executor: db, dialect: dialect}
}

func (r *Repository) withExecutor(executor bun.IDB) *Repository {
	return &Repository{db: r.db, executor: executor, dialect: r.dialect}
}

func countRelationshipSubject(ctx context.Context, db bun.IDB, tenantID, subjectType, subjectID string) (int, error) {
	entityCount, err := db.NewSelect().Table("iam_relationships").
		Where("tenant_id = ? AND subject_type = ? AND subject_id = ?", tenantID, subjectType, subjectID).
		Count(ctx)
	if err != nil {
		return 0, err
	}
	resourceCount, err := db.NewSelect().Table("iam_resource_relationships").
		Where("tenant_id = ? AND subject_type = ? AND subject_id = ?", tenantID, subjectType, subjectID).
		Count(ctx)
	return entityCount + resourceCount, err
}

func (r *Repository) list(ctx context.Context, tenantID string) ([]departmentRow, error) {
	rows := make([]departmentRow, 0)
	if err := r.executor.NewSelect().Model(&rows).Where("tenant_id = ?", tenantID).Scan(ctx); err != nil {
		return nil, fmt.Errorf("list departments: %w", err)
	}
	return rows, nil
}

func (r *Repository) get(ctx context.Context, tenantID, id string) (departmentRow, error) {
	var row departmentRow
	if err := r.executor.NewSelect().Model(&row).Where("tenant_id = ? AND id = ?", tenantID, id).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return departmentRow{}, ErrNotFound
		}
		return departmentRow{}, fmt.Errorf("get department: %w", err)
	}
	return row, nil
}

func (r *Repository) insert(ctx context.Context, row *departmentRow) error {
	if row.ParentID != "" {
		if _, err := r.get(ctx, row.TenantID, row.ParentID); err != nil {
			return err
		}
	}
	if _, err := r.executor.NewInsert().Model(row).Exec(ctx); err != nil {
		if isSiblingNameViolation(err) {
			return ErrNameConflict
		}
		return fmt.Errorf("insert department: %w", err)
	}
	self := closureRow{TenantID: row.TenantID, AncestorID: row.ID, DescendantID: row.ID, Depth: 0}
	if _, err := r.executor.NewInsert().Model(&self).Exec(ctx); err != nil {
		return fmt.Errorf("insert department self closure: %w", err)
	}
	if row.ParentID == "" {
		return nil
	}
	ancestors, err := r.ancestors(ctx, row.TenantID, row.ParentID)
	if err != nil {
		return err
	}
	links := make([]closureRow, 0, len(ancestors))
	for _, ancestor := range ancestors {
		links = append(links, closureRow{
			TenantID: row.TenantID, AncestorID: ancestor.AncestorID,
			DescendantID: row.ID, Depth: ancestor.Depth + 1,
		})
	}
	if len(links) > 0 {
		if _, err := r.executor.NewInsert().Model(&links).Exec(ctx); err != nil {
			return fmt.Errorf("insert department ancestor closures: %w", err)
		}
	}
	return nil
}

func (r *Repository) update(ctx context.Context, row *departmentRow, expectedVersion int64) error {
	result, err := r.executor.NewUpdate().Model(row).
		Column("parent_id", "name", "name_key", "status", "sort_order", "version", "updated_at").
		Where("tenant_id = ? AND id = ? AND version = ?", row.TenantID, row.ID, expectedVersion).
		Exec(ctx)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrNameConflict
		}
		return fmt.Errorf("update department: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrVersionConflict
	}
	return nil
}

func (r *Repository) move(ctx context.Context, tenantID, id, parentID string) error {
	subtree, err := r.descendants(ctx, tenantID, id)
	if err != nil {
		return err
	}
	oldAncestors, err := r.ancestors(ctx, tenantID, id)
	if err != nil {
		return err
	}
	subtreeIDs := make([]string, 0, len(subtree))
	for _, link := range subtree {
		subtreeIDs = append(subtreeIDs, link.DescendantID)
	}
	oldAncestorIDs := make([]string, 0, len(oldAncestors))
	for _, link := range oldAncestors {
		if link.AncestorID != id {
			oldAncestorIDs = append(oldAncestorIDs, link.AncestorID)
		}
	}
	if len(oldAncestorIDs) > 0 {
		if _, err := r.executor.NewDelete().Model((*closureRow)(nil)).
			Where("tenant_id = ?", tenantID).
			Where("ancestor_id IN (?)", bun.List(oldAncestorIDs)).
			Where("descendant_id IN (?)", bun.List(subtreeIDs)).Exec(ctx); err != nil {
			return fmt.Errorf("remove old department closures: %w", err)
		}
	}
	if parentID == "" {
		return nil
	}
	newAncestors, err := r.ancestors(ctx, tenantID, parentID)
	if err != nil {
		return err
	}
	links := make([]closureRow, 0, len(newAncestors)*len(subtree))
	for _, ancestor := range newAncestors {
		for _, descendant := range subtree {
			links = append(links, closureRow{
				TenantID: tenantID, AncestorID: ancestor.AncestorID,
				DescendantID: descendant.DescendantID,
				Depth:        ancestor.Depth + descendant.Depth + 1,
			})
		}
	}
	if len(links) > 0 {
		if _, err := r.executor.NewInsert().Model(&links).Exec(ctx); err != nil {
			return fmt.Errorf("insert moved department closures: %w", err)
		}
	}
	return nil
}

func (r *Repository) delete(ctx context.Context, tenantID, id string, version int64) error {
	count, err := r.executor.NewSelect().Model((*departmentRow)(nil)).
		Where("tenant_id = ? AND parent_id = ?", tenantID, id).Count(ctx)
	if err != nil {
		return fmt.Errorf("count department children: %w", err)
	}
	if count > 0 {
		return ErrHasChildren
	}
	for _, table := range []string{"iam_member_departments", "iam_role_scope_departments"} {
		count, err = r.executor.NewSelect().Table(table).Where("tenant_id = ? AND department_id = ?", tenantID, id).Count(ctx)
		if err != nil {
			return fmt.Errorf("check department references in %s: %w", table, err)
		}
		if count > 0 {
			return ErrInUse
		}
	}
	result, err := r.executor.NewDelete().Model((*departmentRow)(nil)).
		Where("tenant_id = ? AND id = ? AND version = ?", tenantID, id, version).Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete department: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrVersionConflict
	}
	return nil
}

func (r *Repository) isDescendant(ctx context.Context, tenantID, ancestorID, descendantID string) (bool, error) {
	count, err := r.executor.NewSelect().Model((*closureRow)(nil)).
		Where("tenant_id = ? AND ancestor_id = ? AND descendant_id = ?", tenantID, ancestorID, descendantID).
		Count(ctx)
	if err != nil {
		return false, fmt.Errorf("check department hierarchy: %w", err)
	}
	return count > 0, nil
}

func (r *Repository) ancestors(ctx context.Context, tenantID, descendantID string) ([]closureRow, error) {
	rows := make([]closureRow, 0)
	if err := r.executor.NewSelect().Model(&rows).
		Where("tenant_id = ? AND descendant_id = ?", tenantID, descendantID).
		Order("depth ASC", "ancestor_id ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list department ancestors: %w", err)
	}
	return rows, nil
}

func (r *Repository) descendants(ctx context.Context, tenantID, ancestorID string) ([]closureRow, error) {
	rows := make([]closureRow, 0)
	if err := r.executor.NewSelect().Model(&rows).
		Where("tenant_id = ? AND ancestor_id = ?", tenantID, ancestorID).
		Order("depth ASC", "descendant_id ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list department descendants: %w", err)
	}
	return rows, nil
}

func orderDepartments(rows []departmentRow) ([]Department, error) {
	children := make(map[string][]departmentRow, len(rows))
	for _, row := range rows {
		children[row.ParentID] = append(children[row.ParentID], row)
	}
	for parentID := range children {
		sort.Slice(children[parentID], func(i, j int) bool {
			left, right := children[parentID][i], children[parentID][j]
			if left.SortOrder != right.SortOrder {
				return left.SortOrder < right.SortOrder
			}
			if left.NameKey != right.NameKey {
				return left.NameKey < right.NameKey
			}
			return left.ID < right.ID
		})
	}
	result := make([]Department, 0, len(rows))
	visited := make(map[string]bool, len(rows))
	var visit func(string, int)
	visit = func(parentID string, depth int) {
		for _, row := range children[parentID] {
			if visited[row.ID] {
				continue
			}
			visited[row.ID] = true
			result = append(result, departmentFromRow(row, depth))
			visit(row.ID, depth+1)
		}
	}
	visit("", 0)
	if len(result) != len(rows) {
		return nil, ErrHierarchyCorrupt
	}
	return result, nil
}

func departmentFromRow(row departmentRow, depth int) Department {
	return Department{
		ID: row.ID, TenantID: row.TenantID, ParentID: row.ParentID, Name: row.Name,
		Status: row.Status, SortOrder: row.SortOrder, Depth: depth, Version: row.Version,
		CreatedAt: unixTime(row.CreatedAt), UpdatedAt: unixTime(row.UpdatedAt),
	}
}

func isUniqueViolation(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint") || strings.Contains(message, "duplicate entry") || strings.Contains(message, "duplicate key")
}

func isSiblingNameViolation(err error) bool {
	message := strings.ToLower(err.Error())
	return isUniqueViolation(err) && (strings.Contains(message, "sibling_name") || (strings.Contains(message, "parent_id") && strings.Contains(message, "name_key")))
}
