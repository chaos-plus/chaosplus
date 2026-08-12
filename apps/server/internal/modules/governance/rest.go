package governance

import (
	"context"
	"errors"
	"net/http"
	"time"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/danielgtaylor/huma/v2"
)

func parseGovernanceID(value string) (guid.ID, error) {
	id, err := guid.Parse(value)
	if err != nil {
		return 0, huma.Error422UnprocessableEntity("invalid_id")
	}
	return id, nil
}

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
		tenantID, err := parseGovernanceID(in.TenantID)
		if err != nil {
			return nil, err
		}
		items, err := service.RequestableRoles(ctx, tenantID)
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
		tenantID, err := parseGovernanceID(in.TenantID)
		if err != nil {
			return nil, err
		}
		principalID, err := humanPrincipal(ctx)
		if err != nil {
			return nil, err
		}
		roleID, err := parseGovernanceID(in.Body.RoleID)
		if err != nil {
			return nil, err
		}
		item, err := service.Create(ctx, tenantID, principalID, CreateAccessRequest{
			RoleID: roleID, Reason: in.Body.Reason, AccessExpiresAt: in.Body.AccessExpiresAt,
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
		tenantID, err := parseGovernanceID(in.TenantID)
		if err != nil {
			return nil, err
		}
		principalID, err := humanPrincipal(ctx)
		if err != nil {
			return nil, err
		}
		items, err := service.List(ctx, tenantID, principalID)
		if err != nil {
			return nil, governanceError(err)
		}
		return respx.OK(ctx, items), nil
	})

	authz.Register(registrar, api, huma.Operation{
		OperationID: "governance-list-access-requests", Method: http.MethodGet, Path: "/iam/access-requests",
		Summary: "List tenant access requests for approval", Tags: []string{"governance"}, Errors: []int{http.StatusUnprocessableEntity, http.StatusInternalServerError},
	}, authz.Guard{Resource: "access_request", Verb: "view"}, func(ctx context.Context, in *tenantInput) (*respx.Body[[]AccessRequest], error) {
		tenantID, err := parseGovernanceID(in.TenantID)
		if err != nil {
			return nil, err
		}
		items, err := service.List(ctx, tenantID, 0)
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
		tenantID, err := parseGovernanceID(in.TenantID)
		if err != nil {
			return nil, err
		}
		id, err := parseGovernanceID(in.ID)
		if err != nil {
			return nil, err
		}
		actorID, err := humanPrincipal(ctx)
		if err != nil {
			return nil, err
		}
		item, err := service.Approve(ctx, tenantID, id, actorID, in.Body.Note)
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
		tenantID, err := parseGovernanceID(in.TenantID)
		if err != nil {
			return nil, err
		}
		id, err := parseGovernanceID(in.ID)
		if err != nil {
			return nil, err
		}
		actorID, err := humanPrincipal(ctx)
		if err != nil {
			return nil, err
		}
		item, err := service.Reject(ctx, tenantID, id, actorID, in.Body.Note)
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
		tenantID, err := parseGovernanceID(in.TenantID)
		if err != nil {
			return nil, err
		}
		id, err := parseGovernanceID(in.ID)
		if err != nil {
			return nil, err
		}
		actorID, err := humanPrincipal(ctx)
		if err != nil {
			return nil, err
		}
		item, err := service.Revoke(ctx, tenantID, id, actorID, in.Body.Reason)
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
		tenantID, err := parseGovernanceID(in.TenantID)
		if err != nil {
			return nil, err
		}
		id, err := parseGovernanceID(in.ID)
		if err != nil {
			return nil, err
		}
		principalID, err := humanPrincipal(ctx)
		if err != nil {
			return nil, err
		}
		item, err := service.Withdraw(ctx, tenantID, id, principalID, in.Body.Reason)
		if err != nil {
			return nil, governanceError(err)
		}
		return respx.OK(ctx, item), nil
	})

	registerReviewREST(api, service, registrar)
}

func humanPrincipal(ctx context.Context) (guid.ID, error) {
	claims, ok := authnext.FromContext(ctx)
	if !ok || claims == nil || claims.SubjectType != authnext.SubjectTypePrincipal || claims.PrincipalID.Zero() {
		return 0, huma.Error403Forbidden("human_principal_required")
	}
	return claims.PrincipalID, nil
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
	case errors.Is(err, ErrReviewDynamicDerived):
		return huma.Error422UnprocessableEntity("access_review_derived_not_revocable")
	case errors.Is(err, ErrInvalidReview):
		return huma.Error422UnprocessableEntity("invalid_access_review")
	case errors.Is(err, ErrInvalid):
		return huma.Error422UnprocessableEntity("invalid_access_request")
	default:
		return huma.Error500InternalServerError("governance_unavailable")
	}
}
