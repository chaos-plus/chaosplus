package organization

import (
	"context"
	"errors"
	"net/http"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
	"github.com/danielgtaylor/huma/v2"
)

type departmentListInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
}

type departmentIDInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	ID       string `path:"id" maxLength:"128"`
}

type createDepartmentInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	Body     struct {
		ParentID  string `json:"parent_id,omitempty" maxLength:"128"`
		Name      string `json:"name" minLength:"1" maxLength:"128"`
		Status    string `json:"status,omitempty" enum:"active,disabled" default:"active"`
		SortOrder int    `json:"sort_order,omitempty" minimum:"0" maximum:"1000000"`
	}
}

type updateDepartmentInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	ID       string `path:"id" maxLength:"128"`
	Body     struct {
		ParentID  *string `json:"parent_id,omitempty" maxLength:"128"`
		Name      *string `json:"name,omitempty" minLength:"1" maxLength:"128"`
		Status    *string `json:"status,omitempty" enum:"active,disabled"`
		SortOrder *int    `json:"sort_order,omitempty" minimum:"0" maximum:"1000000"`
		Version   int64   `json:"version" minimum:"1"`
	}
}

type deleteDepartmentInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	ID       string `path:"id" maxLength:"128"`
	Version  int64  `query:"version" minimum:"1"`
}

type deletedDepartment struct {
	Deleted bool `json:"deleted"`
}

func RegisterREST(api huma.API, service *Service, registrar *authz.Registrar) {
	authz.Register(registrar, api, huma.Operation{
		OperationID: "organization-list-departments", Method: http.MethodGet, Path: "/iam/departments",
		Summary: "List the tenant department hierarchy", Tags: []string{"organization"}, Errors: []int{http.StatusUnprocessableEntity},
	}, authz.Guard{Resource: "dept", Verb: "view"}, func(ctx context.Context, in *departmentListInput) (*respx.Body[[]Department], error) {
		departments, err := service.List(ctx, in.TenantID)
		if err != nil {
			return nil, organizationError(err)
		}
		return respx.OK(ctx, departments), nil
	})

	authz.Register(registrar, api, huma.Operation{
		OperationID: "organization-create-department", Method: http.MethodPost, Path: "/iam/departments",
		Summary: "Create a tenant department", Tags: []string{"organization"}, DefaultStatus: http.StatusCreated,
		Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity},
	}, authz.Guard{Resource: "dept", Verb: "create"}, func(ctx context.Context, in *createDepartmentInput) (*respx.Body[Department], error) {
		department, err := service.Create(ctx, in.TenantID, CreateDepartment{
			ParentID: in.Body.ParentID, Name: in.Body.Name, Status: in.Body.Status, SortOrder: in.Body.SortOrder,
		})
		if err != nil {
			return nil, organizationError(err)
		}
		return respx.OK(ctx, department), nil
	})

	authz.Register(registrar, api, huma.Operation{
		OperationID: "organization-get-department", Method: http.MethodGet, Path: "/iam/departments/{id}",
		Summary: "Get a tenant department", Tags: []string{"organization"}, Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity},
	}, authz.Guard{Resource: "dept", Verb: "view"}, func(ctx context.Context, in *departmentIDInput) (*respx.Body[Department], error) {
		department, err := service.Get(ctx, in.TenantID, in.ID)
		if err != nil {
			return nil, organizationError(err)
		}
		return respx.OK(ctx, department), nil
	})

	authz.Register(registrar, api, huma.Operation{
		OperationID: "organization-update-department", Method: http.MethodPatch, Path: "/iam/departments/{id}",
		Summary: "Update or move a tenant department", Tags: []string{"organization"},
		Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity},
	}, authz.Guard{Resource: "dept", Verb: "update"}, func(ctx context.Context, in *updateDepartmentInput) (*respx.Body[Department], error) {
		department, err := service.Update(ctx, in.TenantID, in.ID, UpdateDepartment{
			ParentID: in.Body.ParentID, Name: in.Body.Name, Status: in.Body.Status,
			SortOrder: in.Body.SortOrder, Version: in.Body.Version,
		})
		if err != nil {
			return nil, organizationError(err)
		}
		return respx.OK(ctx, department), nil
	})

	authz.Register(registrar, api, huma.Operation{
		OperationID: "organization-delete-department", Method: http.MethodDelete, Path: "/iam/departments/{id}",
		Summary: "Delete an empty tenant department", Tags: []string{"organization"},
		Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity},
	}, authz.Guard{Resource: "dept", Verb: "delete"}, func(ctx context.Context, in *deleteDepartmentInput) (*respx.Body[deletedDepartment], error) {
		if err := service.Delete(ctx, in.TenantID, in.ID, in.Version); err != nil {
			return nil, organizationError(err)
		}
		return respx.OK(ctx, deletedDepartment{Deleted: true}), nil
	})
}

func organizationError(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return huma.Error404NotFound("department_not_found")
	case errors.Is(err, ErrNameConflict):
		return huma.Error409Conflict("department_name_exists")
	case errors.Is(err, ErrHierarchyCycle):
		return huma.Error409Conflict("department_hierarchy_cycle")
	case errors.Is(err, ErrHasChildren):
		return huma.Error409Conflict("department_has_children")
	case errors.Is(err, ErrInUse):
		return huma.Error409Conflict("department_in_use")
	case errors.Is(err, ErrVersionConflict):
		return huma.Error409Conflict("department_version_conflict")
	case errors.Is(err, ErrInvalid):
		return huma.Error422UnprocessableEntity("invalid_department")
	default:
		return huma.Error500InternalServerError("organization_unavailable")
	}
}
