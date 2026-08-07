package iam

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
	iamdomain "github.com/chaos-plus/chaosplus/internal/modules/iam/domain"
)

// PlatformAdministratorView is the API projection of one principal's platform
// authorization.
type PlatformAdministratorView struct {
	PrincipalID       string    `json:"principal_id"`
	FullAdministrator bool      `json:"full_administrator" doc:"true when the principal holds every declared platform permission"`
	Permissions       []string  `json:"permissions" doc:"explicit platform permission codes; empty for a full administrator"`
	CreatedAt         time.Time `json:"created_at"`
}

type putPlatformAdministratorInput struct {
	PrincipalID string `path:"principal_id" maxLength:"64"`
	Body        struct {
		FullAdministrator bool     `json:"full_administrator" doc:"grant every declared platform permission"`
		Permissions       []string `json:"permissions,omitempty" maxItems:"32" doc:"explicit platform permission codes; must be empty when full_administrator is true"`
	}
}

type platformPrincipalInput struct {
	PrincipalID string `path:"principal_id" maxLength:"64"`
}

func registerPlatformREST(a huma.API, svc *Service, registrar *authz.Registrar) {
	authz.RegisterPlatform(registrar, a, huma.Operation{
		OperationID: "iam-platform-permission-catalog", Method: http.MethodGet, Path: "/iam/platform-permission-catalog",
		Summary: "List declared platform permissions", Tags: []string{"iam"},
	}, authz.Guard{Resource: "platform", Verb: "administer"}, func(ctx context.Context, _ *struct{}) (*respx.Body[[]authz.Action], error) {
		return respx.OK(ctx, svc.PlatformPermissionCatalog(ctx)), nil
	})

	authz.RegisterPlatform(registrar, a, huma.Operation{
		OperationID: "iam-list-platform-administrators", Method: http.MethodGet, Path: "/iam/platform-administrators",
		Summary: "List platform administrators and their platform permissions", Tags: []string{"iam"},
	}, authz.Guard{Resource: "platform", Verb: "administer"}, func(ctx context.Context, _ *struct{}) (*respx.Body[[]PlatformAdministratorView], error) {
		administrators, err := svc.ListPlatformAdministrators(ctx)
		if err != nil {
			return nil, platformAPIError("list platform administrators", err)
		}
		return respx.OK(ctx, platformAdministratorsFromDomain(administrators)), nil
	})

	authz.RegisterPlatform(registrar, a, huma.Operation{
		OperationID: "iam-put-platform-administrator", Method: http.MethodPut, Path: "/iam/platform-administrators/{principal_id}",
		Summary: "Grant or restrict platform authorization for a principal", Tags: []string{"iam"},
		Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity},
	}, authz.Guard{Resource: "platform", Verb: "administer"}, func(ctx context.Context, in *putPlatformAdministratorInput) (*respx.Body[PlatformAdministratorView], error) {
		administrator, err := svc.SetPlatformAdministrator(ctx, in.PrincipalID, in.Body.FullAdministrator, in.Body.Permissions)
		if err != nil {
			return nil, platformAPIError("put platform administrator", err)
		}
		return respx.OK(ctx, platformAdministratorFromDomain(administrator)), nil
	})

	authz.RegisterPlatform(registrar, a, huma.Operation{
		OperationID: "iam-delete-platform-administrator", Method: http.MethodDelete, Path: "/iam/platform-administrators/{principal_id}",
		Summary: "Revoke every platform grant for a principal", Tags: []string{"iam"},
		Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity},
	}, authz.Guard{Resource: "platform", Verb: "administer"}, func(ctx context.Context, in *platformPrincipalInput) (*respx.Body[MutationResult], error) {
		changed, err := svc.DeletePlatformAdministrator(ctx, in.PrincipalID)
		if err != nil {
			return nil, platformAPIError("delete platform administrator", err)
		}
		return respx.OK(ctx, MutationResult{Changed: changed, SyncStatus: "applied"}), nil
	})
}

// platformAPIError maps platform authorization failures before delegating to
// the shared IAM error contract.
func platformAPIError(operation string, err error) error {
	switch {
	case errors.Is(err, iamdomain.ErrPlatformAdministratorNotFound):
		return huma.Error404NotFound("platform_administrator_not_found")
	case errors.Is(err, iamdomain.ErrLastPlatformAdministrator):
		return huma.Error409Conflict("last_platform_administrator")
	case errors.Is(err, iamdomain.ErrPlatformPermissionScope):
		return huma.Error422UnprocessableEntity("platform_permission_scope")
	default:
		return apiError(operation, err)
	}
}

func platformAdministratorFromDomain(administrator iamdomain.PlatformAdministrator) PlatformAdministratorView {
	permissions := administrator.Permissions
	if permissions == nil {
		permissions = []string{}
	}
	return PlatformAdministratorView{
		PrincipalID: administrator.PrincipalID, FullAdministrator: administrator.FullAdministrator,
		Permissions: permissions, CreatedAt: administrator.CreatedAt,
	}
}

func platformAdministratorsFromDomain(administrators []iamdomain.PlatformAdministrator) []PlatformAdministratorView {
	result := make([]PlatformAdministratorView, 0, len(administrators))
	for _, administrator := range administrators {
		result = append(result, platformAdministratorFromDomain(administrator))
	}
	return result
}
