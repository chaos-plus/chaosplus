package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
	iamdomain "github.com/chaos-plus/chaosplus/internal/modules/iam/domain"
)

type Service interface {
	PermissionCatalog(context.Context) []authz.Action
	SpiceDBSchema(context.Context) string
	ScopeModel(context.Context) []ScopeNode
	MenuCatalog(context.Context) []MenuItem
	CreateRole(context.Context, string, string, string) (iamdomain.Role, error)
	ListRoles(context.Context, string) ([]iamdomain.Role, error)
	GetRole(context.Context, string, string) (iamdomain.Role, error)
	UpdateRole(context.Context, string, string, *string, *string) (iamdomain.Role, error)
	DeleteRole(context.Context, string, string) error
	ListPermissions(context.Context, string, string) ([]string, error)
	GrantPermission(context.Context, string, string, string) (bool, error)
	RevokePermission(context.Context, string, string, string) (bool, error)
	ListMembers(context.Context, string, string) ([]string, error)
	AddMember(context.Context, string, string, string) (bool, error)
	RemoveMember(context.Context, string, string, string) (bool, error)
	PutTenantMember(context.Context, string, string, string, string, iamdomain.MemberStatus) (iamdomain.TenantMember, error)
	GetTenantMember(context.Context, string, string) (iamdomain.TenantMember, error)
	ListTenantMembers(context.Context, string, iamdomain.MemberFilter) ([]iamdomain.TenantMember, int64, error)
	SetTenantMemberStatus(context.Context, string, string, iamdomain.MemberStatus) (iamdomain.TenantMember, error)
	ListTenantMemberRoles(context.Context, string, string) ([]string, error)
	CreateMenu(context.Context, iamdomain.Menu) (iamdomain.Menu, error)
	ListMenus(context.Context, string) ([]iamdomain.Menu, error)
	GetMenu(context.Context, string, string) (iamdomain.Menu, error)
	UpdateMenu(context.Context, iamdomain.Menu) (iamdomain.Menu, error)
	DeleteMenu(context.Context, string, string) error
	EffectiveMenus(context.Context, string, string) ([]MenuItem, error)
}

type ScopeNode struct {
	Type       string `json:"type" doc:"SpiceDB object type"`
	ParentType string `json:"parent_type,omitempty" doc:"parent object type"`
	Relation   string `json:"relation" doc:"relation used to connect to parent or administer"`
	Label      string `json:"label" doc:"display label"`
}

type MenuItem struct {
	ID             string     `json:"id"`
	Label          string     `json:"label"`
	Path           string     `json:"path,omitempty"`
	PermissionCode string     `json:"permission_code"`
	Icon           string     `json:"icon,omitempty"`
	SortOrder      int        `json:"sort_order"`
	Children       []MenuItem `json:"children,omitempty"`
}

type TenantMember struct {
	TenantID    string                 `json:"tenant_id"`
	Subject     string                 `json:"subject"`
	DisplayName string                 `json:"display_name"`
	Email       string                 `json:"email,omitempty"`
	Status      iamdomain.MemberStatus `json:"status"`
	RoleIDs     []string               `json:"role_ids,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
	DisabledAt  *time.Time             `json:"disabled_at,omitempty"`
}

type Menu struct {
	ID             string               `json:"id"`
	TenantID       string               `json:"tenant_id"`
	ParentID       string               `json:"parent_id,omitempty"`
	Label          string               `json:"label"`
	Route          string               `json:"route,omitempty"`
	Icon           string               `json:"icon,omitempty"`
	SortOrder      int                  `json:"sort_order"`
	PermissionCode string               `json:"permission_code,omitempty"`
	Status         iamdomain.MenuStatus `json:"status"`
	CreatedAt      time.Time            `json:"created_at"`
	UpdatedAt      time.Time            `json:"updated_at"`
}

type schemaOutput struct {
	Schema string `json:"schema"`
}

type Role struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type MutationResult struct {
	Changed    bool   `json:"changed" doc:"true when the local desired binding changed"`
	SyncStatus string `json:"sync_status" doc:"SpiceDB delivery state; pending means queued in the transactional outbox"`
}

type tenantInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128" doc:"tenant authorization boundary"`
}

type roleInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	RoleID   string `path:"role_id" maxLength:"32"`
}

type createRoleInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	Body     struct {
		Name        string `json:"name" minLength:"1" maxLength:"128"`
		Description string `json:"description,omitempty" maxLength:"4096"`
	}
}

type updateRoleInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	RoleID   string `path:"role_id" maxLength:"32"`
	Body     struct {
		Name        *string `json:"name,omitempty" minLength:"1" maxLength:"128"`
		Description *string `json:"description,omitempty" maxLength:"4096"`
	}
}

type permissionInput struct {
	TenantID       string `header:"X-Tenant-Id" maxLength:"128"`
	RoleID         string `path:"role_id" maxLength:"32"`
	PermissionCode string `path:"permission_code" maxLength:"128"`
}

type memberInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	RoleID   string `path:"role_id" maxLength:"32"`
	Subject  string `path:"subject" maxLength:"255" doc:"immutable Zitadel user subject"`
}

type listTenantMembersInput struct {
	TenantID string                 `header:"X-Tenant-Id" maxLength:"128"`
	Search   string                 `query:"search" maxLength:"128"`
	Status   iamdomain.MemberStatus `query:"status" enum:"active,disabled"`
	Offset   int                    `query:"offset" minimum:"0" default:"0"`
	Limit    int                    `query:"limit" minimum:"1" maximum:"200" default:"50"`
}

type tenantMemberInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	Subject  string `path:"subject" maxLength:"255"`
}

type createTenantMemberInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	Body     struct {
		Subject     string                 `json:"subject" minLength:"1" maxLength:"255"`
		DisplayName string                 `json:"display_name" minLength:"1" maxLength:"128"`
		Email       string                 `json:"email,omitempty" maxLength:"320"`
		Status      iamdomain.MemberStatus `json:"status" enum:"active,disabled" default:"active"`
	}
}

type updateTenantMemberInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	Subject  string `path:"subject" maxLength:"255"`
	Body     struct {
		DisplayName *string                 `json:"display_name,omitempty" minLength:"1" maxLength:"128"`
		Email       *string                 `json:"email,omitempty" maxLength:"320"`
		Status      *iamdomain.MemberStatus `json:"status,omitempty" enum:"active,disabled"`
	}
}

type menuInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	MenuID   string `path:"menu_id" maxLength:"32"`
}

type createMenuInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	Body     struct {
		ParentID       string               `json:"parent_id,omitempty" maxLength:"32"`
		Label          string               `json:"label" minLength:"1" maxLength:"128"`
		Route          string               `json:"route,omitempty" maxLength:"512"`
		Icon           string               `json:"icon,omitempty" maxLength:"64"`
		SortOrder      int                  `json:"sort_order" minimum:"-100000" maximum:"100000" default:"0"`
		PermissionCode string               `json:"permission_code,omitempty" maxLength:"128"`
		Status         iamdomain.MenuStatus `json:"status" enum:"active,disabled" default:"active"`
	}
}

type updateMenuInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	MenuID   string `path:"menu_id" maxLength:"32"`
	Body     struct {
		ParentID       *string               `json:"parent_id,omitempty" maxLength:"32"`
		Label          *string               `json:"label,omitempty" minLength:"1" maxLength:"128"`
		Route          *string               `json:"route,omitempty" maxLength:"512"`
		Icon           *string               `json:"icon,omitempty" maxLength:"64"`
		SortOrder      *int                  `json:"sort_order,omitempty" minimum:"-100000" maximum:"100000"`
		PermissionCode *string               `json:"permission_code,omitempty" maxLength:"128"`
		Status         *iamdomain.MenuStatus `json:"status,omitempty" enum:"active,disabled"`
	}
}

// RegisterREST mounts IAM discovery endpoints for the management UI.
func RegisterREST(a huma.API, svc Service, registrar *authz.Registrar) {
	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-permission-catalog",
		Method:      http.MethodGet,
		Path:        "/iam/permission-catalog",
		Summary:     "List declared permissions",
		Tags:        []string{"iam"},
	}, authz.Guard{Resource: "role", Verb: "view"}, func(ctx context.Context, _ *struct{}) (*respx.Body[[]authz.Action], error) {
		return respx.OK(ctx, svc.PermissionCatalog(ctx)), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-spicedb-schema",
		Method:      http.MethodGet,
		Path:        "/iam/spicedb/schema",
		Summary:     "Return the generated SpiceDB schema",
		Tags:        []string{"iam"},
	}, authz.Guard{Resource: "tenant", Verb: "administer"}, func(ctx context.Context, _ *struct{}) (*respx.Body[schemaOutput], error) {
		return respx.OK(ctx, schemaOutput{Schema: svc.SpiceDBSchema(ctx)}), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-scope-model",
		Method:      http.MethodGet,
		Path:        "/iam/scope-model",
		Summary:     "List platform tenant merchant store scope model",
		Tags:        []string{"iam"},
	}, authz.Guard{Resource: "tenant", Verb: "view"}, func(ctx context.Context, _ *struct{}) (*respx.Body[[]ScopeNode], error) {
		return respx.OK(ctx, svc.ScopeModel(ctx)), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-menu-catalog",
		Method:      http.MethodGet,
		Path:        "/iam/menu-catalog",
		Summary:     "List menu metadata bound to permission codes",
		Tags:        []string{"iam"},
	}, authz.Guard{Resource: "menu", Verb: "view"}, func(ctx context.Context, _ *struct{}) (*respx.Body[[]MenuItem], error) {
		return respx.OK(ctx, svc.MenuCatalog(ctx)), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-list-roles", Method: http.MethodGet, Path: "/iam/roles", Summary: "List tenant roles", Tags: []string{"iam"},
	}, authz.Guard{Resource: "role", Verb: "view"}, func(ctx context.Context, in *tenantInput) (*respx.Body[[]Role], error) {
		roles, err := svc.ListRoles(ctx, in.TenantID)
		if err != nil {
			return nil, apiError("list roles", err)
		}
		return respx.OK(ctx, rolesFromDomain(roles)), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-create-role", Method: http.MethodPost, Path: "/iam/roles", Summary: "Create a tenant role", Tags: []string{"iam"}, Errors: []int{http.StatusConflict},
	}, authz.Guard{Resource: "role", Verb: "create"}, func(ctx context.Context, in *createRoleInput) (*respx.Body[Role], error) {
		role, err := svc.CreateRole(ctx, in.TenantID, in.Body.Name, in.Body.Description)
		if err != nil {
			return nil, apiError("create role", err)
		}
		return respx.OK(ctx, roleFromDomain(role)), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-get-role", Method: http.MethodGet, Path: "/iam/roles/{role_id}", Summary: "Get a tenant role", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound},
	}, authz.Guard{Resource: "role", Verb: "view"}, func(ctx context.Context, in *roleInput) (*respx.Body[Role], error) {
		role, err := svc.GetRole(ctx, in.TenantID, in.RoleID)
		if err != nil {
			return nil, apiError("get role", err)
		}
		return respx.OK(ctx, roleFromDomain(role)), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-update-role", Method: http.MethodPatch, Path: "/iam/roles/{role_id}", Summary: "Update a tenant role", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound, http.StatusConflict},
	}, authz.Guard{Resource: "role", Verb: "update"}, func(ctx context.Context, in *updateRoleInput) (*respx.Body[Role], error) {
		role, err := svc.UpdateRole(ctx, in.TenantID, in.RoleID, in.Body.Name, in.Body.Description)
		if err != nil {
			return nil, apiError("update role", err)
		}
		return respx.OK(ctx, roleFromDomain(role)), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-delete-role", Method: http.MethodDelete, Path: "/iam/roles/{role_id}", Summary: "Delete a tenant role", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound},
	}, authz.Guard{Resource: "role", Verb: "delete"}, func(ctx context.Context, in *roleInput) (*respx.Body[MutationResult], error) {
		if err := svc.DeleteRole(ctx, in.TenantID, in.RoleID); err != nil {
			return nil, apiError("delete role", err)
		}
		return respx.OK(ctx, MutationResult{Changed: true, SyncStatus: "pending"}), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-list-role-permissions", Method: http.MethodGet, Path: "/iam/roles/{role_id}/permissions", Summary: "List role permission grants", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound},
	}, authz.Guard{Resource: "role", Verb: "view"}, func(ctx context.Context, in *roleInput) (*respx.Body[[]string], error) {
		codes, err := svc.ListPermissions(ctx, in.TenantID, in.RoleID)
		if err != nil {
			return nil, apiError("list role permissions", err)
		}
		return respx.OK(ctx, codes), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-grant-role-permission", Method: http.MethodPut, Path: "/iam/roles/{role_id}/permissions/{permission_code}", Summary: "Grant a permission to a role", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound},
	}, authz.Guard{Resource: "role", Verb: "grant_permission"}, func(ctx context.Context, in *permissionInput) (*respx.Body[MutationResult], error) {
		changed, err := svc.GrantPermission(ctx, in.TenantID, in.RoleID, in.PermissionCode)
		if err != nil {
			return nil, apiError("grant role permission", err)
		}
		return respx.OK(ctx, MutationResult{Changed: changed, SyncStatus: "pending"}), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-revoke-role-permission", Method: http.MethodDelete, Path: "/iam/roles/{role_id}/permissions/{permission_code}", Summary: "Revoke a permission from a role", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound},
	}, authz.Guard{Resource: "role", Verb: "grant_permission"}, func(ctx context.Context, in *permissionInput) (*respx.Body[MutationResult], error) {
		changed, err := svc.RevokePermission(ctx, in.TenantID, in.RoleID, in.PermissionCode)
		if err != nil {
			return nil, apiError("revoke role permission", err)
		}
		return respx.OK(ctx, MutationResult{Changed: changed, SyncStatus: "pending"}), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-list-role-members", Method: http.MethodGet, Path: "/iam/roles/{role_id}/members", Summary: "List immutable Zitadel subjects in a role", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound},
	}, authz.Guard{Resource: "role", Verb: "view"}, func(ctx context.Context, in *roleInput) (*respx.Body[[]string], error) {
		subjects, err := svc.ListMembers(ctx, in.TenantID, in.RoleID)
		if err != nil {
			return nil, apiError("list role members", err)
		}
		return respx.OK(ctx, subjects), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-add-role-member", Method: http.MethodPut, Path: "/iam/roles/{role_id}/members/{subject}", Summary: "Add a Zitadel subject to a role", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound},
	}, authz.Guard{Resource: "role", Verb: "manage_member"}, func(ctx context.Context, in *memberInput) (*respx.Body[MutationResult], error) {
		changed, err := svc.AddMember(ctx, in.TenantID, in.RoleID, in.Subject)
		if err != nil {
			return nil, apiError("add role member", err)
		}
		return respx.OK(ctx, MutationResult{Changed: changed, SyncStatus: "pending"}), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-remove-role-member", Method: http.MethodDelete, Path: "/iam/roles/{role_id}/members/{subject}", Summary: "Remove a Zitadel subject from a role", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound},
	}, authz.Guard{Resource: "role", Verb: "manage_member"}, func(ctx context.Context, in *memberInput) (*respx.Body[MutationResult], error) {
		changed, err := svc.RemoveMember(ctx, in.TenantID, in.RoleID, in.Subject)
		if err != nil {
			return nil, apiError("remove role member", err)
		}
		return respx.OK(ctx, MutationResult{Changed: changed, SyncStatus: "pending"}), nil
	})

	authz.Register(registrar, a, huma.Operation{OperationID: "iam-list-tenant-members", Method: http.MethodGet, Path: "/iam/members", Summary: "List tenant memberships", Tags: []string{"iam"}}, authz.Guard{Resource: "user", Verb: "view"}, func(ctx context.Context, in *listTenantMembersInput) (*respx.Body[[]TenantMember], error) {
		members, total, err := svc.ListTenantMembers(ctx, in.TenantID, iamdomain.MemberFilter{Search: in.Search, Status: in.Status, Offset: in.Offset, Limit: in.Limit})
		if err != nil {
			return nil, apiError("list tenant members", err)
		}
		return respx.List(ctx, membersFromDomain(members), respx.Page{Offset: in.Offset, Limit: in.Limit, Count: len(members), Total: total}), nil
	})
	authz.Register(registrar, a, huma.Operation{OperationID: "iam-create-tenant-member", Method: http.MethodPost, Path: "/iam/members", Summary: "Bind an existing Zitadel subject to a tenant", Tags: []string{"iam"}}, authz.Guard{Resource: "user", Verb: "create"}, func(ctx context.Context, in *createTenantMemberInput) (*respx.Body[TenantMember], error) {
		member, err := svc.PutTenantMember(ctx, in.TenantID, in.Body.Subject, in.Body.DisplayName, in.Body.Email, in.Body.Status)
		if err != nil {
			return nil, apiError("create tenant member", err)
		}
		return respx.OK(ctx, memberFromDomain(member)), nil
	})
	authz.Register(registrar, a, huma.Operation{OperationID: "iam-get-tenant-member", Method: http.MethodGet, Path: "/iam/members/{subject}", Summary: "Get a tenant membership", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound}}, authz.Guard{Resource: "user", Verb: "view"}, func(ctx context.Context, in *tenantMemberInput) (*respx.Body[TenantMember], error) {
		member, err := svc.GetTenantMember(ctx, in.TenantID, in.Subject)
		if err != nil {
			return nil, apiError("get tenant member", err)
		}
		roles, err := svc.ListTenantMemberRoles(ctx, in.TenantID, in.Subject)
		if err != nil {
			return nil, apiError("list tenant member roles", err)
		}
		out := memberFromDomain(member)
		out.RoleIDs = roles
		return respx.OK(ctx, out), nil
	})
	authz.Register(registrar, a, huma.Operation{OperationID: "iam-update-tenant-member", Method: http.MethodPatch, Path: "/iam/members/{subject}", Summary: "Update or disable a tenant membership", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound}}, authz.Guard{Resource: "user", Verb: "update"}, func(ctx context.Context, in *updateTenantMemberInput) (*respx.Body[TenantMember], error) {
		member, err := svc.GetTenantMember(ctx, in.TenantID, in.Subject)
		if err != nil {
			return nil, apiError("get tenant member", err)
		}
		if in.Body.DisplayName != nil {
			member.DisplayName = *in.Body.DisplayName
		}
		if in.Body.Email != nil {
			member.Email = *in.Body.Email
		}
		if in.Body.Status != nil {
			member.Status = *in.Body.Status
		}
		member, err = svc.PutTenantMember(ctx, in.TenantID, in.Subject, member.DisplayName, member.Email, member.Status)
		if err != nil {
			return nil, apiError("update tenant member", err)
		}
		return respx.OK(ctx, memberFromDomain(member)), nil
	})
	authz.Register(registrar, a, huma.Operation{OperationID: "iam-list-tenant-member-roles", Method: http.MethodGet, Path: "/iam/members/{subject}/roles", Summary: "List role assignments for a tenant member", Tags: []string{"iam"}}, authz.Guard{Resource: "role", Verb: "view"}, func(ctx context.Context, in *tenantMemberInput) (*respx.Body[[]string], error) {
		roles, err := svc.ListTenantMemberRoles(ctx, in.TenantID, in.Subject)
		if err != nil {
			return nil, apiError("list tenant member roles", err)
		}
		return respx.OK(ctx, roles), nil
	})

	authz.Register(registrar, a, huma.Operation{OperationID: "iam-list-menus", Method: http.MethodGet, Path: "/iam/menus", Summary: "List tenant menu metadata", Tags: []string{"iam"}}, authz.Guard{Resource: "menu", Verb: "view"}, func(ctx context.Context, in *tenantInput) (*respx.Body[[]Menu], error) {
		menus, err := svc.ListMenus(ctx, in.TenantID)
		if err != nil {
			return nil, apiError("list menus", err)
		}
		return respx.OK(ctx, menusFromDomain(menus)), nil
	})
	authz.Register(registrar, a, huma.Operation{OperationID: "iam-create-menu", Method: http.MethodPost, Path: "/iam/menus", Summary: "Create a tenant menu", Tags: []string{"iam"}}, authz.Guard{Resource: "menu", Verb: "create"}, func(ctx context.Context, in *createMenuInput) (*respx.Body[Menu], error) {
		menu, err := svc.CreateMenu(ctx, iamdomain.Menu{TenantID: in.TenantID, ParentID: in.Body.ParentID, Label: in.Body.Label, Route: in.Body.Route, Icon: in.Body.Icon, SortOrder: in.Body.SortOrder, PermissionCode: in.Body.PermissionCode, Status: in.Body.Status})
		if err != nil {
			return nil, apiError("create menu", err)
		}
		return respx.OK(ctx, menuFromDomain(menu)), nil
	})
	authz.Register(registrar, a, huma.Operation{OperationID: "iam-get-menu", Method: http.MethodGet, Path: "/iam/menus/{menu_id}", Summary: "Get a tenant menu", Tags: []string{"iam"}}, authz.Guard{Resource: "menu", Verb: "view"}, func(ctx context.Context, in *menuInput) (*respx.Body[Menu], error) {
		menu, err := svc.GetMenu(ctx, in.TenantID, in.MenuID)
		if err != nil {
			return nil, apiError("get menu", err)
		}
		return respx.OK(ctx, menuFromDomain(menu)), nil
	})
	authz.Register(registrar, a, huma.Operation{OperationID: "iam-update-menu", Method: http.MethodPatch, Path: "/iam/menus/{menu_id}", Summary: "Update a tenant menu", Tags: []string{"iam"}}, authz.Guard{Resource: "menu", Verb: "update"}, func(ctx context.Context, in *updateMenuInput) (*respx.Body[Menu], error) {
		menu, err := svc.GetMenu(ctx, in.TenantID, in.MenuID)
		if err != nil {
			return nil, apiError("get menu", err)
		}
		if in.Body.ParentID != nil {
			menu.ParentID = *in.Body.ParentID
		}
		if in.Body.Label != nil {
			menu.Label = *in.Body.Label
		}
		if in.Body.Route != nil {
			menu.Route = *in.Body.Route
		}
		if in.Body.Icon != nil {
			menu.Icon = *in.Body.Icon
		}
		if in.Body.SortOrder != nil {
			menu.SortOrder = *in.Body.SortOrder
		}
		if in.Body.PermissionCode != nil {
			menu.PermissionCode = *in.Body.PermissionCode
		}
		if in.Body.Status != nil {
			menu.Status = *in.Body.Status
		}
		menu, err = svc.UpdateMenu(ctx, menu)
		if err != nil {
			return nil, apiError("update menu", err)
		}
		return respx.OK(ctx, menuFromDomain(menu)), nil
	})
	authz.Register(registrar, a, huma.Operation{OperationID: "iam-delete-menu", Method: http.MethodDelete, Path: "/iam/menus/{menu_id}", Summary: "Delete a tenant menu", Tags: []string{"iam"}}, authz.Guard{Resource: "menu", Verb: "delete"}, func(ctx context.Context, in *menuInput) (*respx.Body[MutationResult], error) {
		if err := svc.DeleteMenu(ctx, in.TenantID, in.MenuID); err != nil {
			return nil, apiError("delete menu", err)
		}
		return respx.OK(ctx, MutationResult{Changed: true, SyncStatus: "not_applicable"}), nil
	})
	authz.Register(registrar, a, huma.Operation{OperationID: "iam-effective-menus", Method: http.MethodGet, Path: "/iam/me/menus", Summary: "Return the current member's authorized menu tree", Tags: []string{"iam"}}, authz.Guard{Resource: "menu", Verb: "view"}, func(ctx context.Context, in *tenantInput) (*respx.Body[[]MenuItem], error) {
		claims, ok := authnext.FromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("unauthorized")
		}
		menus, err := svc.EffectiveMenus(ctx, in.TenantID, claims.Subject)
		if err != nil {
			return nil, apiError("effective menus", err)
		}
		return respx.OK(ctx, menus), nil
	})
}

func roleFromDomain(role iamdomain.Role) Role {
	return Role{ID: role.ID, TenantID: role.TenantID, Name: role.Name, Description: role.Description, CreatedAt: role.CreatedAt, UpdatedAt: role.UpdatedAt}
}

func rolesFromDomain(roles []iamdomain.Role) []Role {
	out := make([]Role, 0, len(roles))
	for _, role := range roles {
		out = append(out, roleFromDomain(role))
	}
	return out
}

func memberFromDomain(member iamdomain.TenantMember) TenantMember {
	out := TenantMember{TenantID: member.TenantID, Subject: member.Subject, DisplayName: member.DisplayName, Email: member.Email, Status: member.Status, CreatedAt: member.CreatedAt, UpdatedAt: member.UpdatedAt}
	if !member.DisabledAt.IsZero() {
		value := member.DisabledAt
		out.DisabledAt = &value
	}
	return out
}

func membersFromDomain(members []iamdomain.TenantMember) []TenantMember {
	out := make([]TenantMember, 0, len(members))
	for _, member := range members {
		out = append(out, memberFromDomain(member))
	}
	return out
}
func menuFromDomain(menu iamdomain.Menu) Menu {
	return Menu{ID: menu.ID, TenantID: menu.TenantID, ParentID: menu.ParentID, Label: menu.Label, Route: menu.Route, Icon: menu.Icon, SortOrder: menu.SortOrder, PermissionCode: menu.PermissionCode, Status: menu.Status, CreatedAt: menu.CreatedAt, UpdatedAt: menu.UpdatedAt}
}
func menusFromDomain(menus []iamdomain.Menu) []Menu {
	out := make([]Menu, 0, len(menus))
	for _, menu := range menus {
		out = append(out, menuFromDomain(menu))
	}
	return out
}

func apiError(operation string, err error) error {
	switch {
	case errors.Is(err, iamdomain.ErrRoleNotFound):
		return huma.Error404NotFound("role_not_found")
	case errors.Is(err, iamdomain.ErrRoleNameConflict):
		return huma.Error409Conflict("iam_role_name_conflict")
	case errors.Is(err, iamdomain.ErrMemberNotFound), errors.Is(err, iamdomain.ErrMenuNotFound):
		return huma.Error404NotFound("iam_resource_not_found")
	case errors.Is(err, iamdomain.ErrMenuConflict), errors.Is(err, iamdomain.ErrMenuHasChildren):
		return huma.Error409Conflict("iam_resource_conflict")
	case errors.Is(err, iamdomain.ErrMemberInactive):
		return huma.Error409Conflict("tenant_member_inactive")
	case errors.Is(err, iamdomain.ErrInvalidArgument), errors.Is(err, iamdomain.ErrPermissionNotFound):
		return huma.Error422UnprocessableEntity("validation_failed", err)
	default:
		slog.Error("iam request failed", "operation", operation, "err", err)
		return huma.Error500InternalServerError("internal_server_error")
	}
}
