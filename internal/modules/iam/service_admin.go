package iam

import (
	"context"
	"fmt"
	"net/mail"
	"sort"
	"strings"

	"github.com/chaos-plus/chaosplus/internal/core/extension/spicedbx"
	iamapi "github.com/chaos-plus/chaosplus/internal/modules/iam/api"
)

func (s *Service) PutTenantMember(ctx context.Context, tenantID, subject, displayName, email string, status MemberStatus) (TenantMember, error) {
	if err := validateTenant(tenantID); err != nil {
		return TenantMember{}, err
	}
	subject, displayName, email = strings.TrimSpace(subject), strings.TrimSpace(displayName), strings.TrimSpace(email)
	if subject == "" || len(subject) > 255 || displayName == "" || len(displayName) > 128 || len(email) > 320 || (status != MemberActive && status != MemberDisabled) {
		return TenantMember{}, fmt.Errorf("%w: invalid tenant member", ErrInvalidArgument)
	}
	if email != "" {
		if parsed, err := mail.ParseAddress(email); err != nil || parsed.Address != email {
			return TenantMember{}, fmt.Errorf("%w: invalid member email", ErrInvalidArgument)
		}
	}
	return s.repo.PutMember(ctx, TenantMember{TenantID: tenantID, Subject: subject, DisplayName: displayName, Email: email, Status: status})
}

func (s *Service) GetTenantMember(ctx context.Context, tenantID, subject string) (TenantMember, error) {
	if err := validateMemberRef(tenantID, subject); err != nil {
		return TenantMember{}, err
	}
	return s.repo.GetMember(ctx, tenantID, subject)
}

func (s *Service) ListTenantMembers(ctx context.Context, tenantID string, filter MemberFilter) ([]TenantMember, int64, error) {
	if err := validateTenant(tenantID); err != nil {
		return nil, 0, err
	}
	if filter.Offset < 0 || filter.Limit < 1 || filter.Limit > 200 || (filter.Status != "" && filter.Status != MemberActive && filter.Status != MemberDisabled) || len(filter.Search) > 128 {
		return nil, 0, fmt.Errorf("%w: invalid member filter", ErrInvalidArgument)
	}
	return s.repo.ListMembersPage(ctx, tenantID, filter)
}

func (s *Service) SetTenantMemberStatus(ctx context.Context, tenantID, subject string, status MemberStatus) (TenantMember, error) {
	if err := validateMemberRef(tenantID, subject); err != nil {
		return TenantMember{}, err
	}
	if status != MemberActive && status != MemberDisabled {
		return TenantMember{}, fmt.Errorf("%w: invalid member status", ErrInvalidArgument)
	}
	return s.repo.SetMemberStatus(ctx, tenantID, subject, status)
}

func (s *Service) ListTenantMemberRoles(ctx context.Context, tenantID, subject string) ([]string, error) {
	if err := validateMemberRef(tenantID, subject); err != nil {
		return nil, err
	}
	if _, err := s.repo.GetMember(ctx, tenantID, subject); err != nil {
		return nil, err
	}
	return s.repo.ListMemberRoleIDs(ctx, tenantID, subject)
}

func (s *Service) CreateMenu(ctx context.Context, menu Menu) (Menu, error) {
	menu.ID = ""
	if err := s.validateMenu(ctx, menu); err != nil {
		return Menu{}, err
	}
	return s.repo.CreateMenu(ctx, menu)
}

func (s *Service) ListMenus(ctx context.Context, tenantID string) ([]Menu, error) {
	if err := validateTenant(tenantID); err != nil {
		return nil, err
	}
	return s.repo.ListMenus(ctx, tenantID, false)
}

func (s *Service) GetMenu(ctx context.Context, tenantID, menuID string) (Menu, error) {
	if err := validateMenuRef(tenantID, menuID); err != nil {
		return Menu{}, err
	}
	return s.repo.GetMenu(ctx, tenantID, menuID)
}

func (s *Service) UpdateMenu(ctx context.Context, menu Menu) (Menu, error) {
	if err := validateMenuRef(menu.TenantID, menu.ID); err != nil {
		return Menu{}, err
	}
	if _, err := s.repo.GetMenu(ctx, menu.TenantID, menu.ID); err != nil {
		return Menu{}, err
	}
	if err := s.validateMenu(ctx, menu); err != nil {
		return Menu{}, err
	}
	if menu.ParentID != "" {
		seen := map[string]bool{menu.ID: true}
		parentID := menu.ParentID
		for depth := 0; parentID != "" && depth < 100; depth++ {
			if seen[parentID] {
				return Menu{}, fmt.Errorf("%w: menu hierarchy cycle", ErrInvalidArgument)
			}
			seen[parentID] = true
			parent, err := s.repo.GetMenu(ctx, menu.TenantID, parentID)
			if err != nil {
				return Menu{}, fmt.Errorf("%w: parent menu", ErrInvalidArgument)
			}
			parentID = parent.ParentID
		}
		if parentID != "" {
			return Menu{}, fmt.Errorf("%w: menu hierarchy too deep", ErrInvalidArgument)
		}
	}
	return s.repo.UpdateMenu(ctx, menu)
}

func (s *Service) DeleteMenu(ctx context.Context, tenantID, menuID string) error {
	if err := validateMenuRef(tenantID, menuID); err != nil {
		return err
	}
	return s.repo.DeleteMenu(ctx, tenantID, menuID)
}

func (s *Service) EffectiveMenus(ctx context.Context, tenantID, subject string) ([]iamapi.MenuItem, error) {
	if err := validateMemberRef(tenantID, subject); err != nil {
		return nil, err
	}
	menus, err := s.repo.ListMenus(ctx, tenantID, true)
	if err != nil {
		return nil, err
	}
	codeSet := map[string]struct{}{}
	for _, menu := range menus {
		if menu.PermissionCode == "" {
			continue
		}
		if _, ok := s.registry.Find(menu.PermissionCode); !ok {
			return nil, fmt.Errorf("unknown persisted menu permission %q", menu.PermissionCode)
		}
		codeSet[menu.PermissionCode] = struct{}{}
	}
	codes := make([]string, 0, len(codeSet))
	for code := range codeSet {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	allowed := map[string]bool{}
	for start := 0; start < len(codes); start += 100 {
		end := min(start+100, len(codes))
		result, err := s.checker.CheckBulk(ctx, spicedbx.ObjectRef{Type: "tenant", ID: tenantID}, codes[start:end], spicedbx.SubjectRef{Object: spicedbx.ObjectRef{Type: "user", ID: subject}}, "")
		if err != nil {
			return nil, fmt.Errorf("check effective menu permissions: %w", err)
		}
		for code, value := range result {
			allowed[code] = value
		}
	}
	children := map[string][]Menu{}
	for _, menu := range menus {
		children[menu.ParentID] = append(children[menu.ParentID], menu)
	}
	var build func(string, map[string]bool) []iamapi.MenuItem
	build = func(parent string, visiting map[string]bool) []iamapi.MenuItem {
		var result []iamapi.MenuItem
		for _, menu := range children[parent] {
			if visiting[menu.ID] {
				continue
			}
			next := make(map[string]bool, len(visiting)+1)
			for id, value := range visiting {
				next[id] = value
			}
			next[menu.ID] = true
			nodes := build(menu.ID, next)
			if menu.PermissionCode != "" && !allowed[menu.PermissionCode] && len(nodes) == 0 {
				continue
			}
			result = append(result, iamapi.MenuItem{ID: menu.ID, Label: menu.Label, Path: menu.Route, Icon: menu.Icon, SortOrder: menu.SortOrder, PermissionCode: menu.PermissionCode, Children: nodes})
		}
		return result
	}
	return build("", map[string]bool{}), nil
}

func (s *Service) validateMenu(ctx context.Context, menu Menu) error {
	if err := validateTenant(menu.TenantID); err != nil {
		return err
	}
	menu.Label, menu.Route, menu.Icon, menu.PermissionCode = strings.TrimSpace(menu.Label), strings.TrimSpace(menu.Route), strings.TrimSpace(menu.Icon), strings.TrimSpace(menu.PermissionCode)
	if menu.Label == "" || len(menu.Label) > 128 || len(menu.Route) > 512 || len(menu.Icon) > 64 || menu.SortOrder < -100000 || menu.SortOrder > 100000 || (menu.Status != MenuActive && menu.Status != MenuDisabled) {
		return fmt.Errorf("%w: invalid menu", ErrInvalidArgument)
	}
	if menu.Route != "" && (!strings.HasPrefix(menu.Route, "/") || strings.Contains(menu.Route, "//")) {
		return fmt.Errorf("%w: invalid menu route", ErrInvalidArgument)
	}
	if menu.PermissionCode != "" {
		if _, ok := s.registry.Find(menu.PermissionCode); !ok {
			return fmt.Errorf("%w: %s", ErrPermissionNotFound, menu.PermissionCode)
		}
	}
	if menu.ParentID != "" {
		if _, err := s.repo.GetMenu(ctx, menu.TenantID, menu.ParentID); err != nil {
			return fmt.Errorf("%w: parent menu", ErrInvalidArgument)
		}
	}
	return nil
}

func validateMemberRef(tenantID, subject string) error {
	if err := validateTenant(tenantID); err != nil {
		return err
	}
	if strings.TrimSpace(subject) == "" || len(subject) > 255 {
		return fmt.Errorf("%w: invalid member subject", ErrInvalidArgument)
	}
	return nil
}

func validateMenuRef(tenantID, menuID string) error {
	if err := validateTenant(tenantID); err != nil {
		return err
	}
	if strings.TrimSpace(menuID) == "" || len(menuID) > 32 {
		return fmt.Errorf("%w: invalid menu id", ErrInvalidArgument)
	}
	return nil
}
