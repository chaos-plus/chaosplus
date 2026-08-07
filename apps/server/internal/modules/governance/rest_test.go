package governance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
	"github.com/chaos-plus/chaosplus/pkg/i18n"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type governanceEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func TestGovernanceHTTPWorkflowAndOpenAPI(t *testing.T) {
	fixture := newGovernanceFixture(t)
	server, api := newGovernanceHTTPServer(t, fixture.service)
	roles := governanceRequest(t, server, http.MethodGet, "/iam/requestable-roles", "tenant-a", "requester", authn.SubjectTypePrincipal, "en-US", nil)
	require.Equal(t, http.StatusOK, roles.status, roles.Message)

	created := governanceRequest(t, server, http.MethodPost, "/iam/access-requests", "tenant-a", "requester", authn.SubjectTypePrincipal, "en-US", map[string]any{
		"role_id": "role-a", "reason": "temporary operations", "access_expires_at": governanceNow.Add(24 * time.Hour),
	})
	require.Equal(t, http.StatusCreated, created.status, created.Message)
	var request AccessRequest
	require.NoError(t, json.Unmarshal(created.Data, &request))

	listed := governanceRequest(t, server, http.MethodGet, "/iam/my/access-requests", "tenant-a", "requester", authn.SubjectTypePrincipal, "en-US", nil)
	require.Equal(t, http.StatusOK, listed.status, listed.Message)
	var requests []AccessRequest
	require.NoError(t, json.Unmarshal(listed.Data, &requests))
	require.Len(t, requests, 1)
	queue := governanceRequest(t, server, http.MethodGet, "/iam/access-requests", "tenant-a", "approver", authn.SubjectTypePrincipal, "en-US", nil)
	assert.Equal(t, http.StatusOK, queue.status, queue.Message)

	approved := governanceRequest(t, server, http.MethodPost, "/iam/access-requests/"+request.ID+"/approve", "tenant-a", "approver", authn.SubjectTypePrincipal, "en-US", map[string]any{"note": "approved"})
	require.Equal(t, http.StatusOK, approved.status, approved.Message)
	withdrawn := governanceRequest(t, server, http.MethodPost, "/iam/access-requests/"+request.ID+"/withdraw", "tenant-a", "requester", authn.SubjectTypePrincipal, "en-US", map[string]any{"reason": "finished"})
	assert.Equal(t, http.StatusOK, withdrawn.status, withdrawn.Message)

	rejectedRequest := createGovernanceHTTPRequest(t, server)
	rejected := governanceRequest(t, server, http.MethodPost, "/iam/access-requests/"+rejectedRequest.ID+"/reject", "tenant-a", "approver", authn.SubjectTypePrincipal, "en-US", map[string]any{"note": "not needed"})
	assert.Equal(t, http.StatusOK, rejected.status, rejected.Message)
	revokedRequest := createGovernanceHTTPRequest(t, server)
	approved = governanceRequest(t, server, http.MethodPost, "/iam/access-requests/"+revokedRequest.ID+"/approve", "tenant-a", "approver", authn.SubjectTypePrincipal, "en-US", map[string]any{"note": "approved"})
	require.Equal(t, http.StatusOK, approved.status, approved.Message)
	revoked := governanceRequest(t, server, http.MethodPost, "/iam/access-requests/"+revokedRequest.ID+"/revoke", "tenant-a", "approver", authn.SubjectTypePrincipal, "en-US", map[string]any{"reason": "revoked"})
	assert.Equal(t, http.StatusOK, revoked.status, revoked.Message)

	operations := map[string]string{
		api.OpenAPI().Paths["/iam/access-requests"].Get.OperationID:               api.OpenAPI().Paths["/iam/access-requests"].Get.Extensions[authz.GuardExtensionKey].(string),
		api.OpenAPI().Paths["/iam/access-requests/{id}/approve"].Post.OperationID: api.OpenAPI().Paths["/iam/access-requests/{id}/approve"].Post.Extensions[authz.GuardExtensionKey].(string),
		api.OpenAPI().Paths["/iam/access-requests/{id}/reject"].Post.OperationID:  api.OpenAPI().Paths["/iam/access-requests/{id}/reject"].Post.Extensions[authz.GuardExtensionKey].(string),
		api.OpenAPI().Paths["/iam/access-requests/{id}/revoke"].Post.OperationID:  api.OpenAPI().Paths["/iam/access-requests/{id}/revoke"].Post.Extensions[authz.GuardExtensionKey].(string),
	}
	assert.Equal(t, map[string]string{
		"governance-list-access-requests":   "access_request_view",
		"governance-approve-access-request": "access_request_approve",
		"governance-reject-access-request":  "access_request_approve",
		"governance-revoke-access-grant":    "access_request_approve",
	}, operations)
	assert.Equal(t, http.StatusCreated, api.OpenAPI().Paths["/iam/access-requests"].Post.DefaultStatus)
}

func createGovernanceHTTPRequest(t *testing.T, server *httptest.Server) AccessRequest {
	t.Helper()
	response := governanceRequest(t, server, http.MethodPost, "/iam/access-requests", "tenant-a", "requester", authn.SubjectTypePrincipal, "en-US", map[string]any{
		"role_id": "role-a", "reason": "temporary operations", "access_expires_at": governanceNow.Add(24 * time.Hour),
	})
	require.Equal(t, http.StatusCreated, response.status, response.Message)
	var request AccessRequest
	require.NoError(t, json.Unmarshal(response.Data, &request))
	return request
}

func TestGovernanceHTTPErrorsAreLocalized(t *testing.T) {
	fixture := newGovernanceFixture(t)
	server, _ := newGovernanceHTTPServer(t, fixture.service)
	for _, item := range []struct {
		locale, expected string
	}{
		{"en-US", "The requested role no longer exists in this tenant. Choose an available role and submit a new request."},
		{"zh-CN", "申请的角色在当前租户中已不存在。请选择可用角色后重新提交申请。"},
		{"ms-MY", "Peranan yang diminta tidak lagi wujud dalam penyewa ini. Pilih peranan yang tersedia dan hantar permintaan baharu."},
	} {
		response := governanceRequest(t, server, http.MethodPost, "/iam/access-requests", "tenant-a", "requester", authn.SubjectTypePrincipal, item.locale, map[string]any{
			"role_id": "missing", "reason": "temporary operations", "access_expires_at": governanceNow.Add(24 * time.Hour),
		})
		assert.Equal(t, http.StatusNotFound, response.status)
		assert.Equal(t, item.expected, response.Message)
		assert.NotContains(t, response.Message, "access_request_")
	}

	serviceAccount := governanceRequest(t, server, http.MethodPost, "/iam/access-requests", "tenant-a", "service", authn.SubjectTypeServiceAccount, "zh-CN", map[string]any{
		"role_id": "role-a", "reason": "temporary operations", "access_expires_at": governanceNow.Add(24 * time.Hour),
	})
	assert.Equal(t, http.StatusForbidden, serviceAccount.status)
	assert.Equal(t, "该操作必须由已认证的人员身份执行，服务账号和 OAuth 客户端不能申请、审批或复核访问权限。", serviceAccount.Message)

	_, err := fixture.db.ExecContext(t.Context(), "DROP TABLE iam_approval_steps")
	require.NoError(t, err)
	_, err = fixture.db.ExecContext(t.Context(), "DROP TABLE iam_access_requests")
	require.NoError(t, err)
	unavailable := governanceRequest(t, server, http.MethodGet, "/iam/my/access-requests", "tenant-a", "requester", authn.SubjectTypePrincipal, "ms-MY", nil)
	assert.Equal(t, http.StatusInternalServerError, unavailable.status)
	assert.Equal(t, "Perkhidmatan tadbir urus akses tidak tersedia buat sementara waktu. Tiada perubahan dilakukan; cuba lagi kemudian.", unavailable.Message)
	queueUnavailable := governanceRequest(t, server, http.MethodGet, "/iam/access-requests", "tenant-a", "approver", authn.SubjectTypePrincipal, "en-US", nil)
	assert.Equal(t, http.StatusInternalServerError, queueUnavailable.status)
	assert.Equal(t, "The access governance service is temporarily unavailable. No governance change was committed; try again later.", queueUnavailable.Message)
}

func TestGovernanceErrorMappingAndHumanPrincipal(t *testing.T) {
	for _, item := range []struct {
		err    error
		status int
	}{
		{ErrNotFound, http.StatusNotFound}, {ErrRoleNotFound, http.StatusNotFound}, {ErrExpired, http.StatusGone},
		{ErrRequesterInactive, http.StatusConflict}, {ErrAlreadyGranted, http.StatusConflict}, {ErrStateConflict, http.StatusConflict},
		{ErrSelfApproval, http.StatusConflict}, {ErrRequesterOnly, http.StatusForbidden}, {ErrInvalid, http.StatusUnprocessableEntity},
		{ErrReviewNotFound, http.StatusNotFound}, {ErrReviewItemNotFound, http.StatusNotFound}, {ErrReviewExpired, http.StatusGone},
		{ErrReviewEmpty, http.StatusConflict}, {ErrReviewStateConflict, http.StatusConflict}, {ErrReviewIncomplete, http.StatusConflict},
		{ErrReviewSelfDecision, http.StatusConflict}, {ErrReviewLastAdministrator, http.StatusConflict}, {ErrInvalidReview, http.StatusUnprocessableEntity},
		{context.Canceled, http.StatusInternalServerError},
	} {
		var statusError huma.StatusError
		require.True(t, errors.As(governanceError(item.err), &statusError))
		assert.Equal(t, item.status, statusError.GetStatus())
	}
	_, err := humanPrincipal(t.Context())
	assert.Error(t, err)
	ctx := authn.WithClaims(t.Context(), &authn.Claims{Subject: "principal", SubjectType: authn.SubjectTypePrincipal})
	principal, err := humanPrincipal(ctx)
	require.NoError(t, err)
	assert.Equal(t, "principal", principal)
}

func newGovernanceHTTPServer(t *testing.T, service *Service) (*httptest.Server, huma.API) {
	t.Helper()
	require.NoError(t, i18n.InitEmbedded(i18n.Base))
	require.NoError(t, RegisterI18n())
	router := chi.NewMux()
	router.Use(respx.Timing)
	router.Use(respx.Locale)
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			claims := &authn.Claims{Subject: request.Header.Get("X-Test-Subject"), SubjectType: request.Header.Get("X-Test-Subject-Type")}
			next.ServeHTTP(writer, request.WithContext(authn.WithClaims(request.Context(), claims)))
		})
	})
	config := huma.DefaultConfig("governance-test", "1.0.0")
	config.Transformers = append(config.Transformers, respx.LocalizeMessage)
	respx.Install()
	api := humachi.New(router, config)
	registrar := authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry())
	RegisterREST(api, service, registrar)
	require.NoError(t, authz.ValidateOperations(api, registrar.Registry()))
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server, api
}

func governanceRequest(t *testing.T, server *httptest.Server, method, path, tenantID, subject, subjectType, locale string, body any) struct {
	governanceEnvelope
	status int
} {
	t.Helper()
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		require.NoError(t, err)
		payload = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(t.Context(), method, server.URL+path, payload)
	require.NoError(t, err)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set(authz.TenantHeader, tenantID)
	request.Header.Set("Accept-Language", locale)
	request.Header.Set("X-Test-Subject", subject)
	request.Header.Set("X-Test-Subject-Type", subjectType)
	response, err := server.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	encoded, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	var envelope governanceEnvelope
	require.NoError(t, json.Unmarshal(encoded, &envelope), "body: %s", encoded)
	return struct {
		governanceEnvelope
		status int
	}{envelope, response.StatusCode}
}
