package organization

import (
	"context"
	"errors"
	"net/http"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
	"github.com/danielgtaylor/huma/v2"
)

type myTenantsInput struct {
	IncludeDeleted bool `query:"include_deleted" doc:"include deleted tenants"`

	listTenantsInput
}

type listTenantsInput struct {
	IncludeDeleted bool `query:"include_deleted"`
}

type tenantIDInput struct {
	ID string `path:"tenant_id" maxLength:"128"`
}

type createTenantInput struct {
	Body struct {
		Slug string `json:"slug" minLength:"3" maxLength:"63" pattern:"^[a-z0-9](?:[a-z0-9-]{1,61}[a-z0-9])?$"`
		Name string `json:"name" minLength:"1" maxLength:"128"`
	}
}

type updateTenantInput struct {
	ID   string `path:"tenant_id" maxLength:"128"`
	Body struct {
		Name    *string `json:"name,omitempty" minLength:"1" maxLength:"128"`
		Status  *string `json:"status,omitempty" enum:"active,suspended"`
		Version int64   `json:"version" minimum:"1"`
	}
}

type deleteTenantInput struct {
	ID      string `path:"tenant_id" maxLength:"128"`
	Version int64  `query:"version" minimum:"1"`
}

type deletedTenant struct {
	Deleted bool `json:"deleted"`
}

func RegisterTenantREST(api huma.API, service *TenantService, registrar *authz.Registrar) {
	// 当前用户自己的租户(登录即可,不要求平台管理员)。解决注册用户无法列出
	// 自己租户的问题 —— /iam/tenants 是平台级操作。
	authz.RegisterAuthenticated(registrar, api, huma.Operation{OperationID: "organization-my-tenants", Method: http.MethodGet, Path: "/iam/me/tenants", Summary: "List the caller's tenants", Tags: []string{"organization"}}, func(ctx context.Context, _ *myTenantsInput) (*respx.Body[[]Tenant], error) {
		claims, ok := authn.FromContext(ctx)
		if !ok || claims == nil || claims.PrincipalID.Zero() {
			return nil, errors.New("not authenticated")
		}
		items, err := service.ListByMember(ctx, claims.PrincipalID)
		if err != nil {
			return nil, tenantError(err)
		}
		return respx.OK(ctx, items), nil
	})

	authz.RegisterPlatform(registrar, api, huma.Operation{OperationID: "organization-list-tenants", Method: http.MethodGet, Path: "/iam/tenants", Summary: "List platform tenants", Tags: []string{"organization"}}, authz.Guard{Resource: "tenant", Verb: "view"}, func(ctx context.Context, in *listTenantsInput) (*respx.Body[[]Tenant], error) {
		items, err := service.List(ctx, in.IncludeDeleted)
		if err != nil {
			return nil, tenantError(err)
		}
		return respx.OK(ctx, items), nil
	})

	authz.RegisterPlatform(registrar, api, huma.Operation{OperationID: "organization-create-tenant", Method: http.MethodPost, Path: "/iam/tenants", Summary: "Create a platform tenant", Tags: []string{"organization"}, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusConflict, http.StatusUnprocessableEntity}}, authz.Guard{Resource: "tenant", Verb: "create"}, func(ctx context.Context, in *createTenantInput) (*respx.Body[Tenant], error) {
		item, err := service.Create(ctx, CreateTenant{Slug: in.Body.Slug, Name: in.Body.Name})
		if err != nil {
			return nil, tenantError(err)
		}
		return respx.OK(ctx, item), nil
	})

	authz.RegisterPlatform(registrar, api, huma.Operation{OperationID: "organization-get-tenant", Method: http.MethodGet, Path: "/iam/tenants/{tenant_id}", Summary: "Get a platform tenant", Tags: []string{"organization"}, Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity}}, authz.Guard{Resource: "tenant", Verb: "view"}, func(ctx context.Context, in *tenantIDInput) (*respx.Body[Tenant], error) {
		id, err := parseOrganizationID(in.ID)
		if err != nil {
			return nil, err
		}
		item, err := service.Get(ctx, id)
		if err != nil {
			return nil, tenantError(err)
		}
		return respx.OK(ctx, item), nil
	})

	authz.RegisterPlatform(registrar, api, huma.Operation{OperationID: "organization-update-tenant", Method: http.MethodPatch, Path: "/iam/tenants/{tenant_id}", Summary: "Update or suspend a platform tenant", Tags: []string{"organization"}, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity}}, authz.Guard{Resource: "tenant", Verb: "update"}, func(ctx context.Context, in *updateTenantInput) (*respx.Body[Tenant], error) {
		id, err := parseOrganizationID(in.ID)
		if err != nil {
			return nil, err
		}
		item, err := service.Update(ctx, id, UpdateTenant{Name: in.Body.Name, Status: in.Body.Status, Version: in.Body.Version})
		if err != nil {
			return nil, tenantError(err)
		}
		return respx.OK(ctx, item), nil
	})

	authz.RegisterPlatform(registrar, api, huma.Operation{OperationID: "organization-delete-tenant", Method: http.MethodDelete, Path: "/iam/tenants/{tenant_id}", Summary: "Soft-delete a platform tenant", Tags: []string{"organization"}, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity}}, authz.Guard{Resource: "tenant", Verb: "delete"}, func(ctx context.Context, in *deleteTenantInput) (*respx.Body[deletedTenant], error) {
		id, err := parseOrganizationID(in.ID)
		if err != nil {
			return nil, err
		}
		if err := service.Delete(ctx, id, in.Version); err != nil {
			return nil, tenantError(err)
		}
		return respx.OK(ctx, deletedTenant{Deleted: true}), nil
	})
}

func tenantError(err error) error {
	switch {
	case errors.Is(err, ErrTenantNotFound):
		return huma.Error404NotFound("tenant_not_found")
	case errors.Is(err, ErrTenantSlugConflict):
		return huma.Error409Conflict("tenant_slug_exists")
	case errors.Is(err, ErrTenantVersionConflict):
		return huma.Error409Conflict("tenant_version_conflict")
	case errors.Is(err, ErrInvalidTenant):
		return huma.Error422UnprocessableEntity("invalid_tenant")
	default:
		return huma.Error500InternalServerError("organization_unavailable")
	}
}
