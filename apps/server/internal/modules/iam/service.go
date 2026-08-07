package iam

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/policyx"
	iamdomain "github.com/chaos-plus/chaosplus/internal/modules/iam/domain"
)

type AuthorizationEvaluator interface {
	CheckBulk(ctx context.Context, tenantID string, permissions []string, subject string) (map[string]bool, error)
	Constraint(ctx context.Context, tenantID, permission, subject string) (authz.DataConstraint, error)
	ExplainEntity(ctx context.Context, tenantID, entityID, permission, subject string) (authz.Explanation, error)
	ExplainResource(ctx context.Context, tenantID, entityID, resourceType, resourceID, permission, subject string) (authz.Explanation, error)
}

type Service struct {
	registry       *authz.Registry
	repo           *Repository
	checker        AuthorizationEvaluator
	writes         *transactionCoordinator
	administrators *AdministratorGuard
}

func NewService(registry *authz.Registry, repo *Repository, checker AuthorizationEvaluator, audit auditx.Appender) *Service {
	if registry == nil || repo == nil || checker == nil || audit == nil {
		panic("iam service requires registry, repository, permission checker, and audit appender")
	}
	return &Service{registry: registry, repo: repo, checker: checker, writes: newTransactionCoordinator(repo, audit), administrators: NewAdministratorGuard()}
}

func newDeclarationService(registry *authz.Registry) *Service {
	return &Service{registry: registry}
}

func (s *Service) PermissionCatalog(context.Context) []authz.Action {
	actions := s.registry.All()
	tenantActions := make([]authz.Action, 0, len(actions))
	for _, action := range actions {
		if action.Scope != "platform" {
			tenantActions = append(tenantActions, action)
		}
	}
	return tenantActions
}

func (s *Service) ScopeModel(context.Context) []ScopeNode {
	return []ScopeNode{
		{Type: "platform", ParentType: "", Relation: "owns", Label: "Platform"},
		{Type: "tenant", ParentType: "platform", Relation: "contains", Label: "Tenant"},
		{Type: "entity", ParentType: "tenant", Relation: "parent", Label: "Company / enterprise / merchant / store"},
		{Type: "business_resource", ParentType: "entity", Relation: "owner", Label: "Business resource"},
	}
}

func (s *Service) MenuCatalog(context.Context) []MenuItem {
	items := make([]MenuItem, 0, len(DefaultMenus()))
	for _, menu := range DefaultMenus() {
		items = append(items, MenuItem{
			ID:             menu.ID,
			Label:          menu.Label,
			Path:           menu.Route,
			PermissionCode: menu.PermissionCode,
			Icon:           menu.Icon,
			SortOrder:      menu.SortOrder,
		})
	}
	return []MenuItem{{ID: "iam", Label: "IAM Administration", Children: items}}
}

// DefaultMenus is the built-in administration navigation seeded for a new
// tenant. Only routes implemented by the current administration application
// belong here; roadmap pages must not appear before their APIs and workflows.
func DefaultMenus() []Menu {
	return []Menu{
		{ID: "iam-users", Label: "Principals and members", Route: "/iam/users", Icon: "users", SortOrder: 10, PermissionCode: "user_view", Status: MenuActive},
		{ID: "iam-invitations", Label: "Invitations", Route: "/iam/invitations", Icon: "mail-plus", SortOrder: 11, PermissionCode: "invitation_view", Status: MenuActive},
		{ID: "iam-service-accounts", Label: "Service accounts", Route: "/iam/service-accounts", Icon: "bot", SortOrder: 12, PermissionCode: "service_account_view", Status: MenuActive},
		{ID: "iam-entities", Label: "Entities", Route: "/iam/entities", Icon: "network", SortOrder: 13, PermissionCode: "entity_view", Status: MenuActive},
		{ID: "iam-departments", Label: "Departments", Route: "/iam/departments", Icon: "building-2", SortOrder: 15, PermissionCode: "dept_view", Status: MenuActive},
		{ID: "iam-positions", Label: "Positions", Route: "/iam/positions", Icon: "briefcase-business", SortOrder: 18, PermissionCode: "position_view", Status: MenuActive},
		{ID: "iam-groups", Label: "Groups", Route: "/iam/groups", Icon: "users-round", SortOrder: 19, PermissionCode: "group_view", Status: MenuActive},
		{ID: "iam-roles", Label: "Roles and permissions", Route: "/iam/roles", Icon: "key-round", SortOrder: 20, PermissionCode: "role_view", Status: MenuActive},
		{ID: "iam-access-requests", Label: "Access requests", Route: "/iam/access-requests", Icon: "clipboard-check", SortOrder: 21, PermissionCode: "access_request_view", Status: MenuActive},
		{ID: "iam-access-reviews", Label: "Access reviews", Route: "/iam/access-reviews", Icon: "list-checks", SortOrder: 22, PermissionCode: "access_review_view", Status: MenuActive},
		{ID: "iam-menus", Label: "Menu metadata", Route: "/iam/menus", Icon: "boxes", SortOrder: 30, PermissionCode: "menu_view", Status: MenuActive},
		{ID: "iam-oauth-clients", Label: "OAuth clients", Route: "/iam/oauth-clients", Icon: "app-window", SortOrder: 40, PermissionCode: "oauth_client_view", Status: MenuActive},
		{ID: "iam-scim-directories", Label: "SCIM directories", Route: "/iam/scim-directories", Icon: "folder-sync", SortOrder: 45, PermissionCode: "tenant_administer", Status: MenuActive},
		{ID: "iam-audit-events", Label: "Audit events", Route: "/iam/audit-events", Icon: "file-clock", SortOrder: 50, PermissionCode: "audit_event_view", Status: MenuActive},
		{ID: "iam-audit-governance", Label: "Audit governance", Route: "/iam/audit-governance", Icon: "shield-check", SortOrder: 51, PermissionCode: "audit_event_view", Status: MenuActive},
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
	var role iamdomain.Role
	record := newAuditRecord(ctx, tenantID, "role_created", "role", "")
	record.PolicyChanged = true
	err := s.writes.Run(ctx, record, func(repo *Repository) error {
		var err error
		role, err = repo.CreateRole(ctx, tenantID, name, description)
		record.TargetID = role.ID
		return err
	})
	return role, err
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
	var updated iamdomain.Role
	record := newAuditRecord(ctx, tenantID, "role_updated", "role", roleID)
	record.PolicyChanged = true
	err := s.writes.Run(ctx, record, func(repo *Repository) error {
		current, err := repo.GetRole(ctx, tenantID, roleID)
		if err != nil {
			return err
		}
		if name != nil {
			current.Name = strings.TrimSpace(*name)
		}
		if description != nil {
			current.Description = *description
		}
		if current.Name == "" || len(current.Name) > 128 || len(current.Description) > 4096 {
			return fmt.Errorf("%w: invalid role name or description", ErrInvalidArgument)
		}
		updated, err = repo.UpdateRole(ctx, tenantID, roleID, current.Name, current.Description)
		return err
	})
	return updated, err
}

func (s *Service) DeleteRole(ctx context.Context, tenantID, roleID string) error {
	if err := validateRoleRef(tenantID, roleID); err != nil {
		return err
	}
	record := newAuditRecord(ctx, tenantID, "role_deleted", "role", roleID)
	record.PolicyChanged = true
	return s.writes.Run(ctx, record, func(repo *Repository) error {
		verify, err := s.administrators.Protect(ctx, repo.executor, repo.dialect, tenantID)
		if err != nil {
			return err
		}
		if err := repo.DeleteRole(ctx, tenantID, roleID); err != nil {
			return err
		}
		return verify()
	})
}

func (s *Service) ListPermissions(ctx context.Context, tenantID, roleID string) ([]string, error) {
	if err := validateRoleRef(tenantID, roleID); err != nil {
		return nil, err
	}
	return s.repo.ListPermissions(ctx, tenantID, roleID)
}

func (s *Service) ListPermissionGrants(ctx context.Context, tenantID, roleID string) ([]RolePermissionGrant, error) {
	if err := validateRoleRef(tenantID, roleID); err != nil {
		return nil, err
	}
	return s.repo.ListPermissionGrants(ctx, tenantID, roleID)
}

func (s *Service) SetPermissionCondition(ctx context.Context, tenantID, roleID, code string, condition json.RawMessage) (RolePermissionGrant, bool, error) {
	if err := validateRoleRef(tenantID, roleID); err != nil {
		return RolePermissionGrant{}, false, err
	}
	action, ok := s.registry.Find(code)
	if !ok {
		return RolePermissionGrant{}, false, fmt.Errorf("%w: %s", ErrPermissionNotFound, code)
	}
	if action.Scope == "platform" {
		return RolePermissionGrant{}, false, fmt.Errorf("%w: platform permissions cannot be granted to tenant roles", ErrInvalidArgument)
	}
	// Relaxing a condition broadens an existing grant, so it needs the same
	// escalation guard as granting the permission outright.
	if err := s.requireGrantablePermissions(ctx, tenantID, code); err != nil {
		return RolePermissionGrant{}, false, err
	}
	canonical, err := policyx.CanonicalCondition(condition)
	if err != nil {
		return RolePermissionGrant{}, false, fmt.Errorf("%w: %v", ErrInvalidRolePermissionCondition, err)
	}
	record := newAuditRecord(ctx, tenantID, "role_permission_condition_updated", "role", roleID)
	record.Detail["permission_code"] = code
	if len(canonical) > 0 {
		record.Detail["condition"] = json.RawMessage(canonical)
	} else {
		record.Detail["condition_cleared"] = true
	}
	var grant RolePermissionGrant
	var changed bool
	err = s.writes.Run(ctx, record, func(repo *Repository) error {
		verify := func() error { return nil }
		if isAdministratorPermission(code) {
			var err error
			verify, err = s.administrators.Protect(ctx, repo.executor, repo.dialect, tenantID)
			if err != nil {
				return err
			}
		}
		var err error
		grant, changed, err = repo.SetPermissionCondition(ctx, tenantID, roleID, code, canonical)
		if err == nil && changed {
			err = verify()
		}
		record.PolicyChanged, record.SkipAudit = changed, !changed
		record.Detail["changed"] = changed
		return err
	})
	return grant, changed, err
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
	action, ok := s.registry.Find(code)
	if !ok {
		return false, fmt.Errorf("%w: %s", ErrPermissionNotFound, code)
	}
	if action.Scope == "platform" {
		return false, fmt.Errorf("%w: platform permissions cannot be granted to tenant roles", ErrInvalidArgument)
	}
	if grant {
		if err := s.requireGrantablePermissions(ctx, tenantID, code); err != nil {
			return false, err
		}
	}
	eventType := "role_permission_revoked"
	if grant {
		eventType = "role_permission_granted"
	}
	record := newAuditRecord(ctx, tenantID, eventType, "role", roleID)
	record.Detail["permission_code"] = code
	var changed bool
	err := s.writes.Run(ctx, record, func(repo *Repository) error {
		var err error
		if grant {
			changed, err = repo.GrantPermission(ctx, tenantID, roleID, code)
		} else {
			var verify func() error
			verify, err = s.administrators.Protect(ctx, repo.executor, repo.dialect, tenantID)
			if err != nil {
				return err
			}
			changed, err = repo.RevokePermission(ctx, tenantID, roleID, code)
			if err == nil {
				err = verify()
			}
		}
		record.PolicyChanged = changed
		record.Detail["changed"] = changed
		return err
	})
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
		return false, fmt.Errorf("%w: invalid principal subject", ErrInvalidArgument)
	}
	if add {
		if err := s.requireGrantableRole(ctx, tenantID, roleID); err != nil {
			return false, err
		}
	}
	eventType := "role_member_removed"
	if add {
		eventType = "role_member_added"
	}
	record := newAuditRecord(ctx, tenantID, eventType, "role", roleID)
	record.Detail["subject"] = subject
	var changed bool
	err := s.writes.Run(ctx, record, func(repo *Repository) error {
		var err error
		if add {
			active, activeErr := repo.IsMemberActive(ctx, tenantID, subject)
			if activeErr != nil {
				return activeErr
			}
			if !active {
				return ErrMemberInactive
			}
			changed, err = repo.AddMember(ctx, tenantID, roleID, subject)
		} else {
			var verify func() error
			verify, err = s.administrators.Protect(ctx, repo.executor, repo.dialect, tenantID)
			if err != nil {
				return err
			}
			changed, err = repo.RemoveMember(ctx, tenantID, roleID, subject)
			if err == nil {
				err = verify()
			}
		}
		record.PolicyChanged = changed
		record.Detail["changed"] = changed
		return err
	})
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
