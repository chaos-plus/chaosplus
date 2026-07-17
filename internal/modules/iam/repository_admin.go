package iam

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/uptrace/bun"
)

type tenantMemberRow struct {
	bun.BaseModel `bun:"table:iam_tenant_members"`
	TenantID      string `bun:"tenant_id,pk"`
	UserSubject   string `bun:"user_subject,pk"`
	DisplayName   string
	Email         string
	Status        string
	CreatedAt     int64
	UpdatedAt     int64
	DisabledAt    int64
}

type menuRow struct {
	bun.BaseModel  `bun:"table:iam_menus"`
	TenantID       string  `bun:"tenant_id,pk"`
	ID             string  `bun:"id,pk"`
	ParentID       *string `bun:"parent_id"`
	Label          string
	Route          *string
	Icon           string
	SortOrder      int
	PermissionCode string
	Status         string
	CreatedAt      int64
	UpdatedAt      int64
}

func (r *Repository) PutMember(ctx context.Context, member TenantMember) (TenantMember, error) {
	now := r.now().UTC().UnixMilli()
	disabledAt := int64(0)
	if member.Status == MemberDisabled {
		disabledAt = now
	}
	query := memberUpsertSQL(r.dialect)
	if query == "" {
		return TenantMember{}, fmt.Errorf("unsupported iam database dialect %q", r.dialect)
	}
	if _, err := r.db.ExecContext(ctx, query, member.TenantID, member.Subject, member.DisplayName, member.Email, member.Status, now, now, disabledAt); err != nil {
		return TenantMember{}, fmt.Errorf("upsert tenant member: %w", err)
	}
	return r.GetMember(ctx, member.TenantID, member.Subject)
}

func memberUpsertSQL(dialect string) string {
	const insert = `INSERT INTO iam_tenant_members
 (tenant_id, user_subject, display_name, email, status, created_at, updated_at, disabled_at)
 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	if dialect == "mysql" {
		return insert + ` ON DUPLICATE KEY UPDATE display_name = VALUES(display_name), email = VALUES(email), status = VALUES(status), updated_at = VALUES(updated_at), disabled_at = VALUES(disabled_at)`
	}
	if dialect == "sqlite" || dialect == "postgres" {
		return insert + ` ON CONFLICT (tenant_id, user_subject) DO UPDATE SET display_name = excluded.display_name, email = excluded.email, status = excluded.status, updated_at = excluded.updated_at, disabled_at = excluded.disabled_at`
	}
	return ""
}

func (r *Repository) GetMember(ctx context.Context, tenantID, subject string) (TenantMember, error) {
	var row tenantMemberRow
	if err := r.db.NewSelect().Model(&row).Where("tenant_id = ? AND user_subject = ?", tenantID, subject).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TenantMember{}, ErrMemberNotFound
		}
		return TenantMember{}, fmt.Errorf("get tenant member: %w", err)
	}
	return memberFromRow(row), nil
}

func (r *Repository) ListMembersPage(ctx context.Context, tenantID string, filter MemberFilter) ([]TenantMember, int64, error) {
	query := r.db.NewSelect().Model((*tenantMemberRow)(nil)).Where("tenant_id = ?", tenantID)
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if search := strings.TrimSpace(filter.Search); search != "" {
		like := "%" + strings.ToLower(search) + "%"
		query = query.Where("(LOWER(display_name) LIKE ? OR LOWER(email) LIKE ? OR LOWER(user_subject) LIKE ?)", like, like, like)
	}
	count, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count tenant members: %w", err)
	}
	var rows []tenantMemberRow
	if err := query.Order("display_name ASC", "user_subject ASC").Offset(filter.Offset).Limit(filter.Limit).Scan(ctx, &rows); err != nil {
		return nil, 0, fmt.Errorf("list tenant members: %w", err)
	}
	members := make([]TenantMember, 0, len(rows))
	for _, row := range rows {
		members = append(members, memberFromRow(row))
	}
	return members, int64(count), nil
}

func (r *Repository) SetMemberStatus(ctx context.Context, tenantID, subject string, status MemberStatus) (TenantMember, error) {
	member, err := r.GetMember(ctx, tenantID, subject)
	if err != nil {
		return TenantMember{}, err
	}
	member.Status = status
	return r.PutMember(ctx, member)
}

func (r *Repository) IsMemberActive(ctx context.Context, tenantID, subject string) (bool, error) {
	count, err := r.db.NewSelect().Model((*tenantMemberRow)(nil)).Where("tenant_id = ? AND user_subject = ? AND status = ?", tenantID, subject, MemberActive).Count(ctx)
	if err != nil {
		return false, fmt.Errorf("check tenant member: %w", err)
	}
	return count == 1, nil
}

func (r *Repository) ListMemberRoleIDs(ctx context.Context, tenantID, subject string) ([]string, error) {
	ids := make([]string, 0)
	if err := r.db.NewSelect().Model((*memberRow)(nil)).Column("role_id").Where("tenant_id = ? AND user_subject = ?", tenantID, subject).Order("role_id ASC").Scan(ctx, &ids); err != nil {
		return nil, fmt.Errorf("list tenant member roles: %w", err)
	}
	return ids, nil
}

func (r *Repository) CreateMenu(ctx context.Context, menu Menu) (Menu, error) {
	id, err := r.nextID()
	if err != nil {
		return Menu{}, fmt.Errorf("generate menu id: %w", err)
	}
	now := r.now().UTC().UnixMilli()
	row := menuToRow(menu)
	row.ID, row.CreatedAt, row.UpdatedAt = id, now, now
	if _, err := r.db.NewInsert().Model(&row).Exec(ctx); err != nil {
		if isUniqueViolation(err) {
			return Menu{}, ErrMenuConflict
		}
		return Menu{}, fmt.Errorf("insert menu: %w", err)
	}
	return menuFromRow(row), nil
}

func (r *Repository) GetMenu(ctx context.Context, tenantID, menuID string) (Menu, error) {
	var row menuRow
	if err := r.db.NewSelect().Model(&row).Where("tenant_id = ? AND id = ?", tenantID, menuID).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Menu{}, ErrMenuNotFound
		}
		return Menu{}, fmt.Errorf("get menu: %w", err)
	}
	return menuFromRow(row), nil
}

func (r *Repository) ListMenus(ctx context.Context, tenantID string, activeOnly bool) ([]Menu, error) {
	query := r.db.NewSelect().Model((*menuRow)(nil)).Where("tenant_id = ?", tenantID)
	if activeOnly {
		query = query.Where("status = ?", MenuActive)
	}
	var rows []menuRow
	if err := query.Order("sort_order ASC", "id ASC").Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("list menus: %w", err)
	}
	menus := make([]Menu, 0, len(rows))
	for _, row := range rows {
		menus = append(menus, menuFromRow(row))
	}
	return menus, nil
}

func (r *Repository) UpdateMenu(ctx context.Context, menu Menu) (Menu, error) {
	now := r.now().UTC().UnixMilli()
	row := menuToRow(menu)
	result, err := r.db.NewUpdate().Model(&row).Column("parent_id", "label", "route", "icon", "sort_order", "permission_code", "status").
		Set("updated_at = ?", now).WherePK().Exec(ctx)
	if err != nil {
		if isUniqueViolation(err) {
			return Menu{}, ErrMenuConflict
		}
		return Menu{}, fmt.Errorf("update menu: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return Menu{}, ErrMenuNotFound
	}
	return r.GetMenu(ctx, menu.TenantID, menu.ID)
}

func (r *Repository) DeleteMenu(ctx context.Context, tenantID, menuID string) error {
	children, err := r.db.NewSelect().Model((*menuRow)(nil)).Where("tenant_id = ? AND parent_id = ?", tenantID, menuID).Count(ctx)
	if err != nil {
		return fmt.Errorf("count menu children: %w", err)
	}
	if children > 0 {
		return ErrMenuHasChildren
	}
	result, err := r.db.NewDelete().Model((*menuRow)(nil)).Where("tenant_id = ? AND id = ?", tenantID, menuID).Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete menu: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrMenuNotFound
	}
	return nil
}

func memberFromRow(row tenantMemberRow) TenantMember {
	member := TenantMember{TenantID: row.TenantID, Subject: row.UserSubject, DisplayName: row.DisplayName, Email: row.Email, Status: MemberStatus(row.Status), CreatedAt: time.UnixMilli(row.CreatedAt).UTC(), UpdatedAt: time.UnixMilli(row.UpdatedAt).UTC()}
	if row.DisabledAt > 0 {
		member.DisabledAt = time.UnixMilli(row.DisabledAt).UTC()
	}
	return member
}

func menuToRow(menu Menu) menuRow {
	var parentID, route *string
	if menu.ParentID != "" {
		parentID = &menu.ParentID
	}
	if menu.Route != "" {
		route = &menu.Route
	}
	return menuRow{TenantID: menu.TenantID, ID: menu.ID, ParentID: parentID, Label: menu.Label, Route: route, Icon: menu.Icon, SortOrder: menu.SortOrder, PermissionCode: menu.PermissionCode, Status: string(menu.Status)}
}

func menuFromRow(row menuRow) Menu {
	menu := Menu{ID: row.ID, TenantID: row.TenantID, Label: row.Label, Icon: row.Icon, SortOrder: row.SortOrder, PermissionCode: row.PermissionCode, Status: MenuStatus(row.Status), CreatedAt: time.UnixMilli(row.CreatedAt).UTC(), UpdatedAt: time.UnixMilli(row.UpdatedAt).UTC()}
	if row.ParentID != nil {
		menu.ParentID = *row.ParentID
	}
	if row.Route != nil {
		menu.Route = *row.Route
	}
	return menu
}
