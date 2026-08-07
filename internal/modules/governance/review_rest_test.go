package governance

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccessReviewHTTPWorkflowAndOpenAPI(t *testing.T) {
	fixture := newGovernanceFixture(t)
	addDirectReviewGrant(t, fixture, "requester", governanceNow)
	server, api := newGovernanceHTTPServer(t, fixture.service)

	createdResponse := governanceRequest(t, server, http.MethodPost, "/iam/access-reviews", "tenant-a", "approver", authn.SubjectTypePrincipal, "en-US", map[string]any{
		"name": "Quarterly review", "due_at": governanceNow.Add(7 * 24 * time.Hour),
	})
	require.Equal(t, http.StatusCreated, createdResponse.status, createdResponse.Message)
	var review AccessReview
	require.NoError(t, json.Unmarshal(createdResponse.Data, &review))
	require.Len(t, review.Items, 1)

	listed := governanceRequest(t, server, http.MethodGet, "/iam/access-reviews", "tenant-a", "approver", authn.SubjectTypePrincipal, "en-US", nil)
	require.Equal(t, http.StatusOK, listed.status, listed.Message)
	var reviews []AccessReview
	require.NoError(t, json.Unmarshal(listed.Data, &reviews))
	require.Len(t, reviews, 1)
	assert.NotNil(t, reviews[0].Items)

	loaded := governanceRequest(t, server, http.MethodGet, "/iam/access-reviews/"+review.ID, "tenant-a", "approver", authn.SubjectTypePrincipal, "en-US", nil)
	assert.Equal(t, http.StatusOK, loaded.status, loaded.Message)
	decided := governanceRequest(t, server, http.MethodPost, "/iam/access-reviews/"+review.ID+"/items/"+review.Items[0].ID+"/decide", "tenant-a", "other", authn.SubjectTypePrincipal, "en-US", map[string]any{
		"decision": ReviewDecisionKeep, "note": "still required",
	})
	assert.Equal(t, http.StatusOK, decided.status, decided.Message)
	completed := governanceRequest(t, server, http.MethodPost, "/iam/access-reviews/"+review.ID+"/complete", "tenant-a", "approver", authn.SubjectTypePrincipal, "en-US", nil)
	assert.Equal(t, http.StatusOK, completed.status, completed.Message)

	addDirectReviewGrant(t, fixture, "other", governanceNow)
	cancelResponse := governanceRequest(t, server, http.MethodPost, "/iam/access-reviews", "tenant-a", "approver", authn.SubjectTypePrincipal, "en-US", map[string]any{
		"name": "Cancelled review", "due_at": governanceNow.Add(7 * 24 * time.Hour),
	})
	require.Equal(t, http.StatusCreated, cancelResponse.status, cancelResponse.Message)
	var cancelledReview AccessReview
	require.NoError(t, json.Unmarshal(cancelResponse.Data, &cancelledReview))
	cancelled := governanceRequest(t, server, http.MethodPost, "/iam/access-reviews/"+cancelledReview.ID+"/cancel", "tenant-a", "approver", authn.SubjectTypePrincipal, "en-US", nil)
	assert.Equal(t, http.StatusOK, cancelled.status, cancelled.Message)

	guards := map[string]string{
		api.OpenAPI().Paths["/iam/access-reviews"].Get.OperationID:                              api.OpenAPI().Paths["/iam/access-reviews"].Get.Extensions[authz.GuardExtensionKey].(string),
		api.OpenAPI().Paths["/iam/access-reviews"].Post.OperationID:                             api.OpenAPI().Paths["/iam/access-reviews"].Post.Extensions[authz.GuardExtensionKey].(string),
		api.OpenAPI().Paths["/iam/access-reviews/{id}"].Get.OperationID:                         api.OpenAPI().Paths["/iam/access-reviews/{id}"].Get.Extensions[authz.GuardExtensionKey].(string),
		api.OpenAPI().Paths["/iam/access-reviews/{id}/items/{item_id}/decide"].Post.OperationID: api.OpenAPI().Paths["/iam/access-reviews/{id}/items/{item_id}/decide"].Post.Extensions[authz.GuardExtensionKey].(string),
		api.OpenAPI().Paths["/iam/access-reviews/{id}/complete"].Post.OperationID:               api.OpenAPI().Paths["/iam/access-reviews/{id}/complete"].Post.Extensions[authz.GuardExtensionKey].(string),
		api.OpenAPI().Paths["/iam/access-reviews/{id}/cancel"].Post.OperationID:                 api.OpenAPI().Paths["/iam/access-reviews/{id}/cancel"].Post.Extensions[authz.GuardExtensionKey].(string),
	}
	assert.Equal(t, map[string]string{
		"governance-list-access-reviews":       "access_review_view",
		"governance-create-access-review":      "access_review_create",
		"governance-get-access-review":         "access_review_view",
		"governance-decide-access-review-item": "access_review_decide",
		"governance-complete-access-review":    "access_review_manage",
		"governance-cancel-access-review":      "access_review_manage",
	}, guards)
	assert.Equal(t, http.StatusCreated, api.OpenAPI().Paths["/iam/access-reviews"].Post.DefaultStatus)
}

func TestAccessReviewHTTPErrorsAreLocalized(t *testing.T) {
	fixture := newGovernanceFixture(t)
	server, _ := newGovernanceHTTPServer(t, fixture.service)
	for _, item := range []struct {
		locale, expected string
	}{
		{"en-US", "There are no active direct or temporary tenant role grants to review."},
		{"zh-CN", "当前没有可复核的有效直接角色授权或临时角色授权。"},
		{"ms-MY", "Tiada pemberian peranan penyewa langsung atau sementara yang aktif untuk disemak."},
	} {
		response := governanceRequest(t, server, http.MethodPost, "/iam/access-reviews", "tenant-a", "approver", authn.SubjectTypePrincipal, item.locale, map[string]any{
			"name": "Empty review", "due_at": governanceNow.Add(time.Hour),
		})
		assert.Equal(t, http.StatusConflict, response.status)
		assert.Equal(t, item.expected, response.Message)
		assert.NotContains(t, response.Message, "access_review_")
	}

	serviceAccount := governanceRequest(t, server, http.MethodPost, "/iam/access-reviews", "tenant-a", "service", authn.SubjectTypeServiceAccount, "ms-MY", map[string]any{
		"name": "Service review", "due_at": governanceNow.Add(time.Hour),
	})
	assert.Equal(t, http.StatusForbidden, serviceAccount.status)
	assert.NotContains(t, serviceAccount.Message, "human_principal_required")

	missing := governanceRequest(t, server, http.MethodGet, "/iam/access-reviews/missing", "tenant-a", "approver", authn.SubjectTypePrincipal, "en-US", nil)
	assert.Equal(t, http.StatusNotFound, missing.status)
	assert.Equal(t, "The access review was not found in this tenant. Refresh the review list and verify its identifier.", missing.Message)

	missingDecision := governanceRequest(t, server, http.MethodPost, "/iam/access-reviews/missing/items/missing/decide", "tenant-a", "approver", authn.SubjectTypePrincipal, "en-US", map[string]any{
		"decision": ReviewDecisionKeep,
	})
	assert.Equal(t, http.StatusNotFound, missingDecision.status)

	missingCompletion := governanceRequest(t, server, http.MethodPost, "/iam/access-reviews/missing/complete", "tenant-a", "approver", authn.SubjectTypePrincipal, "en-US", nil)
	assert.Equal(t, http.StatusNotFound, missingCompletion.status)

	serviceDecision := governanceRequest(t, server, http.MethodPost, "/iam/access-reviews/missing/items/missing/decide", "tenant-a", "service", authn.SubjectTypeServiceAccount, "en-US", map[string]any{
		"decision": ReviewDecisionKeep,
	})
	assert.Equal(t, http.StatusForbidden, serviceDecision.status)
	serviceCompletion := governanceRequest(t, server, http.MethodPost, "/iam/access-reviews/missing/complete", "tenant-a", "service", authn.SubjectTypeServiceAccount, "en-US", nil)
	assert.Equal(t, http.StatusForbidden, serviceCompletion.status)

	brokenFixture := newGovernanceFixture(t)
	_, err := brokenFixture.db.ExecContext(t.Context(), "DROP TABLE iam_access_reviews")
	require.NoError(t, err)
	brokenServer, _ := newGovernanceHTTPServer(t, brokenFixture.service)
	listFailure := governanceRequest(t, brokenServer, http.MethodGet, "/iam/access-reviews", "tenant-a", "approver", authn.SubjectTypePrincipal, "en-US", nil)
	assert.Equal(t, http.StatusInternalServerError, listFailure.status)
	assert.Equal(t, "The access governance service is temporarily unavailable. No governance change was committed; try again later.", listFailure.Message)
}
