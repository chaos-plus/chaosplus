package governance

import (
	"context"
	"net/http"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
	"github.com/danielgtaylor/huma/v2"
)

type createReviewInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	Body     struct {
		Name  string    `json:"name" minLength:"3" maxLength:"128"`
		DueAt time.Time `json:"due_at" format:"date-time"`
	}
}

type reviewInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	ID       string `path:"id" maxLength:"64"`
}

type reviewItemInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	ID       string `path:"id" maxLength:"64"`
	ItemID   string `path:"item_id" maxLength:"64"`
	Body     struct {
		Decision string `json:"decision" minLength:"4" maxLength:"6"`
		Note     string `json:"note,omitempty" maxLength:"500"`
	}
}

func registerReviewREST(api huma.API, service *Service, registrar *authz.Registrar) {
	authz.Register(registrar, api, huma.Operation{
		OperationID: "governance-list-access-reviews", Method: http.MethodGet, Path: "/iam/access-reviews",
		Summary: "List tenant access review campaigns", Tags: []string{"governance"}, Errors: []int{http.StatusUnprocessableEntity, http.StatusInternalServerError},
	}, authz.Guard{Resource: "access_review", Verb: "view"}, func(ctx context.Context, in *tenantInput) (*respx.Body[[]AccessReview], error) {
		tenantID, err := parseGovernanceID(in.TenantID)
		if err != nil {
			return nil, err
		}
		items, err := service.ListReviews(ctx, tenantID)
		if err != nil {
			return nil, governanceError(err)
		}
		return respx.OK(ctx, items), nil
	})

	authz.Register(registrar, api, huma.Operation{
		OperationID: "governance-create-access-review", Method: http.MethodPost, Path: "/iam/access-reviews",
		Summary: "Create a review of current direct and temporary tenant role grants", Tags: []string{"governance"}, DefaultStatus: http.StatusCreated,
		Errors: []int{http.StatusConflict, http.StatusUnprocessableEntity, http.StatusInternalServerError},
	}, authz.Guard{Resource: "access_review", Verb: "create"}, func(ctx context.Context, in *createReviewInput) (*respx.Body[AccessReview], error) {
		tenantID, err := parseGovernanceID(in.TenantID)
		if err != nil {
			return nil, err
		}
		actorID, err := humanPrincipal(ctx)
		if err != nil {
			return nil, err
		}
		item, err := service.CreateReview(ctx, tenantID, actorID, CreateAccessReview{Name: in.Body.Name, DueAt: in.Body.DueAt})
		if err != nil {
			return nil, governanceError(err)
		}
		return respx.OK(ctx, item), nil
	})

	authz.Register(registrar, api, huma.Operation{
		OperationID: "governance-get-access-review", Method: http.MethodGet, Path: "/iam/access-reviews/{id}",
		Summary: "Get an access review and its snapshotted grants", Tags: []string{"governance"},
		Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusInternalServerError},
	}, authz.Guard{Resource: "access_review", Verb: "view"}, func(ctx context.Context, in *reviewInput) (*respx.Body[AccessReview], error) {
		tenantID, err := parseGovernanceID(in.TenantID)
		if err != nil {
			return nil, err
		}
		id, err := parseGovernanceID(in.ID)
		if err != nil {
			return nil, err
		}
		item, err := service.GetReview(ctx, tenantID, id)
		if err != nil {
			return nil, governanceError(err)
		}
		return respx.OK(ctx, item), nil
	})

	authz.Register(registrar, api, huma.Operation{
		OperationID: "governance-decide-access-review-item", Method: http.MethodPost, Path: "/iam/access-reviews/{id}/items/{item_id}/decide",
		Summary: "Keep or revoke one snapshotted tenant role grant", Tags: []string{"governance"},
		Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusGone, http.StatusUnprocessableEntity, http.StatusInternalServerError},
	}, authz.Guard{Resource: "access_review", Verb: "decide"}, func(ctx context.Context, in *reviewItemInput) (*respx.Body[AccessReviewItem], error) {
		tenantID, err := parseGovernanceID(in.TenantID)
		if err != nil {
			return nil, err
		}
		id, err := parseGovernanceID(in.ID)
		if err != nil {
			return nil, err
		}
		itemID, err := parseGovernanceID(in.ItemID)
		if err != nil {
			return nil, err
		}
		actorID, err := humanPrincipal(ctx)
		if err != nil {
			return nil, err
		}
		item, err := service.DecideReviewItem(ctx, tenantID, id, itemID, actorID, in.Body.Decision, in.Body.Note)
		if err != nil {
			return nil, governanceError(err)
		}
		return respx.OK(ctx, item), nil
	})

	for _, operation := range []struct {
		id, path, summary, status string
	}{
		{"governance-complete-access-review", "/iam/access-reviews/{id}/complete", "Complete an access review after every item is decided", ReviewStatusCompleted},
		{"governance-cancel-access-review", "/iam/access-reviews/{id}/cancel", "Cancel an open access review", ReviewStatusCancelled},
	} {
		operation := operation
		authz.Register(registrar, api, huma.Operation{
			OperationID: operation.id, Method: http.MethodPost, Path: operation.path, Summary: operation.summary, Tags: []string{"governance"},
			Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusGone, http.StatusUnprocessableEntity, http.StatusInternalServerError},
		}, authz.Guard{Resource: "access_review", Verb: "manage"}, func(ctx context.Context, in *reviewInput) (*respx.Body[AccessReview], error) {
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
			var item AccessReview
			if operation.status == ReviewStatusCompleted {
				item, err = service.CompleteReview(ctx, tenantID, id, actorID)
			} else {
				item, err = service.CancelReview(ctx, tenantID, id, actorID)
			}
			if err != nil {
				return nil, governanceError(err)
			}
			return respx.OK(ctx, item), nil
		})
	}
}
