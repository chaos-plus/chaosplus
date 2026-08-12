package organization

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
	iamdomain "github.com/chaos-plus/chaosplus/internal/modules/iam/domain"
	"github.com/danielgtaylor/huma/v2"
)

type groupListInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
}

type groupIDInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	ID       string `path:"id" maxLength:"128"`
}

type createGroupInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	Body     struct {
		Name        string         `json:"name" minLength:"1" maxLength:"128"`
		Type        string         `json:"type,omitempty" enum:"static,dynamic" default:"static"`
		Rule        MembershipRule `json:"membership_rule,omitempty"`
		Description string         `json:"description,omitempty" maxLength:"1024"`
		Status      string         `json:"status,omitempty" enum:"active,disabled" default:"active"`
		SortOrder   int            `json:"sort_order,omitempty" minimum:"0" maximum:"1000000"`
	}
}

type updateGroupInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	ID       string `path:"id" maxLength:"128"`
	Body     struct {
		Name        *string         `json:"name,omitempty" minLength:"1" maxLength:"128"`
		Description *string         `json:"description,omitempty" maxLength:"1024"`
		Status      *string         `json:"status,omitempty" enum:"active,disabled"`
		SortOrder   *int            `json:"sort_order,omitempty" minimum:"0" maximum:"1000000"`
		Rule        *MembershipRule `json:"membership_rule,omitempty"`
		Version     int64           `json:"version" minimum:"1"`
	}
}

type deleteGroupInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	ID       string `path:"id" maxLength:"128"`
	Version  int64  `query:"version" minimum:"1"`
}

type groupMemberInput struct {
	TenantID    string `header:"X-Tenant-Id" maxLength:"128"`
	ID          string `path:"id" maxLength:"128"`
	PrincipalID string `path:"principal_id" maxLength:"255"`
}

type putGroupMemberInput struct {
	TenantID    string `header:"X-Tenant-Id" maxLength:"128"`
	ID          string `path:"id" maxLength:"128"`
	PrincipalID string `path:"principal_id" maxLength:"255"`
	Body        struct {
		StartsAt *time.Time `json:"starts_at,omitempty"`
		EndsAt   *time.Time `json:"ends_at,omitempty"`
	}
}

type deletedGroup struct {
	Deleted bool `json:"deleted"`
}

func (MembershipRule) Schema(huma.Registry) *huma.Schema {
	minItems, maxConditions, maxValues, maxLength := 1, 16, 16, 320
	version := float64(1)
	condition := &huma.Schema{
		Type: "object", AdditionalProperties: false,
		Required: []string{"field", "operator", "values"},
		Properties: map[string]*huma.Schema{
			"field":    {Type: "string", Enum: []any{"member.subject", "member.email", "member.email_domain", "member.department_id", "member.status"}},
			"operator": {Type: "string", Enum: []any{"in", "not_in"}},
			"values":   {Type: "array", Items: &huma.Schema{Type: "string", MaxLength: &maxLength}, MinItems: &minItems, MaxItems: &maxValues},
		},
	}
	return &huma.Schema{
		Type: "object", Description: "Version 1 dynamic membership rule evaluated against stable tenant-member attributes", AdditionalProperties: false,
		Required: []string{"version", "match", "conditions"},
		Properties: map[string]*huma.Schema{
			"version":    {Type: "integer", Minimum: &version, Maximum: &version},
			"match":      {Type: "string", Enum: []any{"all", "any"}},
			"conditions": {Type: "array", Items: condition, MinItems: &minItems, MaxItems: &maxConditions},
		},
	}
}

func RegisterGroupREST(api huma.API, service *GroupService, registrar *authz.Registrar) {
	authz.Register(registrar, api, huma.Operation{OperationID: "organization-list-groups", Method: http.MethodGet, Path: "/iam/groups", Summary: "List tenant groups", Tags: []string{"organization"}, Errors: []int{http.StatusUnprocessableEntity}}, authz.Guard{Resource: "group", Verb: "view"}, func(ctx context.Context, in *groupListInput) (*respx.Body[[]Group], error) {
		tenantID, err := parseOrganizationID(in.TenantID)
		if err != nil {
			return nil, err
		}
		items, err := service.List(ctx, tenantID)
		if err != nil {
			return nil, groupError(err)
		}
		return respx.OK(ctx, items), nil
	})

	authz.Register(registrar, api, huma.Operation{OperationID: "organization-create-group", Method: http.MethodPost, Path: "/iam/groups", Summary: "Create a tenant group", Tags: []string{"organization"}, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusConflict, http.StatusUnprocessableEntity}}, authz.Guard{Resource: "group", Verb: "create"}, func(ctx context.Context, in *createGroupInput) (*respx.Body[Group], error) {
		tenantID, err := parseOrganizationID(in.TenantID)
		if err != nil {
			return nil, err
		}
		item, err := service.Create(ctx, tenantID, CreateGroup{Name: in.Body.Name, Type: in.Body.Type, Rule: in.Body.Rule, Description: in.Body.Description, Status: in.Body.Status, SortOrder: in.Body.SortOrder})
		if err != nil {
			return nil, groupError(err)
		}
		return respx.OK(ctx, item), nil
	})

	authz.Register(registrar, api, huma.Operation{OperationID: "organization-get-group", Method: http.MethodGet, Path: "/iam/groups/{id}", Summary: "Get a tenant group", Tags: []string{"organization"}, Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity}}, authz.Guard{Resource: "group", Verb: "view"}, func(ctx context.Context, in *groupIDInput) (*respx.Body[Group], error) {
		tenantID, err := parseOrganizationID(in.TenantID)
		if err != nil {
			return nil, err
		}
		id, err := parseOrganizationID(in.ID)
		if err != nil {
			return nil, err
		}
		item, err := service.Get(ctx, tenantID, id)
		if err != nil {
			return nil, groupError(err)
		}
		return respx.OK(ctx, item), nil
	})

	authz.Register(registrar, api, huma.Operation{OperationID: "organization-update-group", Method: http.MethodPatch, Path: "/iam/groups/{id}", Summary: "Update a tenant group", Tags: []string{"organization"}, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity}}, authz.Guard{Resource: "group", Verb: "update"}, func(ctx context.Context, in *updateGroupInput) (*respx.Body[Group], error) {
		tenantID, err := parseOrganizationID(in.TenantID)
		if err != nil {
			return nil, err
		}
		id, err := parseOrganizationID(in.ID)
		if err != nil {
			return nil, err
		}
		item, err := service.Update(ctx, tenantID, id, UpdateGroup{Name: in.Body.Name, Description: in.Body.Description, Status: in.Body.Status, SortOrder: in.Body.SortOrder, Rule: in.Body.Rule, Version: in.Body.Version})
		if err != nil {
			return nil, groupError(err)
		}
		return respx.OK(ctx, item), nil
	})

	authz.Register(registrar, api, huma.Operation{OperationID: "organization-delete-group", Method: http.MethodDelete, Path: "/iam/groups/{id}", Summary: "Delete an empty tenant group", Tags: []string{"organization"}, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity}}, authz.Guard{Resource: "group", Verb: "delete"}, func(ctx context.Context, in *deleteGroupInput) (*respx.Body[deletedGroup], error) {
		tenantID, err := parseOrganizationID(in.TenantID)
		if err != nil {
			return nil, err
		}
		id, err := parseOrganizationID(in.ID)
		if err != nil {
			return nil, err
		}
		if err := service.Delete(ctx, tenantID, id, in.Version); err != nil {
			return nil, groupError(err)
		}
		return respx.OK(ctx, deletedGroup{Deleted: true}), nil
	})

	authz.Register(registrar, api, huma.Operation{OperationID: "organization-list-group-members", Method: http.MethodGet, Path: "/iam/groups/{id}/members", Summary: "List group members", Tags: []string{"organization"}, Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity}}, authz.Guard{Resource: "group", Verb: "view"}, func(ctx context.Context, in *groupIDInput) (*respx.Body[[]GroupMember], error) {
		tenantID, err := parseOrganizationID(in.TenantID)
		if err != nil {
			return nil, err
		}
		id, err := parseOrganizationID(in.ID)
		if err != nil {
			return nil, err
		}
		items, err := service.ListMembers(ctx, tenantID, id)
		if err != nil {
			return nil, groupError(err)
		}
		return respx.OK(ctx, items), nil
	})

	authz.Register(registrar, api, huma.Operation{OperationID: "organization-put-group-member", Method: http.MethodPut, Path: "/iam/groups/{id}/members/{principal_id}", Summary: "Assign or update a group member", Tags: []string{"organization"}, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity}}, authz.Guard{Resource: "group", Verb: "manage_member"}, func(ctx context.Context, in *putGroupMemberInput) (*respx.Body[GroupMember], error) {
		tenantID, err := parseOrganizationID(in.TenantID)
		if err != nil {
			return nil, err
		}
		id, err := parseOrganizationID(in.ID)
		if err != nil {
			return nil, err
		}
		principalID, err := parseOrganizationID(in.PrincipalID)
		if err != nil {
			return nil, err
		}
		item, err := service.PutMember(ctx, tenantID, id, principalID, GroupMemberWindow{StartsAt: in.Body.StartsAt, EndsAt: in.Body.EndsAt})
		if err != nil {
			return nil, groupError(err)
		}
		return respx.OK(ctx, item), nil
	})

	authz.Register(registrar, api, huma.Operation{OperationID: "organization-delete-group-member", Method: http.MethodDelete, Path: "/iam/groups/{id}/members/{principal_id}", Summary: "Remove a group member", Tags: []string{"organization"}, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity}}, authz.Guard{Resource: "group", Verb: "manage_member"}, func(ctx context.Context, in *groupMemberInput) (*respx.Body[deletedGroup], error) {
		tenantID, err := parseOrganizationID(in.TenantID)
		if err != nil {
			return nil, err
		}
		id, err := parseOrganizationID(in.ID)
		if err != nil {
			return nil, err
		}
		principalID, err := parseOrganizationID(in.PrincipalID)
		if err != nil {
			return nil, err
		}
		deleted, err := service.DeleteMember(ctx, tenantID, id, principalID)
		if err != nil {
			return nil, groupError(err)
		}
		return respx.OK(ctx, deletedGroup{Deleted: deleted}), nil
	})
}

func groupError(err error) error {
	switch {
	case errors.Is(err, ErrGroupNotFound):
		return huma.Error404NotFound("group_not_found")
	case errors.Is(err, ErrGroupNameConflict):
		return huma.Error409Conflict("group_name_exists")
	case errors.Is(err, ErrGroupVersionConflict):
		return huma.Error409Conflict("group_version_conflict")
	case errors.Is(err, ErrGroupHasMembers):
		return huma.Error409Conflict("group_has_members")
	case errors.Is(err, ErrGroupRoleBound):
		return huma.Error409Conflict("group_role_bound")
	case errors.Is(err, ErrGroupRelationshipBound):
		return huma.Error409Conflict("group_relationship_bound")
	case errors.Is(err, ErrGroupMemberInactive):
		return huma.Error409Conflict("group_member_inactive")
	case errors.Is(err, ErrDynamicGroupMembers):
		return huma.Error409Conflict("dynamic_group_members_computed")
	case errors.Is(err, ErrGroupRuleType):
		return huma.Error422UnprocessableEntity("group_rule_requires_dynamic_type")
	case errors.Is(err, ErrGroupRuleInvalid):
		return huma.Error422UnprocessableEntity("invalid_group_membership_rule")
	case errors.Is(err, iamdomain.ErrLastTenantAdministrator):
		return huma.Error409Conflict("last_tenant_administrator")
	case errors.Is(err, ErrGroupInvalid):
		return huma.Error422UnprocessableEntity("invalid_group")
	default:
		return huma.Error500InternalServerError("organization_unavailable")
	}
}
