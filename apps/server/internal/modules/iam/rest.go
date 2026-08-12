package iam

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	iamdomain "github.com/chaos-plus/chaosplus/internal/modules/iam/domain"
)

func requireID(value string) (guid.ID, error) {
	id, err := guid.Parse(strings.TrimSpace(value))
	if err != nil {
		return 0, huma.Error422UnprocessableEntity("invalid_id")
	}
	return id, nil
}

func optionalID(value string) (guid.ID, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	return requireID(value)
}

func requireIDs(values []string) ([]guid.ID, error) {
	ids := make([]guid.ID, 0, len(values))
	for _, value := range values {
		id, err := requireID(value)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func roleRef(in *roleInput) (guid.ID, guid.ID, error) {
	return roleRefIDs(in.TenantID, in.RoleID)
}

func roleRefIDs(tenantIDValue, roleIDValue string) (guid.ID, guid.ID, error) {
	tenantID, err := requireID(tenantIDValue)
	if err != nil {
		return 0, 0, err
	}
	roleID, err := requireID(roleIDValue)
	if err != nil {
		return 0, 0, err
	}
	return tenantID, roleID, nil
}

func memberRef(in *memberInput) (guid.ID, guid.ID, guid.ID, error) {
	tenantID, err := requireID(in.TenantID)
	if err != nil {
		return 0, 0, 0, err
	}
	roleID, err := requireID(in.RoleID)
	if err != nil {
		return 0, 0, 0, err
	}
	subject, err := requireID(in.Subject)
	if err != nil {
		return 0, 0, 0, err
	}
	return tenantID, roleID, subject, nil
}

func tenantMemberRef(in *tenantMemberInput) (guid.ID, guid.ID, error) {
	tenantID, err := requireID(in.TenantID)
	if err != nil {
		return 0, 0, err
	}
	subject, err := requireID(in.Subject)
	if err != nil {
		return 0, 0, err
	}
	return tenantID, subject, nil
}

func menuRef(in *menuInput) (guid.ID, guid.ID, error) {
	tenantID, err := requireID(in.TenantID)
	if err != nil {
		return 0, 0, err
	}
	menuID, err := requireID(in.MenuID)
	if err != nil {
		return 0, 0, err
	}
	return tenantID, menuID, nil
}

type ScopeNode struct {
	Type       string `json:"type" doc:"authorization scope type"`
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

type APITenantMember struct {
	TenantID     string                 `json:"tenant_id"`
	Subject      string                 `json:"subject"`
	DisplayName  string                 `json:"display_name"`
	Email        string                 `json:"email,omitempty"`
	DepartmentID string                 `json:"department_id,omitempty"`
	Status       iamdomain.MemberStatus `json:"status"`
	RoleIDs      []string               `json:"role_ids,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
	DisabledAt   *time.Time             `json:"disabled_at,omitempty"`
}

type APIMenu struct {
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

type APIRole struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type APIRolePermissionGrant struct {
	PermissionCode string          `json:"permission_code"`
	Condition      policyCondition `json:"condition,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

type APIRoleDirectoryBinding struct {
	RoleID       string                          `json:"role_id"`
	AssigneeType iamdomain.DirectoryAssigneeType `json:"assignee_type" enum:"group,position"`
	AssigneeID   string                          `json:"assignee_id"`
	CreatedAt    time.Time                       `json:"created_at"`
}

type APIRoleDataScope struct {
	RoleID        string              `json:"role_id"`
	Scope         iamdomain.DataScope `json:"scope" enum:"all,self,department,department_and_descendants,selected_departments"`
	DepartmentIDs []string            `json:"department_ids"`
	UpdatedAt     *time.Time          `json:"updated_at,omitempty"`
}

type MutationResult struct {
	Changed    bool   `json:"changed" doc:"true when the local desired binding changed"`
	SyncStatus string `json:"sync_status" doc:"local transaction state; applied means immediately effective"`
}

type Entity struct {
	ID        string                 `json:"id"`
	TenantID  string                 `json:"tenant_id"`
	ParentID  string                 `json:"parent_id,omitempty"`
	Type      string                 `json:"type"`
	Name      string                 `json:"name"`
	Status    iamdomain.EntityStatus `json:"status"`
	Metadata  map[string]any         `json:"metadata"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

type EntityRoleBinding struct {
	EntityID    string                  `json:"entity_id"`
	RoleID      string                  `json:"role_id"`
	PrincipalID string                  `json:"principal_id"`
	Effect      iamdomain.BindingEffect `json:"effect" enum:"allow,deny"`
	ExpiresAt   *time.Time              `json:"expires_at,omitempty"`
	CreatedAt   time.Time               `json:"created_at"`
}

type tenantInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128" doc:"tenant authorization boundary"`
	// ponytail: pagination added to prevent unbounded list responses.
	// Offset and Limit are optional; 0/0 returns all records.
	Offset int `query:"offset" minimum:"0" default:"0"`
	Limit  int `query:"limit" minimum:"0" maximum:"200" default:"0"`
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

type setRoleDataScopeInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	RoleID   string `path:"role_id" maxLength:"32"`
	Body     struct {
		Scope         iamdomain.DataScope `json:"scope" enum:"all,self,department,department_and_descendants,selected_departments"`
		DepartmentIDs []string            `json:"department_ids,omitempty" maxItems:"200"`
	}
}

type permissionInput struct {
	TenantID       string `header:"X-Tenant-Id" maxLength:"128"`
	RoleID         string `path:"role_id" maxLength:"32"`
	PermissionCode string `path:"permission_code" maxLength:"128"`
}

type setPermissionConditionInput struct {
	TenantID       string `header:"X-Tenant-Id" maxLength:"128"`
	RoleID         string `path:"role_id" maxLength:"32"`
	PermissionCode string `path:"permission_code" maxLength:"128"`
	Body           struct {
		Condition policyCondition `json:"condition"`
	}
}

type memberInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	RoleID   string `path:"role_id" maxLength:"32"`
	Subject  string `path:"subject" maxLength:"255" doc:"immutable local principal ID"`
}

type directoryBindingInput struct {
	TenantID     string                          `header:"X-Tenant-Id" maxLength:"128"`
	RoleID       string                          `path:"role_id" maxLength:"32"`
	AssigneeType iamdomain.DirectoryAssigneeType `path:"assignee_type" enum:"group,position"`
	AssigneeID   string                          `path:"assignee_id" maxLength:"128"`
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
		Subject      string                 `json:"subject" minLength:"1" maxLength:"255"`
		DisplayName  string                 `json:"display_name" minLength:"1" maxLength:"128"`
		Email        string                 `json:"email,omitempty" maxLength:"320"`
		DepartmentID string                 `json:"department_id,omitempty" maxLength:"128"`
		Status       iamdomain.MemberStatus `json:"status,omitempty" enum:"active,disabled" default:"active"`
	}
}

type updateTenantMemberInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	Subject  string `path:"subject" maxLength:"255"`
	Body     struct {
		DisplayName  *string                 `json:"display_name,omitempty" minLength:"1" maxLength:"128"`
		Email        *string                 `json:"email,omitempty" maxLength:"320"`
		DepartmentID *string                 `json:"department_id,omitempty" maxLength:"128"`
		Status       *iamdomain.MemberStatus `json:"status,omitempty" enum:"active,disabled"`
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
		SortOrder      int                  `json:"sort_order,omitempty" minimum:"-100000" maximum:"100000" default:"0"`
		PermissionCode string               `json:"permission_code,omitempty" maxLength:"128"`
		Status         iamdomain.MenuStatus `json:"status,omitempty" enum:"active,disabled" default:"active"`
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

type entityInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	EntityID string `path:"entity_id" maxLength:"64"`
}

type createEntityInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	Body     struct {
		ParentID string                 `json:"parent_id,omitempty" maxLength:"64"`
		Type     string                 `json:"type" minLength:"1" maxLength:"64"`
		Name     string                 `json:"name" minLength:"1" maxLength:"200"`
		Status   iamdomain.EntityStatus `json:"status,omitempty" enum:"active,disabled" default:"active"`
		Metadata map[string]any         `json:"metadata,omitempty"`
	}
}

type updateEntityInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	EntityID string `path:"entity_id" maxLength:"64"`
	Body     struct {
		ParentID *string                 `json:"parent_id,omitempty" maxLength:"64"`
		Type     *string                 `json:"type,omitempty" minLength:"1" maxLength:"64"`
		Name     *string                 `json:"name,omitempty" minLength:"1" maxLength:"200"`
		Status   *iamdomain.EntityStatus `json:"status,omitempty" enum:"active,disabled"`
		Metadata *map[string]any         `json:"metadata,omitempty"`
	}
}

type entityRoleBindingRef struct {
	TenantID    string `header:"X-Tenant-Id" maxLength:"128"`
	EntityID    string `path:"entity_id" maxLength:"64"`
	RoleID      string `path:"role_id" maxLength:"32"`
	PrincipalID string `path:"principal_id" maxLength:"64"`
}

type putEntityRoleBindingInput struct {
	TenantID    string `header:"X-Tenant-Id" maxLength:"128"`
	EntityID    string `path:"entity_id" maxLength:"64"`
	RoleID      string `path:"role_id" maxLength:"32"`
	PrincipalID string `path:"principal_id" maxLength:"64"`
	Body        struct {
		Effect    iamdomain.BindingEffect `json:"effect" enum:"allow,deny"`
		ExpiresAt *time.Time              `json:"expires_at,omitempty"`
	}
}

type authorizationConstraintInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	Body     struct {
		PermissionCode string `json:"permission_code" maxLength:"128"`
		Subject        string `json:"subject" maxLength:"255"`
	}
}

type authorizationExplanationInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	Body     struct {
		EntityID       string `json:"entity_id" maxLength:"64"`
		ResourceType   string `json:"resource_type,omitempty" maxLength:"64"`
		ResourceID     string `json:"resource_id,omitempty" maxLength:"255"`
		PermissionCode string `json:"permission_code" maxLength:"128"`
		Subject        string `json:"subject" maxLength:"255"`
	}
}

// RegisterREST mounts IAM discovery endpoints for the management UI.
func RegisterREST(a huma.API, svc *Service, registrar *authz.Registrar) {
	registerRelationshipREST(a, svc, registrar)
	registerPlatformREST(a, svc, registrar)
	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-permission-catalog",
		Method:      http.MethodGet,
		Path:        "/iam/permission-catalog",
		Summary:     "List declared permissions",
		Tags:        []string{"iam"},
	}, authz.Guard{Resource: "role", Verb: "view"}, func(ctx context.Context, _ *struct{}) (*respx.Body[[]authz.Action], error) {
		return respx.OK(ctx, svc.PermissionCatalog(ctx)), nil
	})

	authz.RegisterPlatform(registrar, a, huma.Operation{
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
		OperationID: "iam-authorization-constraint", Method: http.MethodPost, Path: "/iam/authorization/constraints", Summary: "Compute an entity data constraint for a tenant subject", Tags: []string{"iam"}, Errors: []int{http.StatusUnprocessableEntity},
	}, authz.Guard{Resource: "role", Verb: "view"}, func(ctx context.Context, in *authorizationConstraintInput) (*respx.Body[authz.DataConstraint], error) {
		tenantID, err := requireID(in.TenantID)
		if err != nil {
			return nil, err
		}
		subject, err := requireID(in.Body.Subject)
		if err != nil {
			return nil, err
		}
		constraint, err := svc.AuthorizationConstraint(ctx, tenantID, in.Body.PermissionCode, subject)
		if err != nil {
			return nil, apiError("compute authorization constraint", err)
		}
		return respx.OK(ctx, constraint), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-explain-authorization", Method: http.MethodPost, Path: "/iam/authorization/explain", Summary: "Explain an entity or business-resource authorization decision", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity},
	}, authz.Guard{Resource: "role", Verb: "view"}, func(ctx context.Context, in *authorizationExplanationInput) (*respx.Body[authz.Explanation], error) {
		var explanation authz.Explanation
		tenantID, err := requireID(in.TenantID)
		if err != nil {
			return nil, err
		}
		entityID, err := requireID(in.Body.EntityID)
		if err != nil {
			return nil, err
		}
		subject, err := requireID(in.Body.Subject)
		if err != nil {
			return nil, err
		}
		if in.Body.ResourceType != "" || in.Body.ResourceID != "" {
			resourceID, err := requireID(in.Body.ResourceID)
			if err != nil {
				return nil, err
			}
			explanation, err = svc.ExplainResourceAuthorization(ctx, tenantID, entityID, in.Body.ResourceType, resourceID, in.Body.PermissionCode, subject)
			if err != nil {
				return nil, apiError("explain resource authorization", err)
			}
		} else {
			explanation, err = svc.ExplainEntityAuthorization(ctx, tenantID, entityID, in.Body.PermissionCode, subject)
			if err != nil {
				return nil, apiError("explain entity authorization", err)
			}
		}
		return respx.OK(ctx, explanation), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-list-entities", Method: http.MethodGet, Path: "/iam/entities", Summary: "List tenant entities", Tags: []string{"iam"},
	}, authz.Guard{Resource: "entity", Verb: "view"}, func(ctx context.Context, in *tenantInput) (*respx.Body[[]Entity], error) {
		tenantID, err := requireID(in.TenantID)
		if err != nil {
			return nil, err
		}
		entities, total, err := svc.ListEntitiesPage(ctx, tenantID, in.Offset, in.Limit)
		if err != nil {
			return nil, apiError("list entities", err)
		}
		return respx.List(ctx, entitiesFromDomain(entities), respx.Page{Offset: in.Offset, Limit: in.Limit, Total: total}), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-create-entity", Method: http.MethodPost, Path: "/iam/entities", Summary: "Create a tenant entity", Tags: []string{"iam"}, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity},
	}, authz.Guard{Resource: "entity", Verb: "create"}, func(ctx context.Context, in *createEntityInput) (*respx.Body[Entity], error) {
		tenantID, err := requireID(in.TenantID)
		if err != nil {
			return nil, err
		}
		parentID, err := optionalID(in.Body.ParentID)
		if err != nil {
			return nil, err
		}
		entity, err := svc.CreateEntity(ctx, iamdomain.Entity{
			TenantID: tenantID, ParentID: parentID, Type: in.Body.Type, Name: in.Body.Name,
			Status: in.Body.Status, Metadata: in.Body.Metadata,
		})
		if err != nil {
			return nil, apiError("create entity", err)
		}
		return respx.OK(ctx, entityFromDomain(entity)), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-get-entity", Method: http.MethodGet, Path: "/iam/entities/{entity_id}", Summary: "Get a tenant entity", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound},
	}, authz.Guard{Resource: "entity", Verb: "view"}, func(ctx context.Context, in *entityInput) (*respx.Body[Entity], error) {
		tenantID, err := requireID(in.TenantID)
		if err != nil {
			return nil, err
		}
		entityID, err := requireID(in.EntityID)
		if err != nil {
			return nil, err
		}
		entity, err := svc.GetEntity(ctx, tenantID, entityID)
		if err != nil {
			return nil, apiError("get entity", err)
		}
		return respx.OK(ctx, entityFromDomain(entity)), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-update-entity", Method: http.MethodPatch, Path: "/iam/entities/{entity_id}", Summary: "Update a tenant entity", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity},
	}, authz.Guard{Resource: "entity", Verb: "update"}, func(ctx context.Context, in *updateEntityInput) (*respx.Body[Entity], error) {
		tenantID, err := requireID(in.TenantID)
		if err != nil {
			return nil, err
		}
		entityID, err := requireID(in.EntityID)
		if err != nil {
			return nil, err
		}
		patch := iamdomain.EntityPatch{Type: in.Body.Type, Name: in.Body.Name, Status: in.Body.Status, Metadata: in.Body.Metadata}
		if in.Body.ParentID != nil {
			parentID, err := requireID(*in.Body.ParentID)
			if err != nil {
				return nil, err
			}
			patch.ParentID = &parentID
		}
		entity, err := svc.UpdateEntity(ctx, tenantID, entityID, patch)
		if err != nil {
			return nil, apiError("update entity", err)
		}
		return respx.OK(ctx, entityFromDomain(entity)), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-delete-entity", Method: http.MethodDelete, Path: "/iam/entities/{entity_id}", Summary: "Delete an empty tenant entity", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound, http.StatusConflict},
	}, authz.Guard{Resource: "entity", Verb: "delete"}, func(ctx context.Context, in *entityInput) (*respx.Body[MutationResult], error) {
		tenantID, err := requireID(in.TenantID)
		if err != nil {
			return nil, err
		}
		entityID, err := requireID(in.EntityID)
		if err != nil {
			return nil, err
		}
		if err := svc.DeleteEntity(ctx, tenantID, entityID); err != nil {
			return nil, apiError("delete entity", err)
		}
		return respx.OK(ctx, MutationResult{Changed: true, SyncStatus: "applied"}), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-list-entity-role-bindings", Method: http.MethodGet, Path: "/iam/entities/{entity_id}/role-bindings", Summary: "List direct principal role bindings for an entity", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound},
	}, authz.Guard{Resource: "entity", Verb: "view"}, func(ctx context.Context, in *entityInput) (*respx.Body[[]EntityRoleBinding], error) {
		tenantID, err := requireID(in.TenantID)
		if err != nil {
			return nil, err
		}
		entityID, err := requireID(in.EntityID)
		if err != nil {
			return nil, err
		}
		bindings, err := svc.ListEntityRoleBindings(ctx, tenantID, entityID)
		if err != nil {
			return nil, apiError("list entity role bindings", err)
		}
		return respx.OK(ctx, entityBindingsFromDomain(bindings)), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-put-entity-role-binding", Method: http.MethodPut, Path: "/iam/entities/{entity_id}/role-bindings/{role_id}/{principal_id}", Summary: "Assign a direct principal role at an entity scope", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity},
	}, authz.Guard{Resource: "entity", Verb: "manage_binding"}, func(ctx context.Context, in *putEntityRoleBindingInput) (*respx.Body[EntityRoleBinding], error) {
		tenantID, err := requireID(in.TenantID)
		if err != nil {
			return nil, err
		}
		entityID, err := requireID(in.EntityID)
		if err != nil {
			return nil, err
		}
		roleID, err := requireID(in.RoleID)
		if err != nil {
			return nil, err
		}
		principalID, err := requireID(in.PrincipalID)
		if err != nil {
			return nil, err
		}
		expiresAt := time.Time{}
		if in.Body.ExpiresAt != nil {
			expiresAt = *in.Body.ExpiresAt
		}
		binding, _, err := svc.PutEntityRoleBinding(ctx, tenantID, entityID, roleID, principalID, in.Body.Effect, expiresAt)
		if err != nil {
			return nil, apiError("put entity role binding", err)
		}
		return respx.OK(ctx, entityBindingFromDomain(binding)), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-delete-entity-role-binding", Method: http.MethodDelete, Path: "/iam/entities/{entity_id}/role-bindings/{role_id}/{principal_id}", Summary: "Remove a direct principal role from an entity scope", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity},
	}, authz.Guard{Resource: "entity", Verb: "manage_binding"}, func(ctx context.Context, in *entityRoleBindingRef) (*respx.Body[MutationResult], error) {
		tenantID, err := requireID(in.TenantID)
		if err != nil {
			return nil, err
		}
		entityID, err := requireID(in.EntityID)
		if err != nil {
			return nil, err
		}
		roleID, err := requireID(in.RoleID)
		if err != nil {
			return nil, err
		}
		principalID, err := requireID(in.PrincipalID)
		if err != nil {
			return nil, err
		}
		changed, err := svc.DeleteEntityRoleBinding(ctx, tenantID, entityID, roleID, principalID)
		if err != nil {
			return nil, apiError("delete entity role binding", err)
		}
		return respx.OK(ctx, MutationResult{Changed: changed, SyncStatus: "applied"}), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-list-roles", Method: http.MethodGet, Path: "/iam/roles", Summary: "List tenant roles", Tags: []string{"iam"},
	}, authz.Guard{Resource: "role", Verb: "view"}, func(ctx context.Context, in *tenantInput) (*respx.Body[[]APIRole], error) {
		tenantID, err := requireID(in.TenantID)
		if err != nil {
			return nil, err
		}
		roles, total, err := svc.ListRolesPage(ctx, tenantID, in.Offset, in.Limit)
		if err != nil {
			return nil, apiError("list roles", err)
		}
		return respx.List(ctx, rolesFromDomain(roles), respx.Page{Offset: in.Offset, Limit: in.Limit, Total: total}), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-create-role", Method: http.MethodPost, Path: "/iam/roles", Summary: "Create a tenant role", Tags: []string{"iam"}, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusConflict},
	}, authz.Guard{Resource: "role", Verb: "create"}, func(ctx context.Context, in *createRoleInput) (*respx.Body[APIRole], error) {
		tenantID, err := requireID(in.TenantID)
		if err != nil {
			return nil, err
		}
		role, err := svc.CreateRole(ctx, tenantID, in.Body.Name, in.Body.Description)
		if err != nil {
			return nil, apiError("create role", err)
		}
		return respx.OK(ctx, roleFromDomain(role)), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-get-role", Method: http.MethodGet, Path: "/iam/roles/{role_id}", Summary: "Get a tenant role", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound},
	}, authz.Guard{Resource: "role", Verb: "view"}, func(ctx context.Context, in *roleInput) (*respx.Body[APIRole], error) {
		tenantID, roleID, err := roleRef(in)
		if err != nil {
			return nil, err
		}
		role, err := svc.GetRole(ctx, tenantID, roleID)
		if err != nil {
			return nil, apiError("get role", err)
		}
		return respx.OK(ctx, roleFromDomain(role)), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-get-role-data-scope", Method: http.MethodGet, Path: "/iam/roles/{role_id}/data-scope", Summary: "Get a role data scope", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity},
	}, authz.Guard{Resource: "role", Verb: "view"}, func(ctx context.Context, in *roleInput) (*respx.Body[APIRoleDataScope], error) {
		tenantID, roleID, err := roleRef(in)
		if err != nil {
			return nil, err
		}
		scope, err := svc.GetRoleDataScope(ctx, tenantID, roleID)
		if err != nil {
			return nil, apiError("get role data scope", err)
		}
		return respx.OK(ctx, roleDataScopeFromDomain(scope)), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-set-role-data-scope", Method: http.MethodPut, Path: "/iam/roles/{role_id}/data-scope", Summary: "Set a role data scope", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity},
	}, authz.Guard{Resource: "role", Verb: "update"}, func(ctx context.Context, in *setRoleDataScopeInput) (*respx.Body[APIRoleDataScope], error) {
		tenantID, roleID, err := roleRefIDs(in.TenantID, in.RoleID)
		if err != nil {
			return nil, err
		}
		departmentIDs, err := requireIDs(in.Body.DepartmentIDs)
		if err != nil {
			return nil, err
		}
		scope, _, err := svc.SetRoleDataScope(ctx, tenantID, roleID, in.Body.Scope, departmentIDs)
		if err != nil {
			return nil, apiError("set role data scope", err)
		}
		return respx.OK(ctx, roleDataScopeFromDomain(scope)), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-update-role", Method: http.MethodPatch, Path: "/iam/roles/{role_id}", Summary: "Update a tenant role", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound, http.StatusConflict},
	}, authz.Guard{Resource: "role", Verb: "update"}, func(ctx context.Context, in *updateRoleInput) (*respx.Body[APIRole], error) {
		tenantID, roleID, err := roleRefIDs(in.TenantID, in.RoleID)
		if err != nil {
			return nil, err
		}
		role, err := svc.UpdateRole(ctx, tenantID, roleID, in.Body.Name, in.Body.Description)
		if err != nil {
			return nil, apiError("update role", err)
		}
		return respx.OK(ctx, roleFromDomain(role)), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-delete-role", Method: http.MethodDelete, Path: "/iam/roles/{role_id}", Summary: "Delete a tenant role", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound, http.StatusConflict},
	}, authz.Guard{Resource: "role", Verb: "delete"}, func(ctx context.Context, in *roleInput) (*respx.Body[MutationResult], error) {
		tenantID, roleID, err := roleRef(in)
		if err != nil {
			return nil, err
		}
		if err := svc.DeleteRole(ctx, tenantID, roleID); err != nil {
			return nil, apiError("delete role", err)
		}
		return respx.OK(ctx, MutationResult{Changed: true, SyncStatus: "applied"}), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-list-role-permissions", Method: http.MethodGet, Path: "/iam/roles/{role_id}/permissions", Summary: "List role permission grants", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound},
	}, authz.Guard{Resource: "role", Verb: "view"}, func(ctx context.Context, in *roleInput) (*respx.Body[[]string], error) {
		tenantID, roleID, err := roleRef(in)
		if err != nil {
			return nil, err
		}
		codes, err := svc.ListPermissions(ctx, tenantID, roleID)
		if err != nil {
			return nil, apiError("list role permissions", err)
		}
		return respx.OK(ctx, codes), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-list-role-permission-grants", Method: http.MethodGet, Path: "/iam/roles/{role_id}/permission-grants", Summary: "List role permission grants with conditions", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound},
	}, authz.Guard{Resource: "role", Verb: "view"}, func(ctx context.Context, in *roleInput) (*respx.Body[[]APIRolePermissionGrant], error) {
		tenantID, roleID, err := roleRef(in)
		if err != nil {
			return nil, err
		}
		grants, err := svc.ListPermissionGrants(ctx, tenantID, roleID)
		if err != nil {
			return nil, apiError("list role permission grants", err)
		}
		return respx.OK(ctx, rolePermissionGrantsFromDomain(grants)), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-grant-role-permission", Method: http.MethodPut, Path: "/iam/roles/{role_id}/permissions/{permission_code}", Summary: "Grant a permission to a role", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound},
	}, authz.Guard{Resource: "role", Verb: "grant_permission"}, func(ctx context.Context, in *permissionInput) (*respx.Body[MutationResult], error) {
		tenantID, roleID, err := roleRefIDs(in.TenantID, in.RoleID)
		if err != nil {
			return nil, err
		}
		changed, err := svc.GrantPermission(ctx, tenantID, roleID, in.PermissionCode)
		if err != nil {
			return nil, apiError("grant role permission", err)
		}
		return respx.OK(ctx, MutationResult{Changed: changed, SyncStatus: "applied"}), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-revoke-role-permission", Method: http.MethodDelete, Path: "/iam/roles/{role_id}/permissions/{permission_code}", Summary: "Revoke a permission from a role", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound, http.StatusConflict},
	}, authz.Guard{Resource: "role", Verb: "grant_permission"}, func(ctx context.Context, in *permissionInput) (*respx.Body[MutationResult], error) {
		tenantID, roleID, err := roleRefIDs(in.TenantID, in.RoleID)
		if err != nil {
			return nil, err
		}
		changed, err := svc.RevokePermission(ctx, tenantID, roleID, in.PermissionCode)
		if err != nil {
			return nil, apiError("revoke role permission", err)
		}
		return respx.OK(ctx, MutationResult{Changed: changed, SyncStatus: "applied"}), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-set-role-permission-condition", Method: http.MethodPut, Path: "/iam/roles/{role_id}/permissions/{permission_code}/condition", Summary: "Set a trusted-context condition on a role permission", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity},
	}, authz.Guard{Resource: "role", Verb: "grant_permission"}, func(ctx context.Context, in *setPermissionConditionInput) (*respx.Body[APIRolePermissionGrant], error) {
		tenantID, roleID, err := roleRefIDs(in.TenantID, in.RoleID)
		if err != nil {
			return nil, err
		}
		grant, _, err := svc.SetPermissionCondition(ctx, tenantID, roleID, in.PermissionCode, json.RawMessage(in.Body.Condition))
		if err != nil {
			return nil, apiError("set role permission condition", err)
		}
		return respx.OK(ctx, rolePermissionGrantFromDomain(grant)), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-clear-role-permission-condition", Method: http.MethodDelete, Path: "/iam/roles/{role_id}/permissions/{permission_code}/condition", Summary: "Clear a role permission condition", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound},
	}, authz.Guard{Resource: "role", Verb: "grant_permission"}, func(ctx context.Context, in *permissionInput) (*respx.Body[MutationResult], error) {
		tenantID, roleID, err := roleRefIDs(in.TenantID, in.RoleID)
		if err != nil {
			return nil, err
		}
		_, changed, err := svc.SetPermissionCondition(ctx, tenantID, roleID, in.PermissionCode, nil)
		if err != nil {
			return nil, apiError("clear role permission condition", err)
		}
		return respx.OK(ctx, MutationResult{Changed: changed, SyncStatus: "applied"}), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-list-role-members", Method: http.MethodGet, Path: "/iam/roles/{role_id}/members", Summary: "List local principals in a role", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound},
	}, authz.Guard{Resource: "role", Verb: "view"}, func(ctx context.Context, in *roleInput) (*respx.Body[[]string], error) {
		tenantID, roleID, err := roleRef(in)
		if err != nil {
			return nil, err
		}
		subjects, err := svc.ListMembers(ctx, tenantID, roleID)
		if err != nil {
			return nil, apiError("list role members", err)
		}
		return respx.OK(ctx, guidStrings(subjects)), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-add-role-member", Method: http.MethodPut, Path: "/iam/roles/{role_id}/members/{subject}", Summary: "Add a local principal to a role", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound},
	}, authz.Guard{Resource: "role", Verb: "manage_member"}, func(ctx context.Context, in *memberInput) (*respx.Body[MutationResult], error) {
		tenantID, roleID, subject, err := memberRef(in)
		if err != nil {
			return nil, err
		}
		changed, err := svc.AddMember(ctx, tenantID, roleID, subject)
		if err != nil {
			return nil, apiError("add role member", err)
		}
		return respx.OK(ctx, MutationResult{Changed: changed, SyncStatus: "applied"}), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-remove-role-member", Method: http.MethodDelete, Path: "/iam/roles/{role_id}/members/{subject}", Summary: "Remove a local principal from a role", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound, http.StatusConflict},
	}, authz.Guard{Resource: "role", Verb: "manage_member"}, func(ctx context.Context, in *memberInput) (*respx.Body[MutationResult], error) {
		tenantID, roleID, subject, err := memberRef(in)
		if err != nil {
			return nil, err
		}
		changed, err := svc.RemoveMember(ctx, tenantID, roleID, subject)
		if err != nil {
			return nil, apiError("remove role member", err)
		}
		return respx.OK(ctx, MutationResult{Changed: changed, SyncStatus: "applied"}), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-list-role-directory-bindings", Method: http.MethodGet, Path: "/iam/roles/{role_id}/directory-bindings", Summary: "List group and position role assignments", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound},
	}, authz.Guard{Resource: "role", Verb: "view"}, func(ctx context.Context, in *roleInput) (*respx.Body[[]APIRoleDirectoryBinding], error) {
		tenantID, roleID, err := roleRef(in)
		if err != nil {
			return nil, err
		}
		bindings, err := svc.ListDirectoryBindings(ctx, tenantID, roleID)
		if err != nil {
			return nil, apiError("list role directory bindings", err)
		}
		return respx.OK(ctx, directoryBindingsFromDomain(bindings)), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-add-role-directory-binding", Method: http.MethodPut, Path: "/iam/roles/{role_id}/directory-bindings/{assignee_type}/{assignee_id}", Summary: "Assign a group or position to a role", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity},
	}, authz.Guard{Resource: "role", Verb: "manage_assignee"}, func(ctx context.Context, in *directoryBindingInput) (*respx.Body[MutationResult], error) {
		tenantID, roleID, err := roleRefIDs(in.TenantID, in.RoleID)
		if err != nil {
			return nil, err
		}
		assigneeID, err := requireID(in.AssigneeID)
		if err != nil {
			return nil, err
		}
		changed, err := svc.AddDirectoryBinding(ctx, tenantID, roleID, in.AssigneeType, assigneeID)
		if err != nil {
			return nil, apiError("add role directory binding", err)
		}
		return respx.OK(ctx, MutationResult{Changed: changed, SyncStatus: "applied"}), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-remove-role-directory-binding", Method: http.MethodDelete, Path: "/iam/roles/{role_id}/directory-bindings/{assignee_type}/{assignee_id}", Summary: "Remove a group or position from a role", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity},
	}, authz.Guard{Resource: "role", Verb: "manage_assignee"}, func(ctx context.Context, in *directoryBindingInput) (*respx.Body[MutationResult], error) {
		tenantID, roleID, err := roleRefIDs(in.TenantID, in.RoleID)
		if err != nil {
			return nil, err
		}
		assigneeID, err := requireID(in.AssigneeID)
		if err != nil {
			return nil, err
		}
		changed, err := svc.RemoveDirectoryBinding(ctx, tenantID, roleID, in.AssigneeType, assigneeID)
		if err != nil {
			return nil, apiError("remove role directory binding", err)
		}
		return respx.OK(ctx, MutationResult{Changed: changed, SyncStatus: "applied"}), nil
	})

	authz.Register(registrar, a, huma.Operation{OperationID: "iam-list-tenant-members", Method: http.MethodGet, Path: "/iam/members", Summary: "List tenant memberships", Tags: []string{"iam"}}, authz.Guard{Resource: "user", Verb: "view"}, func(ctx context.Context, in *listTenantMembersInput) (*respx.Body[[]APITenantMember], error) {
		tenantID, err := requireID(in.TenantID)
		if err != nil {
			return nil, err
		}
		members, total, err := svc.ListTenantMembers(ctx, tenantID, iamdomain.MemberFilter{Search: in.Search, Status: in.Status, Offset: in.Offset, Limit: in.Limit})
		if err != nil {
			return nil, apiError("list tenant members", err)
		}
		return respx.List(ctx, membersFromDomain(members), respx.Page{Offset: in.Offset, Limit: in.Limit, Count: len(members), Total: total}), nil
	})
	authz.Register(registrar, a, huma.Operation{OperationID: "iam-create-tenant-member", Method: http.MethodPost, Path: "/iam/members", Summary: "Bind an existing principal to a tenant", Tags: []string{"iam"}, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusConflict}}, authz.Guard{Resource: "user", Verb: "create"}, func(ctx context.Context, in *createTenantMemberInput) (*respx.Body[APITenantMember], error) {
		tenantID, err := requireID(in.TenantID)
		if err != nil {
			return nil, err
		}
		subject, err := requireID(in.Body.Subject)
		if err != nil {
			return nil, err
		}
		departmentID, err := optionalID(in.Body.DepartmentID)
		if err != nil {
			return nil, err
		}
		member, err := svc.PutTenantMember(ctx, tenantID, subject, in.Body.DisplayName, in.Body.Email, departmentID, in.Body.Status)
		if err != nil {
			return nil, apiError("create tenant member", err)
		}
		return respx.OK(ctx, memberFromDomain(member)), nil
	})
	authz.Register(registrar, a, huma.Operation{OperationID: "iam-get-tenant-member", Method: http.MethodGet, Path: "/iam/members/{subject}", Summary: "Get a tenant membership", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound}}, authz.Guard{Resource: "user", Verb: "view"}, func(ctx context.Context, in *tenantMemberInput) (*respx.Body[APITenantMember], error) {
		tenantID, subject, err := tenantMemberRef(in)
		if err != nil {
			return nil, err
		}
		member, err := svc.GetTenantMember(ctx, tenantID, subject)
		if err != nil {
			return nil, apiError("get tenant member", err)
		}
		roles, err := svc.ListTenantMemberRoles(ctx, tenantID, subject)
		if err != nil {
			return nil, apiError("list tenant member roles", err)
		}
		out := memberFromDomain(member)
		out.RoleIDs = guidStrings(roles)
		return respx.OK(ctx, out), nil
	})
	authz.Register(registrar, a, huma.Operation{OperationID: "iam-update-tenant-member", Method: http.MethodPatch, Path: "/iam/members/{subject}", Summary: "Update or disable a tenant membership", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound, http.StatusConflict}}, authz.Guard{Resource: "user", Verb: "update"}, func(ctx context.Context, in *updateTenantMemberInput) (*respx.Body[APITenantMember], error) {
		tenantID, err := requireID(in.TenantID)
		if err != nil {
			return nil, err
		}
		subject, err := requireID(in.Subject)
		if err != nil {
			return nil, err
		}
		member, err := svc.GetTenantMember(ctx, tenantID, subject)
		if err != nil {
			return nil, apiError("get tenant member", err)
		}
		if in.Body.DisplayName != nil {
			member.DisplayName = *in.Body.DisplayName
		}
		if in.Body.Email != nil {
			member.Email = *in.Body.Email
		}
		if in.Body.DepartmentID != nil {
			departmentID, err := optionalID(*in.Body.DepartmentID)
			if err != nil {
				return nil, err
			}
			member.DepartmentID = departmentID
		}
		if in.Body.Status != nil {
			member.Status = *in.Body.Status
		}
		member, err = svc.PutTenantMember(ctx, tenantID, subject, member.DisplayName, member.Email, member.DepartmentID, member.Status)
		if err != nil {
			return nil, apiError("update tenant member", err)
		}
		return respx.OK(ctx, memberFromDomain(member)), nil
	})
	authz.Register(registrar, a, huma.Operation{OperationID: "iam-list-tenant-member-roles", Method: http.MethodGet, Path: "/iam/members/{subject}/roles", Summary: "List role assignments for a tenant member", Tags: []string{"iam"}}, authz.Guard{Resource: "role", Verb: "view"}, func(ctx context.Context, in *tenantMemberInput) (*respx.Body[[]string], error) {
		tenantID, subject, err := tenantMemberRef(in)
		if err != nil {
			return nil, err
		}
		roles, err := svc.ListTenantMemberRoles(ctx, tenantID, subject)
		if err != nil {
			return nil, apiError("list tenant member roles", err)
		}
		return respx.OK(ctx, guidStrings(roles)), nil
	})

	authz.Register(registrar, a, huma.Operation{OperationID: "iam-list-menus", Method: http.MethodGet, Path: "/iam/menus", Summary: "List tenant menu metadata", Tags: []string{"iam"}}, authz.Guard{Resource: "menu", Verb: "view"}, func(ctx context.Context, in *tenantInput) (*respx.Body[[]APIMenu], error) {
		tenantID, err := requireID(in.TenantID)
		if err != nil {
			return nil, err
		}
		menus, err := svc.ListMenus(ctx, tenantID)
		if err != nil {
			return nil, apiError("list menus", err)
		}
		return respx.OK(ctx, menusFromDomain(menus)), nil
	})
	authz.Register(registrar, a, huma.Operation{OperationID: "iam-create-menu", Method: http.MethodPost, Path: "/iam/menus", Summary: "Create a tenant menu", Tags: []string{"iam"}, DefaultStatus: http.StatusCreated}, authz.Guard{Resource: "menu", Verb: "create"}, func(ctx context.Context, in *createMenuInput) (*respx.Body[APIMenu], error) {
		tenantID, err := requireID(in.TenantID)
		if err != nil {
			return nil, err
		}
		parentID, err := optionalID(in.Body.ParentID)
		if err != nil {
			return nil, err
		}
		menu, err := svc.CreateMenu(ctx, iamdomain.Menu{TenantID: tenantID, ParentID: parentID, Label: in.Body.Label, Route: in.Body.Route, Icon: in.Body.Icon, SortOrder: in.Body.SortOrder, PermissionCode: in.Body.PermissionCode, Status: in.Body.Status})
		if err != nil {
			return nil, apiError("create menu", err)
		}
		return respx.OK(ctx, menuFromDomain(menu)), nil
	})
	authz.Register(registrar, a, huma.Operation{OperationID: "iam-get-menu", Method: http.MethodGet, Path: "/iam/menus/{menu_id}", Summary: "Get a tenant menu", Tags: []string{"iam"}}, authz.Guard{Resource: "menu", Verb: "view"}, func(ctx context.Context, in *menuInput) (*respx.Body[APIMenu], error) {
		tenantID, menuID, err := menuRef(in)
		if err != nil {
			return nil, err
		}
		menu, err := svc.GetMenu(ctx, tenantID, menuID)
		if err != nil {
			return nil, apiError("get menu", err)
		}
		return respx.OK(ctx, menuFromDomain(menu)), nil
	})
	authz.Register(registrar, a, huma.Operation{OperationID: "iam-update-menu", Method: http.MethodPatch, Path: "/iam/menus/{menu_id}", Summary: "Update a tenant menu", Tags: []string{"iam"}}, authz.Guard{Resource: "menu", Verb: "update"}, func(ctx context.Context, in *updateMenuInput) (*respx.Body[APIMenu], error) {
		tenantID, err := requireID(in.TenantID)
		if err != nil {
			return nil, err
		}
		menuID, err := requireID(in.MenuID)
		if err != nil {
			return nil, err
		}
		menu, err := svc.GetMenu(ctx, tenantID, menuID)
		if err != nil {
			return nil, apiError("get menu", err)
		}
		if in.Body.ParentID != nil {
			parentID, err := optionalID(*in.Body.ParentID)
			if err != nil {
				return nil, err
			}
			menu.ParentID = parentID
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
		tenantID, menuID, err := menuRef(in)
		if err != nil {
			return nil, err
		}
		if err := svc.DeleteMenu(ctx, tenantID, menuID); err != nil {
			return nil, apiError("delete menu", err)
		}
		return respx.OK(ctx, MutationResult{Changed: true, SyncStatus: "not_applicable"}), nil
	})
	authz.RegisterTenantMember(registrar, a, huma.Operation{OperationID: "iam-effective-menus", Method: http.MethodGet, Path: "/iam/me/menus", Summary: "Return the current member's authorized menu tree", Tags: []string{"iam"}}, func(ctx context.Context, in *tenantInput) (*respx.Body[[]MenuItem], error) {
		claims, ok := authnext.FromContext(ctx)
		if !ok || claims == nil || claims.PrincipalID.Zero() {
			return nil, huma.Error401Unauthorized("unauthorized")
		}
		tenantID, err := requireID(in.TenantID)
		if err != nil {
			return nil, err
		}
		menus, err := svc.EffectiveMenus(ctx, tenantID, claims.PrincipalID)
		if err != nil {
			return nil, apiError("effective menus", err)
		}
		return paginateList(ctx, menus, in.Offset, in.Limit), nil
	})
}

func roleFromDomain(role iamdomain.Role) APIRole {
	return APIRole{ID: role.ID.String(), TenantID: role.TenantID.String(), Name: role.Name, Description: role.Description, CreatedAt: role.CreatedAt, UpdatedAt: role.UpdatedAt}
}

func rolesFromDomain(roles []iamdomain.Role) []APIRole {
	out := make([]APIRole, 0, len(roles))
	for _, role := range roles {
		out = append(out, roleFromDomain(role))
	}
	return out
}

func rolePermissionGrantFromDomain(grant iamdomain.RolePermissionGrant) APIRolePermissionGrant {
	return APIRolePermissionGrant{PermissionCode: grant.PermissionCode, Condition: policyCondition(grant.Condition), CreatedAt: grant.CreatedAt}
}

func rolePermissionGrantsFromDomain(grants []iamdomain.RolePermissionGrant) []APIRolePermissionGrant {
	out := make([]APIRolePermissionGrant, 0, len(grants))
	for _, grant := range grants {
		out = append(out, rolePermissionGrantFromDomain(grant))
	}
	return out
}

func entityFromDomain(entity iamdomain.Entity) Entity {
	return Entity{
		ID: entity.ID.String(), TenantID: entity.TenantID.String(), ParentID: entity.ParentID.String(), Type: entity.Type, Name: entity.Name,
		Status: entity.Status, Metadata: entity.Metadata, CreatedAt: entity.CreatedAt, UpdatedAt: entity.UpdatedAt,
	}
}

func entitiesFromDomain(entities []iamdomain.Entity) []Entity {
	out := make([]Entity, 0, len(entities))
	for _, entity := range entities {
		out = append(out, entityFromDomain(entity))
	}
	return out
}

func entityBindingFromDomain(binding iamdomain.EntityRoleBinding) EntityRoleBinding {
	out := EntityRoleBinding{
		EntityID: binding.EntityID.String(), RoleID: binding.RoleID.String(), PrincipalID: binding.PrincipalID.String(),
		Effect: binding.Effect, CreatedAt: binding.CreatedAt,
	}
	if !binding.ExpiresAt.IsZero() {
		expiresAt := binding.ExpiresAt
		out.ExpiresAt = &expiresAt
	}
	return out
}

func entityBindingsFromDomain(bindings []iamdomain.EntityRoleBinding) []EntityRoleBinding {
	out := make([]EntityRoleBinding, 0, len(bindings))
	for _, binding := range bindings {
		out = append(out, entityBindingFromDomain(binding))
	}
	return out
}

func directoryBindingsFromDomain(bindings []iamdomain.RoleDirectoryBinding) []APIRoleDirectoryBinding {
	out := make([]APIRoleDirectoryBinding, 0, len(bindings))
	for _, binding := range bindings {
		out = append(out, APIRoleDirectoryBinding{RoleID: binding.RoleID.String(), AssigneeType: binding.AssigneeType, AssigneeID: binding.AssigneeID.String(), CreatedAt: binding.CreatedAt})
	}
	return out
}

func memberFromDomain(member iamdomain.TenantMember) APITenantMember {
	out := APITenantMember{TenantID: member.TenantID.String(), Subject: member.PrincipalID.String(), DisplayName: member.DisplayName, Email: member.Email, DepartmentID: member.DepartmentID.String(), Status: member.Status, CreatedAt: member.CreatedAt, UpdatedAt: member.UpdatedAt}
	if !member.DisabledAt.IsZero() {
		value := member.DisabledAt
		out.DisabledAt = &value
	}
	return out
}

func roleDataScopeFromDomain(scope iamdomain.RoleDataScope) APIRoleDataScope {
	out := APIRoleDataScope{RoleID: scope.RoleID.String(), Scope: scope.Scope, DepartmentIDs: guidStrings(scope.DepartmentIDs)}
	if !scope.UpdatedAt.IsZero() {
		updatedAt := scope.UpdatedAt
		out.UpdatedAt = &updatedAt
	}
	return out
}

func membersFromDomain(members []iamdomain.TenantMember) []APITenantMember {
	out := make([]APITenantMember, 0, len(members))
	for _, member := range members {
		out = append(out, memberFromDomain(member))
	}
	return out
}
func menuFromDomain(menu iamdomain.Menu) APIMenu {
	return APIMenu{ID: menu.ID.String(), TenantID: menu.TenantID.String(), ParentID: menu.ParentID.String(), Label: menu.Label, Route: menu.Route, Icon: menu.Icon, SortOrder: menu.SortOrder, PermissionCode: menu.PermissionCode, Status: menu.Status, CreatedAt: menu.CreatedAt, UpdatedAt: menu.UpdatedAt}
}
func menusFromDomain(menus []iamdomain.Menu) []APIMenu {
	out := make([]APIMenu, 0, len(menus))
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
	case errors.Is(err, iamdomain.ErrDirectoryAssigneeNotFound):
		return huma.Error404NotFound("directory_assignee_not_found")
	case errors.Is(err, iamdomain.ErrDirectoryAssigneeInactive):
		return huma.Error409Conflict("directory_assignee_inactive")
	case errors.Is(err, iamdomain.ErrLastTenantAdministrator):
		return huma.Error409Conflict("last_tenant_administrator")
	case errors.Is(err, iamdomain.ErrEntityNotFound):
		return huma.Error404NotFound("entity_not_found")
	case errors.Is(err, iamdomain.ErrEntityConflict):
		return huma.Error409Conflict("entity_conflict")
	case errors.Is(err, iamdomain.ErrEntityHasChildren):
		return huma.Error409Conflict("entity_has_children")
	case errors.Is(err, iamdomain.ErrEntityHasBindings):
		return huma.Error409Conflict("entity_has_role_bindings")
	case errors.Is(err, iamdomain.ErrEntityHasRelationships):
		return huma.Error409Conflict("entity_has_relationships")
	case errors.Is(err, iamdomain.ErrEntityHierarchy):
		return huma.Error409Conflict("entity_hierarchy_conflict")
	case errors.Is(err, iamdomain.ErrInvalidRelationship):
		return huma.Error422UnprocessableEntity("invalid_relationship")
	case errors.Is(err, iamdomain.ErrRelationshipSubjectMissing):
		return huma.Error404NotFound("relationship_subject_not_found")
	case errors.Is(err, iamdomain.ErrRelationshipSubjectInactive):
		return huma.Error409Conflict("relationship_subject_inactive")
	case errors.Is(err, iamdomain.ErrRelationshipResourceInactive):
		return huma.Error409Conflict("relationship_resource_inactive")
	case errors.Is(err, iamdomain.ErrRelationshipHierarchy):
		return huma.Error409Conflict("relationship_hierarchy_conflict")
	case errors.Is(err, iamdomain.ErrInvalidRelationshipWindow):
		return huma.Error422UnprocessableEntity("invalid_relationship_window")
	case errors.Is(err, iamdomain.ErrInvalidRelationshipCondition):
		return huma.Error422UnprocessableEntity("invalid_relationship_condition")
	case errors.Is(err, iamdomain.ErrInvalidResourceAuthorization):
		return huma.Error422UnprocessableEntity("invalid_resource_authorization")
	case errors.Is(err, iamdomain.ErrAuthorizationChanged):
		return huma.Error503ServiceUnavailable("authorization_policy_changed")
	case errors.Is(err, iamdomain.ErrInvalidRoleDataScope):
		return huma.Error422UnprocessableEntity("invalid_role_data_scope")
	case errors.Is(err, iamdomain.ErrRoleScopeDepartmentNeeded):
		return huma.Error422UnprocessableEntity("role_data_scope_department_required")
	case errors.Is(err, iamdomain.ErrRoleScopeDepartmentsExtra):
		return huma.Error422UnprocessableEntity("role_data_scope_departments_not_allowed")
	case errors.Is(err, iamdomain.ErrRoleScopeDepartmentMissing):
		return huma.Error404NotFound("role_scope_department_not_found")
	case errors.Is(err, iamdomain.ErrRoleScopeDepartmentInactive):
		return huma.Error409Conflict("role_scope_department_inactive")
	case errors.Is(err, iamdomain.ErrMemberDepartmentMissing):
		return huma.Error404NotFound("member_department_not_found")
	case errors.Is(err, iamdomain.ErrMemberDepartmentInactive):
		return huma.Error409Conflict("member_department_inactive")
	case errors.Is(err, iamdomain.ErrPermissionNotFound):
		return huma.Error422UnprocessableEntity("iam_permission_not_found")
	case errors.Is(err, iamdomain.ErrRolePermissionNotGranted):
		return huma.Error404NotFound("role_permission_not_granted")
	case errors.Is(err, iamdomain.ErrInvalidRolePermissionCondition):
		return huma.Error422UnprocessableEntity("invalid_role_permission_condition")
	case errors.Is(err, iamdomain.ErrPrivilegeEscalation):
		return huma.Error403Forbidden("privilege_escalation_denied")
	case errors.Is(err, iamdomain.ErrInvalidArgument):
		return huma.Error422UnprocessableEntity("invalid_iam_request")
	default:
		slog.Error("iam request failed", "operation", operation, "err", err)
		return huma.Error500InternalServerError("internal_server_error")
	}
}

// paginateList applies offset/limit to a list result, defaulting to all
// records when both are zero. This prevents unbounded response sizes
// on collections that can grow large (roles, entities, menus).
func paginateList[T any](ctx context.Context, items []T, offset, limit int) *respx.Body[[]T] {
	if offset < 0 {
		offset = 0
	}
	if limit < 0 || limit > 200 {
		limit = 0
	}
	total := len(items)
	if offset >= total {
		return respx.List(ctx, []T{}, respx.Page{Offset: offset, Limit: limit, Total: int64(total)})
	}
	if limit > 0 {
		end := offset + limit
		if end > total {
			end = total
		}
		items = items[offset:end]
	} else if offset > 0 {
		items = items[offset:]
	}
	return respx.List(ctx, items, respx.Page{Offset: offset, Limit: limit, Total: int64(total)})
}
