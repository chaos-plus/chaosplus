package identity

import (
	"context"
	"errors"
	"net/http"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
	iamdomain "github.com/chaos-plus/chaosplus/internal/modules/iam/domain"
	"github.com/danielgtaylor/huma/v2"
)

type listInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	Search   string `query:"search"`
	Limit    int    `query:"limit" default:"50" minimum:"1" maximum:"200"`
	Offset   int    `query:"offset" default:"0" minimum:"0"`
}
type listData struct {
	Items []Principal `json:"items"`
	Total int64       `json:"total"`
}
type idInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	ID       string `path:"id"`
}
type createInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	Body     struct {
		LoginName   string `json:"login_name" minLength:"1" maxLength:"200"`
		Password    string `json:"password" minLength:"12" maxLength:"1024"`
		DisplayName string `json:"display_name,omitempty" maxLength:"128"`
		Email       string `json:"email,omitempty" maxLength:"320"`
	}
}

type updateInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	ID       string `path:"id"`
	Body     struct {
		DisplayName *string `json:"display_name,omitempty" minLength:"1" maxLength:"128"`
		Email       *string `json:"email,omitempty" maxLength:"320"`
	}
}

func RegisterREST(api huma.API, service *Service, registrar *authz.Registrar) {
	RegisterServiceAccountREST(api, service, registrar)
	authz.Register(registrar, api, huma.Operation{OperationID: "identity-list-principals", Method: http.MethodGet, Path: "/iam/principals", Summary: "List local principals in the tenant", Tags: []string{"identity"}}, authz.Guard{Resource: "user", Verb: "view"}, func(ctx context.Context, in *listInput) (*respx.Body[listData], error) {
		items, total, err := service.List(ctx, in.TenantID, in.Search, in.Limit, in.Offset)
		if err != nil {
			return nil, identityError(err)
		}
		return respx.OK(ctx, listData{Items: items, Total: total}), nil
	})
	authz.Register(registrar, api, huma.Operation{OperationID: "identity-create-principal", Method: http.MethodPost, Path: "/iam/principals", Summary: "Create a local principal and tenant membership", Tags: []string{"identity"}}, authz.Guard{Resource: "user", Verb: "create"}, func(ctx context.Context, in *createInput) (*respx.Body[Principal], error) {
		principal, err := service.Create(ctx, in.TenantID, in.Body.LoginName, in.Body.Password, in.Body.DisplayName, in.Body.Email)
		if err != nil {
			return nil, identityError(err)
		}
		return respx.OK(ctx, principal), nil
	})
	authz.Register(registrar, api, huma.Operation{OperationID: "identity-get-principal", Method: http.MethodGet, Path: "/iam/principals/{id}", Summary: "Get a local principal in the tenant", Tags: []string{"identity"}}, authz.Guard{Resource: "user", Verb: "view"}, func(ctx context.Context, in *idInput) (*respx.Body[Principal], error) {
		principal, err := service.Get(ctx, in.TenantID, in.ID)
		if err != nil {
			return nil, identityError(err)
		}
		return respx.OK(ctx, principal), nil
	})
	authz.Register(registrar, api, huma.Operation{OperationID: "identity-update-principal", Method: http.MethodPatch, Path: "/iam/principals/{id}", Summary: "Update a local principal", Tags: []string{"identity"}}, authz.Guard{Resource: "platform", Verb: "administer"}, func(ctx context.Context, in *updateInput) (*respx.Body[Principal], error) {
		principal, err := service.Update(ctx, in.TenantID, in.ID, in.Body.DisplayName, in.Body.Email)
		if err != nil {
			return nil, identityError(err)
		}
		return respx.OK(ctx, principal), nil
	})
	authz.Register(registrar, api, huma.Operation{OperationID: "identity-disable-principal", Method: http.MethodPost, Path: "/iam/principals/{id}/disable", Summary: "Disable a principal and revoke its tokens", Tags: []string{"identity"}, Errors: []int{http.StatusConflict}}, authz.Guard{Resource: "platform", Verb: "administer"}, func(ctx context.Context, in *idInput) (*respx.Body[Principal], error) {
		principal, err := service.SetStatus(ctx, in.TenantID, in.ID, "disabled")
		if err != nil {
			return nil, identityError(err)
		}
		return respx.OK(ctx, principal), nil
	})
	authz.Register(registrar, api, huma.Operation{OperationID: "identity-restore-principal", Method: http.MethodPost, Path: "/iam/principals/{id}/restore", Summary: "Restore a disabled principal", Tags: []string{"identity"}}, authz.Guard{Resource: "platform", Verb: "administer"}, func(ctx context.Context, in *idInput) (*respx.Body[Principal], error) {
		principal, err := service.SetStatus(ctx, in.TenantID, in.ID, "active")
		if err != nil {
			return nil, identityError(err)
		}
		return respx.OK(ctx, principal), nil
	})
}

func identityError(err error) error {
	if errors.Is(err, ErrNotFound) {
		return huma.Error404NotFound("principal_not_found")
	}
	if errors.Is(err, ErrLoginConflict) {
		return huma.Error409Conflict("login_name_exists")
	}
	if errors.Is(err, ErrInvalid) {
		return huma.Error422UnprocessableEntity("invalid_principal")
	}
	if errors.Is(err, iamdomain.ErrLastTenantAdministrator) {
		return huma.Error409Conflict("last_tenant_administrator")
	}
	return huma.Error500InternalServerError("identity_unavailable")
}
