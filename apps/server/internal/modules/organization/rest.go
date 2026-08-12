package organization

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/danielgtaylor/huma/v2"
)

func parseOrganizationID(value string) (guid.ID, error) {
	id, err := guid.Parse(strings.TrimSpace(value))
	if err != nil {
		return 0, huma.Error422UnprocessableEntity("invalid_id")
	}
	return id, nil
}

func parseOptionalOrganizationID(value string) (guid.ID, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	return parseOrganizationID(value)
}

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
		tenantID, err := parseOrganizationID(in.TenantID)
		if err != nil {
			return nil, err
		}
		departments, err := service.List(ctx, tenantID)
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
		tenantID, err := parseOrganizationID(in.TenantID)
		if err != nil {
			return nil, err
		}
		parentID, err := parseOptionalOrganizationID(in.Body.ParentID)
		if err != nil {
			return nil, err
		}
		department, err := service.Create(ctx, tenantID, CreateDepartment{
			ParentID: parentID, Name: in.Body.Name, Status: in.Body.Status, SortOrder: in.Body.SortOrder,
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
		tenantID, err := parseOrganizationID(in.TenantID)
		if err != nil {
			return nil, err
		}
		id, err := parseOrganizationID(in.ID)
		if err != nil {
			return nil, err
		}
		department, err := service.Get(ctx, tenantID, id)
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
		tenantID, err := parseOrganizationID(in.TenantID)
		if err != nil {
			return nil, err
		}
		id, err := parseOrganizationID(in.ID)
		if err != nil {
			return nil, err
		}
		update := UpdateDepartment{
			Name: in.Body.Name, Status: in.Body.Status,
			SortOrder: in.Body.SortOrder, Version: in.Body.Version,
		}
		if in.Body.ParentID != nil {
			parentID, err := parseOptionalOrganizationID(*in.Body.ParentID)
			if err != nil {
				return nil, err
			}
			update.ParentID = &parentID
		}
		department, err := service.Update(ctx, tenantID, id, update)
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
		tenantID, err := parseOrganizationID(in.TenantID)
		if err != nil {
			return nil, err
		}
		id, err := parseOrganizationID(in.ID)
		if err != nil {
			return nil, err
		}
		if err := service.Delete(ctx, tenantID, id, in.Version); err != nil {
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
