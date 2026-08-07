package iam

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	iamdomain "github.com/chaos-plus/chaosplus/internal/modules/iam/domain"
)

// platformAuditTenant scopes platform administration events into their own
// audit chain. Platform operations sit above every tenant boundary, so they
// cannot borrow a real tenant id without corrupting that tenant's chain.
const platformAuditTenant = "_platform"

type platformGrantRow struct {
	bun.BaseModel  `bun:"table:iam_platform_grants"`
	PrincipalID    string `bun:"principal_id,pk"`
	PermissionCode string `bun:"permission_code,pk"`
	CreatedAt      int64
}

// PlatformPermissionCodes lists every declared platform-scope permission in
// stable order. A full administrator implicitly holds all of them.
func PlatformPermissionCodes(registry *authz.Registry) []string {
	codes := make([]string, 0, 8)
	if registry == nil {
		return codes
	}
	for _, action := range registry.All() {
		if action.Scope == "platform" {
			codes = append(codes, action.Code)
		}
	}
	sort.Strings(codes)
	return codes
}

func validatePlatformPrincipal(principalID string) (string, error) {
	principalID = strings.TrimSpace(principalID)
	if principalID == "" || len(principalID) > 64 {
		return "", fmt.Errorf("%w: invalid platform administrator principal", ErrInvalidArgument)
	}
	return principalID, nil
}

// ListPlatformAdministrators returns every principal holding platform
// authorization, whether full administrator or restricted grant holder.
func (r *Repository) ListPlatformAdministrators(ctx context.Context) ([]iamdomain.PlatformAdministrator, error) {
	adminRows := make([]platformAdministratorRow, 0)
	if err := r.executor.NewSelect().Model(&adminRows).Order("principal_id ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list platform administrators: %w", err)
	}
	grantRows := make([]platformGrantRow, 0)
	if err := r.executor.NewSelect().Model(&grantRows).Order("principal_id ASC", "permission_code ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list platform grants: %w", err)
	}
	byPrincipal := map[string]*iamdomain.PlatformAdministrator{}
	for _, row := range adminRows {
		byPrincipal[row.PrincipalID] = &iamdomain.PlatformAdministrator{
			PrincipalID: row.PrincipalID, FullAdministrator: true,
			Permissions: []string{}, CreatedAt: time.UnixMilli(row.CreatedAt).UTC(),
		}
	}
	for _, row := range grantRows {
		existing, ok := byPrincipal[row.PrincipalID]
		if !ok {
			existing = &iamdomain.PlatformAdministrator{
				PrincipalID: row.PrincipalID, Permissions: []string{}, CreatedAt: time.UnixMilli(row.CreatedAt).UTC(),
			}
			byPrincipal[row.PrincipalID] = existing
		}
		existing.Permissions = append(existing.Permissions, row.PermissionCode)
	}
	order := make([]string, 0, len(byPrincipal))
	for principalID := range byPrincipal {
		order = append(order, principalID)
	}
	sort.Strings(order)
	result := make([]iamdomain.PlatformAdministrator, 0, len(order))
	for _, principalID := range order {
		result = append(result, *byPrincipal[principalID])
	}
	return result, nil
}

// GetPlatformAdministrator returns one principal's platform authorization.
func (r *Repository) GetPlatformAdministrator(ctx context.Context, principalID string) (iamdomain.PlatformAdministrator, error) {
	principalID, err := validatePlatformPrincipal(principalID)
	if err != nil {
		return iamdomain.PlatformAdministrator{}, err
	}
	administrators, err := r.ListPlatformAdministrators(ctx)
	if err != nil {
		return iamdomain.PlatformAdministrator{}, err
	}
	for _, administrator := range administrators {
		if administrator.PrincipalID == principalID {
			return administrator, nil
		}
	}
	return iamdomain.PlatformAdministrator{}, ErrPlatformAdministratorNotFound
}

// SetPlatformAdministrator replaces one principal's platform authorization.
// A full administrator holds every platform permission; otherwise the supplied
// codes become the exact grant set.
func (r *Repository) SetPlatformAdministrator(ctx context.Context, principalID string, full bool, permissions []string) (iamdomain.PlatformAdministrator, error) {
	principalID, err := validatePlatformPrincipal(principalID)
	if err != nil {
		return iamdomain.PlatformAdministrator{}, err
	}
	now := r.now().UTC().UnixMilli()
	if _, err := r.executor.NewDelete().Model((*platformGrantRow)(nil)).Where("principal_id = ?", principalID).Exec(ctx); err != nil {
		return iamdomain.PlatformAdministrator{}, fmt.Errorf("clear platform grants: %w", err)
	}
	if full {
		row := platformAdministratorRow{PrincipalID: principalID, CreatedAt: now}
		if _, err := r.executor.NewInsert().Model(&row).Ignore().Exec(ctx); err != nil {
			return iamdomain.PlatformAdministrator{}, fmt.Errorf("grant platform administrator: %w", err)
		}
		return r.GetPlatformAdministrator(ctx, principalID)
	}
	if _, err := r.executor.NewDelete().Model((*platformAdministratorRow)(nil)).Where("principal_id = ?", principalID).Exec(ctx); err != nil {
		return iamdomain.PlatformAdministrator{}, fmt.Errorf("revoke platform administrator: %w", err)
	}
	rows := make([]platformGrantRow, 0, len(permissions))
	for _, code := range permissions {
		rows = append(rows, platformGrantRow{PrincipalID: principalID, PermissionCode: code, CreatedAt: now})
	}
	if len(rows) == 0 {
		return iamdomain.PlatformAdministrator{}, ErrPlatformAdministratorNotFound
	}
	if _, err := r.executor.NewInsert().Model(&rows).Exec(ctx); err != nil {
		return iamdomain.PlatformAdministrator{}, fmt.Errorf("insert platform grants: %w", err)
	}
	return r.GetPlatformAdministrator(ctx, principalID)
}

// DeletePlatformAdministrator removes every platform grant for one principal.
func (r *Repository) DeletePlatformAdministrator(ctx context.Context, principalID string) (bool, error) {
	principalID, err := validatePlatformPrincipal(principalID)
	if err != nil {
		return false, err
	}
	adminResult, err := r.executor.NewDelete().Model((*platformAdministratorRow)(nil)).Where("principal_id = ?", principalID).Exec(ctx)
	if err != nil {
		return false, fmt.Errorf("revoke platform administrator: %w", err)
	}
	grantResult, err := r.executor.NewDelete().Model((*platformGrantRow)(nil)).Where("principal_id = ?", principalID).Exec(ctx)
	if err != nil {
		return false, fmt.Errorf("revoke platform grants: %w", err)
	}
	administrators, _ := adminResult.RowsAffected()
	grants, _ := grantResult.RowsAffected()
	return administrators+grants > 0, nil
}

// CountFullPlatformAdministrators reports how many principals still hold
// unrestricted platform authorization.
func (r *Repository) CountFullPlatformAdministrators(ctx context.Context) (int, error) {
	count, err := r.executor.NewSelect().Model((*platformAdministratorRow)(nil)).Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("count platform administrators: %w", err)
	}
	return count, nil
}

// assertPlatformAdministratorRemains fails the surrounding transaction when a
// mutation would leave the platform with no unrestricted administrator.
func (r *Repository) assertPlatformAdministratorRemains(ctx context.Context) error {
	count, err := r.CountFullPlatformAdministrators(ctx)
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrLastPlatformAdministrator
	}
	return nil
}

// PlatformPermissionCatalog lists the declared platform-scope actions. The
// tenant catalog deliberately excludes them, so platform administration has
// no other way to discover valid codes.
func (s *Service) PlatformPermissionCatalog(context.Context) []authz.Action {
	actions := make([]authz.Action, 0, 8)
	for _, action := range s.registry.All() {
		if action.Scope == "platform" {
			actions = append(actions, action)
		}
	}
	sort.Slice(actions, func(i, j int) bool { return actions[i].Code < actions[j].Code })
	return actions
}

// ListPlatformAdministrators exposes the platform authorization roster.
func (s *Service) ListPlatformAdministrators(ctx context.Context) ([]iamdomain.PlatformAdministrator, error) {
	return s.repo.ListPlatformAdministrators(ctx)
}

// SetPlatformAdministrator grants or restricts one principal's platform
// authorization. It refuses unknown or tenant-scoped permission codes and
// preserves at least one full platform administrator.
func (s *Service) SetPlatformAdministrator(ctx context.Context, principalID string, full bool, permissions []string) (iamdomain.PlatformAdministrator, error) {
	principalID, err := validatePlatformPrincipal(principalID)
	if err != nil {
		return iamdomain.PlatformAdministrator{}, err
	}
	normalized, err := s.normalizePlatformPermissions(full, permissions)
	if err != nil {
		return iamdomain.PlatformAdministrator{}, err
	}
	record := newAuditRecord(ctx, platformAuditTenant, "platform_administrator_updated", "principal", principalID)
	record.PolicyChanged = true
	record.Detail["full_administrator"] = full
	record.Detail["permissions"] = normalized
	var administrator iamdomain.PlatformAdministrator
	err = s.writes.Run(ctx, record, func(repo *Repository) error {
		current, err := repo.GetPlatformAdministrator(ctx, principalID)
		if err != nil && !errors.Is(err, ErrPlatformAdministratorNotFound) {
			return err
		}
		demoted := err == nil && current.FullAdministrator && !full
		administrator, err = repo.SetPlatformAdministrator(ctx, principalID, full, normalized)
		if err != nil {
			return err
		}
		if demoted {
			return repo.assertPlatformAdministratorRemains(ctx)
		}
		return nil
	})
	return administrator, err
}

// DeletePlatformAdministrator revokes every platform grant for one principal
// while preserving a recoverable platform administration path.
func (s *Service) DeletePlatformAdministrator(ctx context.Context, principalID string) (bool, error) {
	principalID, err := validatePlatformPrincipal(principalID)
	if err != nil {
		return false, err
	}
	record := newAuditRecord(ctx, platformAuditTenant, "platform_administrator_revoked", "principal", principalID)
	record.PolicyChanged = true
	var changed bool
	err = s.writes.Run(ctx, record, func(repo *Repository) error {
		var err error
		changed, err = repo.DeletePlatformAdministrator(ctx, principalID)
		if err != nil {
			return err
		}
		record.Detail["changed"] = changed
		if !changed {
			return ErrPlatformAdministratorNotFound
		}
		return repo.assertPlatformAdministratorRemains(ctx)
	})
	return changed, err
}

func (s *Service) normalizePlatformPermissions(full bool, permissions []string) ([]string, error) {
	if full {
		if len(permissions) > 0 {
			return nil, fmt.Errorf("%w: a full platform administrator does not accept explicit permissions", ErrInvalidArgument)
		}
		return []string{}, nil
	}
	if len(permissions) == 0 {
		return nil, fmt.Errorf("%w: a restricted platform administrator requires at least one permission", ErrInvalidArgument)
	}
	seen := map[string]bool{}
	normalized := make([]string, 0, len(permissions))
	for _, code := range permissions {
		code = strings.TrimSpace(code)
		action, ok := s.registry.Find(code)
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrPermissionNotFound, code)
		}
		if action.Scope != "platform" {
			return nil, fmt.Errorf("%w: %s", ErrPlatformPermissionScope, code)
		}
		if !seen[code] {
			seen[code] = true
			normalized = append(normalized, code)
		}
	}
	sort.Strings(normalized)
	return normalized, nil
}
