package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

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
	Children       []MenuItem `json:"children,omitempty"`
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

func apiError(operation string, err error) error {
	switch {
	case errors.Is(err, iamdomain.ErrRoleNotFound):
		return huma.Error404NotFound("role_not_found")
	case errors.Is(err, iamdomain.ErrRoleNameConflict):
		return huma.Error409Conflict("iam_role_name_conflict")
	case errors.Is(err, iamdomain.ErrInvalidArgument), errors.Is(err, iamdomain.ErrPermissionNotFound):
		return huma.Error422UnprocessableEntity("validation_failed", err)
	default:
		slog.Error("iam request failed", "operation", operation, "err", err)
		return huma.Error500InternalServerError("internal_server_error")
	}
}
