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

type positionListInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
}

type positionIDInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	ID       string `path:"id" maxLength:"128"`
}

type createPositionInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	Body     struct {
		Code      string `json:"code" minLength:"1" maxLength:"64" pattern:"^[A-Za-z][A-Za-z0-9._-]*$"`
		Name      string `json:"name" minLength:"1" maxLength:"128"`
		Status    string `json:"status,omitempty" enum:"active,disabled" default:"active"`
		SortOrder int    `json:"sort_order,omitempty" minimum:"0" maximum:"1000000"`
	}
}

type updatePositionInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	ID       string `path:"id" maxLength:"128"`
	Body     struct {
		Code      *string `json:"code,omitempty" minLength:"1" maxLength:"64" pattern:"^[A-Za-z][A-Za-z0-9._-]*$"`
		Name      *string `json:"name,omitempty" minLength:"1" maxLength:"128"`
		Status    *string `json:"status,omitempty" enum:"active,disabled"`
		SortOrder *int    `json:"sort_order,omitempty" minimum:"0" maximum:"1000000"`
		Version   int64   `json:"version" minimum:"1"`
	}
}

type deletePositionInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	ID       string `path:"id" maxLength:"128"`
	Version  int64  `query:"version" minimum:"1"`
}

type positionMemberInput struct {
	TenantID    string `header:"X-Tenant-Id" maxLength:"128"`
	ID          string `path:"id" maxLength:"128"`
	PrincipalID string `path:"principal_id" maxLength:"255"`
}

type putPositionMemberInput struct {
	TenantID    string `header:"X-Tenant-Id" maxLength:"128"`
	ID          string `path:"id" maxLength:"128"`
	PrincipalID string `path:"principal_id" maxLength:"255"`
	Body        struct {
		StartsAt *time.Time `json:"starts_at,omitempty"`
		EndsAt   *time.Time `json:"ends_at,omitempty"`
	}
}

type deletedPosition struct {
	Deleted bool `json:"deleted"`
}

func RegisterPositionREST(api huma.API, service *PositionService, registrar *authz.Registrar) {
	authz.Register(registrar, api, huma.Operation{OperationID: "organization-list-positions", Method: http.MethodGet, Path: "/iam/positions", Summary: "List tenant positions", Tags: []string{"organization"}, Errors: []int{http.StatusUnprocessableEntity}}, authz.Guard{Resource: "position", Verb: "view"}, func(ctx context.Context, in *positionListInput) (*respx.Body[[]Position], error) {
		items, err := service.List(ctx, in.TenantID)
		if err != nil {
			return nil, positionError(err)
		}
		return respx.OK(ctx, items), nil
	})

	authz.Register(registrar, api, huma.Operation{OperationID: "organization-create-position", Method: http.MethodPost, Path: "/iam/positions", Summary: "Create a tenant position", Tags: []string{"organization"}, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusConflict, http.StatusUnprocessableEntity}}, authz.Guard{Resource: "position", Verb: "create"}, func(ctx context.Context, in *createPositionInput) (*respx.Body[Position], error) {
		item, err := service.Create(ctx, in.TenantID, CreatePosition{Code: in.Body.Code, Name: in.Body.Name, Status: in.Body.Status, SortOrder: in.Body.SortOrder})
		if err != nil {
			return nil, positionError(err)
		}
		return respx.OK(ctx, item), nil
	})

	authz.Register(registrar, api, huma.Operation{OperationID: "organization-get-position", Method: http.MethodGet, Path: "/iam/positions/{id}", Summary: "Get a tenant position", Tags: []string{"organization"}, Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity}}, authz.Guard{Resource: "position", Verb: "view"}, func(ctx context.Context, in *positionIDInput) (*respx.Body[Position], error) {
		item, err := service.Get(ctx, in.TenantID, in.ID)
		if err != nil {
			return nil, positionError(err)
		}
		return respx.OK(ctx, item), nil
	})

	authz.Register(registrar, api, huma.Operation{OperationID: "organization-update-position", Method: http.MethodPatch, Path: "/iam/positions/{id}", Summary: "Update a tenant position", Tags: []string{"organization"}, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity}}, authz.Guard{Resource: "position", Verb: "update"}, func(ctx context.Context, in *updatePositionInput) (*respx.Body[Position], error) {
		item, err := service.Update(ctx, in.TenantID, in.ID, UpdatePosition{Code: in.Body.Code, Name: in.Body.Name, Status: in.Body.Status, SortOrder: in.Body.SortOrder, Version: in.Body.Version})
		if err != nil {
			return nil, positionError(err)
		}
		return respx.OK(ctx, item), nil
	})

	authz.Register(registrar, api, huma.Operation{OperationID: "organization-delete-position", Method: http.MethodDelete, Path: "/iam/positions/{id}", Summary: "Delete an empty tenant position", Tags: []string{"organization"}, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity}}, authz.Guard{Resource: "position", Verb: "delete"}, func(ctx context.Context, in *deletePositionInput) (*respx.Body[deletedPosition], error) {
		if err := service.Delete(ctx, in.TenantID, in.ID, in.Version); err != nil {
			return nil, positionError(err)
		}
		return respx.OK(ctx, deletedPosition{Deleted: true}), nil
	})

	authz.Register(registrar, api, huma.Operation{OperationID: "organization-list-position-members", Method: http.MethodGet, Path: "/iam/positions/{id}/members", Summary: "List position members", Tags: []string{"organization"}, Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity}}, authz.Guard{Resource: "position", Verb: "view"}, func(ctx context.Context, in *positionIDInput) (*respx.Body[[]PositionMember], error) {
		items, err := service.ListMembers(ctx, in.TenantID, in.ID)
		if err != nil {
			return nil, positionError(err)
		}
		return respx.OK(ctx, items), nil
	})

	authz.Register(registrar, api, huma.Operation{OperationID: "organization-put-position-member", Method: http.MethodPut, Path: "/iam/positions/{id}/members/{principal_id}", Summary: "Assign or update a position member", Tags: []string{"organization"}, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity}}, authz.Guard{Resource: "position", Verb: "manage_member"}, func(ctx context.Context, in *putPositionMemberInput) (*respx.Body[PositionMember], error) {
		item, err := service.PutMember(ctx, in.TenantID, in.ID, in.PrincipalID, PositionMemberWindow{StartsAt: in.Body.StartsAt, EndsAt: in.Body.EndsAt})
		if err != nil {
			return nil, positionError(err)
		}
		return respx.OK(ctx, item), nil
	})

	authz.Register(registrar, api, huma.Operation{OperationID: "organization-delete-position-member", Method: http.MethodDelete, Path: "/iam/positions/{id}/members/{principal_id}", Summary: "Remove a position member", Tags: []string{"organization"}, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity}}, authz.Guard{Resource: "position", Verb: "manage_member"}, func(ctx context.Context, in *positionMemberInput) (*respx.Body[deletedPosition], error) {
		deleted, err := service.DeleteMember(ctx, in.TenantID, in.ID, in.PrincipalID)
		if err != nil {
			return nil, positionError(err)
		}
		return respx.OK(ctx, deletedPosition{Deleted: deleted}), nil
	})
}

func positionError(err error) error {
	switch {
	case errors.Is(err, ErrPositionNotFound):
		return huma.Error404NotFound("position_not_found")
	case errors.Is(err, ErrPositionCodeConflict):
		return huma.Error409Conflict("position_code_exists")
	case errors.Is(err, ErrPositionVersionConflict):
		return huma.Error409Conflict("position_version_conflict")
	case errors.Is(err, ErrPositionHasMembers):
		return huma.Error409Conflict("position_has_members")
	case errors.Is(err, ErrPositionRoleBound):
		return huma.Error409Conflict("position_role_bound")
	case errors.Is(err, ErrPositionRelationshipBound):
		return huma.Error409Conflict("position_relationship_bound")
	case errors.Is(err, ErrPositionMemberInactive):
		return huma.Error409Conflict("position_member_inactive")
	case errors.Is(err, iamdomain.ErrLastTenantAdministrator):
		return huma.Error409Conflict("last_tenant_administrator")
	case errors.Is(err, ErrPositionInvalid):
		return huma.Error422UnprocessableEntity("invalid_position")
	default:
		return huma.Error500InternalServerError("organization_unavailable")
	}
}
