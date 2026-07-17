package iam

import (
	"context"
	"fmt"
	"strings"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/spicedbx"
	iamapi "github.com/chaos-plus/chaosplus/internal/modules/iam/api"
	iamdomain "github.com/chaos-plus/chaosplus/internal/modules/iam/domain"
)

type outboxNotifier interface{ Wake() }
type BulkPermissionChecker interface {
	CheckBulk(context.Context, spicedbx.ObjectRef, []string, spicedbx.SubjectRef, spicedbx.ZedToken) (map[string]bool, error)
}

type Service struct {
	registry *authz.Registry
	repo     *Repository
	notifier outboxNotifier
	checker  BulkPermissionChecker
}

func NewService(registry *authz.Registry, repo *Repository, notifier outboxNotifier, checker BulkPermissionChecker) *Service {
	if registry == nil || repo == nil || notifier == nil || checker == nil {
		panic("iam service requires registry, repository, outbox notifier, and permission checker")
	}
	return &Service{registry: registry, repo: repo, notifier: notifier, checker: checker}
}

func newDeclarationService(registry *authz.Registry) *Service {
	return &Service{registry: registry}
}

func (s *Service) PermissionCatalog(context.Context) []authz.Action {
	return s.registry.All()
}

func (s *Service) SpiceDBSchema(context.Context) string {
	return authz.GenerateSchema(s.registry.All())
}

func (s *Service) ScopeModel(context.Context) []iamapi.ScopeNode {
	return []iamapi.ScopeNode{
		{Type: "platform", ParentType: "", Relation: "administer", Label: "Platform"},
		{Type: "tenant", ParentType: "platform", Relation: "platform", Label: "Brand tenant"},
		{Type: "merchant", ParentType: "tenant", Relation: "tenant", Label: "Merchant"},
		{Type: "store", ParentType: "merchant", Relation: "merchant", Label: "Store"},
		{Type: "dept", ParentType: "tenant", Relation: "parent", Label: "Department tree"},
	}
}

func (s *Service) MenuCatalog(context.Context) []iamapi.MenuItem {
	return []iamapi.MenuItem{
		{ID: "iam", Label: "IAM", PermissionCode: "menu_view", Children: []iamapi.MenuItem{
			{ID: "iam-users", Label: "Users", Path: "/iam/users", PermissionCode: "user_view"},
			{ID: "iam-roles", Label: "Roles", Path: "/iam/roles", PermissionCode: "role_view"},
			{ID: "iam-depts", Label: "Departments", Path: "/iam/depts", PermissionCode: "dept_view"},
			{ID: "iam-menus", Label: "Menus", Path: "/iam/menus", PermissionCode: "menu_view"},
		}},
		{ID: "org", Label: "Organization", PermissionCode: "tenant_view", Children: []iamapi.MenuItem{
			{ID: "org-tenants", Label: "Tenants", Path: "/tenants", PermissionCode: "tenant_view"},
			{ID: "org-merchants", Label: "Merchants", Path: "/merchants", PermissionCode: "merchant_view"},
			{ID: "org-stores", Label: "Stores", Path: "/stores", PermissionCode: "store_view"},
		}},
	}
}

func (s *Service) CreateRole(ctx context.Context, tenantID, name, description string) (iamdomain.Role, error) {
	if err := validateTenant(tenantID); err != nil {
		return iamdomain.Role{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 128 || len(description) > 4096 {
		return iamdomain.Role{}, fmt.Errorf("%w: invalid role name or description", ErrInvalidArgument)
	}
	return s.repo.CreateRole(ctx, tenantID, name, description)
}

func (s *Service) ListRoles(ctx context.Context, tenantID string) ([]iamdomain.Role, error) {
	if err := validateTenant(tenantID); err != nil {
		return nil, err
	}
	return s.repo.ListRoles(ctx, tenantID)
}

func (s *Service) GetRole(ctx context.Context, tenantID, roleID string) (iamdomain.Role, error) {
	if err := validateRoleRef(tenantID, roleID); err != nil {
		return iamdomain.Role{}, err
	}
	return s.repo.GetRole(ctx, tenantID, roleID)
}

func (s *Service) UpdateRole(ctx context.Context, tenantID, roleID string, name, description *string) (iamdomain.Role, error) {
	if err := validateRoleRef(tenantID, roleID); err != nil {
		return iamdomain.Role{}, err
	}
	if name == nil && description == nil {
		return iamdomain.Role{}, fmt.Errorf("%w: no role fields supplied", ErrInvalidArgument)
	}
	current, err := s.repo.GetRole(ctx, tenantID, roleID)
	if err != nil {
		return iamdomain.Role{}, err
	}
	if name != nil {
		current.Name = strings.TrimSpace(*name)
	}
	if description != nil {
		current.Description = *description
	}
	if current.Name == "" || len(current.Name) > 128 || len(current.Description) > 4096 {
		return iamdomain.Role{}, fmt.Errorf("%w: invalid role name or description", ErrInvalidArgument)
	}
	return s.repo.UpdateRole(ctx, tenantID, roleID, current.Name, current.Description)
}

func (s *Service) DeleteRole(ctx context.Context, tenantID, roleID string) error {
	if err := validateRoleRef(tenantID, roleID); err != nil {
		return err
	}
	if err := s.repo.DeleteRole(ctx, tenantID, roleID); err != nil {
		return err
	}
	s.notifier.Wake()
	return nil
}

func (s *Service) ListPermissions(ctx context.Context, tenantID, roleID string) ([]string, error) {
	if err := validateRoleRef(tenantID, roleID); err != nil {
		return nil, err
	}
	return s.repo.ListPermissions(ctx, tenantID, roleID)
}

func (s *Service) GrantPermission(ctx context.Context, tenantID, roleID, code string) (bool, error) {
	return s.changePermission(ctx, tenantID, roleID, code, true)
}

func (s *Service) RevokePermission(ctx context.Context, tenantID, roleID, code string) (bool, error) {
	return s.changePermission(ctx, tenantID, roleID, code, false)
}

func (s *Service) changePermission(ctx context.Context, tenantID, roleID, code string, grant bool) (bool, error) {
	if err := validateRoleRef(tenantID, roleID); err != nil {
		return false, err
	}
	if _, ok := s.registry.Find(code); !ok {
		return false, fmt.Errorf("%w: %s", ErrPermissionNotFound, code)
	}
	var changed bool
	var err error
	if grant {
		changed, err = s.repo.GrantPermission(ctx, tenantID, roleID, code)
	} else {
		changed, err = s.repo.RevokePermission(ctx, tenantID, roleID, code)
	}
	if err == nil {
		s.notifier.Wake()
	}
	return changed, err
}

func (s *Service) ListMembers(ctx context.Context, tenantID, roleID string) ([]string, error) {
	if err := validateRoleRef(tenantID, roleID); err != nil {
		return nil, err
	}
	return s.repo.ListMembers(ctx, tenantID, roleID)
}

func (s *Service) AddMember(ctx context.Context, tenantID, roleID, subject string) (bool, error) {
	return s.changeMember(ctx, tenantID, roleID, subject, true)
}

func (s *Service) RemoveMember(ctx context.Context, tenantID, roleID, subject string) (bool, error) {
	return s.changeMember(ctx, tenantID, roleID, subject, false)
}

func (s *Service) changeMember(ctx context.Context, tenantID, roleID, subject string, add bool) (bool, error) {
	if err := validateRoleRef(tenantID, roleID); err != nil {
		return false, err
	}
	subject = strings.TrimSpace(subject)
	if subject == "" || len(subject) > 255 {
		return false, fmt.Errorf("%w: invalid Zitadel subject", ErrInvalidArgument)
	}
	var changed bool
	var err error
	if add {
		active, activeErr := s.repo.IsMemberActive(ctx, tenantID, subject)
		if activeErr != nil {
			return false, activeErr
		}
		if !active {
			return false, ErrMemberInactive
		}
		changed, err = s.repo.AddMember(ctx, tenantID, roleID, subject)
	} else {
		changed, err = s.repo.RemoveMember(ctx, tenantID, roleID, subject)
	}
	if err == nil {
		s.notifier.Wake()
	}
	return changed, err
}

func validateTenant(tenantID string) error {
	if strings.TrimSpace(tenantID) == "" || len(tenantID) > 128 {
		return fmt.Errorf("%w: invalid tenant id", ErrInvalidArgument)
	}
	return nil
}

func validateRoleRef(tenantID, roleID string) error {
	if err := validateTenant(tenantID); err != nil {
		return err
	}
	if strings.TrimSpace(roleID) == "" || len(roleID) > 32 {
		return fmt.Errorf("%w: invalid role id", ErrInvalidArgument)
	}
	return nil
}
