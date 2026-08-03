package governance

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
	"github.com/danielgtaylor/huma/v2"
)

type tenantInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
}

type createRequestInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	Body     struct {
		RoleID          string    `json:"role_id" minLength:"1" maxLength:"32"`
		Reason          string    `json:"reason" minLength:"3" maxLength:"500"`
		AccessExpiresAt time.Time `json:"access_expires_at" format:"date-time"`
	}
}

type decisionInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	ID       string `path:"id" maxLength:"64"`
	Body     struct {
		Note string `json:"note,omitempty" maxLength:"500"`
	}
}

type revokeInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	ID       string `path:"id" maxLength:"64"`
	Body     struct {
		Reason string `json:"reason,omitempty" maxLength:"500"`
	}
}

func RegisterREST(api huma.API, service *Service, registrar *authz.Registrar) {
	authz.RegisterTenantMember(registrar, api, huma.Operation{
		OperationID: "governance-list-requestable-roles", Method: http.MethodGet, Path: "/iam/requestable-roles",
		Summary: "List tenant roles available for access requests", Tags: []string{"governance"}, Errors: []int{http.StatusUnprocessableEntity, http.StatusInternalServerError},
	}, func(ctx context.Context, in *tenantInput) (*respx.Body[[]RequestableRole], error) {
		items, err := service.RequestableRoles(ctx, in.TenantID)
		if err != nil {
			return nil, governanceError(err)
		}
		return respx.OK(ctx, items), nil
	})

	authz.RegisterTenantMember(registrar, api, huma.Operation{
		OperationID: "governance-create-access-request", Method: http.MethodPost, Path: "/iam/access-requests",
		Summary: "Request time-limited membership in a tenant role", Tags: []string{"governance"}, DefaultStatus: http.StatusCreated,
		Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusInternalServerError},
	}, func(ctx context.Context, in *createRequestInput) (*respx.Body[AccessRequest], error) {
		principalID, err := humanPrincipal(ctx)
		if err != nil {
			return nil, err
		}
		item, err := service.Create(ctx, in.TenantID, principalID, CreateAccessRequest{
			RoleID: in.Body.RoleID, Reason: in.Body.Reason, AccessExpiresAt: in.Body.AccessExpiresAt,
		})
		if err != nil {
			return nil, governanceError(err)
		}
		return respx.OK(ctx, item), nil
	})

	authz.RegisterTenantMember(registrar, api, huma.Operation{
		OperationID: "governance-list-my-access-requests", Method: http.MethodGet, Path: "/iam/my/access-requests",
		Summary: "List the current principal's tenant access requests", Tags: []string{"governance"}, Errors: []int{http.StatusUnprocessableEntity, http.StatusInternalServerError},
	}, func(ctx context.Context, in *tenantInput) (*respx.Body[[]AccessRequest], error) {
		principalID, err := humanPrincipal(ctx)
		if err != nil {
			return nil, err
		}
		items, err := service.List(ctx, in.TenantID, principalID)
		if err != nil {
			return nil, governanceError(err)
		}
		return respx.OK(ctx, items), nil
	})

	authz.Register(registrar, api, huma.Operation{
		OperationID: "governance-list-access-requests", Method: http.MethodGet, Path: "/iam/access-requests",
		Summary: "List tenant access requests for approval", Tags: []string{"governance"}, Errors: []int{http.StatusUnprocessableEntity, http.StatusInternalServerError},
	}, authz.Guard{Resource: "access_request", Verb: "view"}, func(ctx context.Context, in *tenantInput) (*respx.Body[[]AccessRequest], error) {
		items, err := service.List(ctx, in.TenantID, "")
		if err != nil {
			return nil, governanceError(err)
		}
		return respx.OK(ctx, items), nil
	})

	authz.Register(registrar, api, huma.Operation{
		OperationID: "governance-approve-access-request", Method: http.MethodPost, Path: "/iam/access-requests/{id}/approve",
		Summary: "Approve an access request and activate its temporary role grant", Tags: []string{"governance"},
		Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusGone, http.StatusUnprocessableEntity, http.StatusInternalServerError},
	}, authz.Guard{Resource: "access_request", Verb: "approve"}, func(ctx context.Context, in *decisionInput) (*respx.Body[AccessRequest], error) {
		actorID, err := humanPrincipal(ctx)
		if err != nil {
			return nil, err
		}
		item, err := service.Approve(ctx, in.TenantID, in.ID, actorID, in.Body.Note)
		if err != nil {
			return nil, governanceError(err)
		}
		return respx.OK(ctx, item), nil
	})

	authz.Register(registrar, api, huma.Operation{
		OperationID: "governance-reject-access-request", Method: http.MethodPost, Path: "/iam/access-requests/{id}/reject",
		Summary: "Reject a pending access request", Tags: []string{"governance"},
		Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusGone, http.StatusUnprocessableEntity, http.StatusInternalServerError},
	}, authz.Guard{Resource: "access_request", Verb: "approve"}, func(ctx context.Context, in *decisionInput) (*respx.Body[AccessRequest], error) {
		actorID, err := humanPrincipal(ctx)
		if err != nil {
			return nil, err
		}
		item, err := service.Reject(ctx, in.TenantID, in.ID, actorID, in.Body.Note)
		if err != nil {
			return nil, governanceError(err)
		}
		return respx.OK(ctx, item), nil
	})

	authz.Register(registrar, api, huma.Operation{
		OperationID: "governance-revoke-access-grant", Method: http.MethodPost, Path: "/iam/access-requests/{id}/revoke",
		Summary: "Revoke an approved temporary role grant", Tags: []string{"governance"},
		Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusGone, http.StatusUnprocessableEntity, http.StatusInternalServerError},
	}, authz.Guard{Resource: "access_request", Verb: "approve"}, func(ctx context.Context, in *revokeInput) (*respx.Body[AccessRequest], error) {
		actorID, err := humanPrincipal(ctx)
		if err != nil {
			return nil, err
		}
		item, err := service.Revoke(ctx, in.TenantID, in.ID, actorID, in.Body.Reason)
		if err != nil {
			return nil, governanceError(err)
		}
		return respx.OK(ctx, item), nil
	})

	authz.RegisterTenantMember(registrar, api, huma.Operation{
		OperationID: "governance-withdraw-access-request", Method: http.MethodPost, Path: "/iam/access-requests/{id}/withdraw",
		Summary: "Withdraw a pending request or relinquish its approved access", Tags: []string{"governance"},
		Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusGone, http.StatusUnprocessableEntity, http.StatusInternalServerError},
	}, func(ctx context.Context, in *revokeInput) (*respx.Body[AccessRequest], error) {
		principalID, err := humanPrincipal(ctx)
		if err != nil {
			return nil, err
		}
		item, err := service.Withdraw(ctx, in.TenantID, in.ID, principalID, in.Body.Reason)
		if err != nil {
			return nil, governanceError(err)
		}
		return respx.OK(ctx, item), nil
	})

	registerReviewREST(api, service, registrar)
}

func humanPrincipal(ctx context.Context) (string, error) {
	claims, ok := authnext.FromContext(ctx)
	if !ok || claims.SubjectType != authnext.SubjectTypePrincipal || strings.TrimSpace(claims.Subject) == "" {
		return "", huma.Error403Forbidden("human_principal_required")
	}
	return claims.Subject, nil
}

func governanceError(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return huma.Error404NotFound("access_request_not_found")
	case errors.Is(err, ErrRoleNotFound):
		return huma.Error404NotFound("access_request_role_not_found")
	case errors.Is(err, ErrExpired):
		return huma.NewError(http.StatusGone, "access_request_expired")
	case errors.Is(err, ErrRequesterInactive):
		return huma.Error409Conflict("access_request_requester_inactive")
	case errors.Is(err, ErrAlreadyGranted):
		return huma.Error409Conflict("access_already_granted")
	case errors.Is(err, ErrStateConflict):
		return huma.Error409Conflict("access_request_state_conflict")
	case errors.Is(err, ErrSelfApproval):
		return huma.Error409Conflict("access_request_self_approval")
	case errors.Is(err, ErrRequesterOnly):
		return huma.Error403Forbidden("access_request_requester_only")
	case errors.Is(err, ErrReviewNotFound):
		return huma.Error404NotFound("access_review_not_found")
	case errors.Is(err, ErrReviewItemNotFound):
		return huma.Error404NotFound("access_review_item_not_found")
	case errors.Is(err, ErrReviewExpired):
		return huma.NewError(http.StatusGone, "access_review_expired")
	case errors.Is(err, ErrReviewEmpty):
		return huma.Error409Conflict("access_review_empty")
	case errors.Is(err, ErrReviewStateConflict):
		return huma.Error409Conflict("access_review_state_conflict")
	case errors.Is(err, ErrReviewIncomplete):
		return huma.Error409Conflict("access_review_incomplete")
	case errors.Is(err, ErrReviewSelfDecision):
		return huma.Error409Conflict("access_review_self_decision")
	case errors.Is(err, ErrReviewLastAdministrator):
		return huma.Error409Conflict("access_review_last_administrator")
	case errors.Is(err, ErrInvalidReview):
		return huma.Error422UnprocessableEntity("invalid_access_review")
	case errors.Is(err, ErrInvalid):
		return huma.Error422UnprocessableEntity("invalid_access_request")
	default:
		return huma.Error500InternalServerError("governance_unavailable")
	}
}
