package organization

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
	"github.com/chaos-plus/chaosplus/pkg/i18n"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type invitationEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func TestInvitationHTTPWorkflowAndOpenAPI(t *testing.T) {
	fixture := newInvitationFixture(t)
	server, api := newInvitationHTTPServer(t, fixture.service)

	create := invitationRequest(t, server, http.MethodPost, "/iam/invitations", "tenant-a", "en-US", map[string]any{"email": "api@example.com", "role_ids": []string{"role-a"}})
	require.Equal(t, http.StatusCreated, create.status, create.message)
	var issued issuedInvitation
	require.NoError(t, json.Unmarshal(create.data, &issued))
	assert.NotEmpty(t, issued.Token)

	listed := invitationRequest(t, server, http.MethodGet, "/iam/invitations", "tenant-a", "en-US", nil)
	require.Equal(t, http.StatusOK, listed.status, listed.message)
	var invitations []Invitation
	require.NoError(t, json.Unmarshal(listed.data, &invitations))
	require.Len(t, invitations, 1)

	resent := invitationRequest(t, server, http.MethodPost, "/iam/invitations/"+issued.Invitation.ID+"/resend", "tenant-a", "en-US", map[string]any{"expires_in_hours": 48})
	require.Equal(t, http.StatusOK, resent.status, resent.message)
	var replacement issuedInvitation
	require.NoError(t, json.Unmarshal(resent.data, &replacement))
	assert.NotEqual(t, issued.Token, replacement.Token)

	accepted := invitationRequest(t, server, http.MethodPost, "/iam/invitations/accept", "", "en-US", map[string]any{"token": replacement.Token, "login_name": "api-user", "password": "correct horse battery staple"})
	require.Equal(t, http.StatusOK, accepted.status, accepted.message)
	var acceptance InvitationAcceptance
	require.NoError(t, json.Unmarshal(accepted.data, &acceptance))
	assert.Equal(t, "api@example.com", acceptance.Email)

	revocable := invitationRequest(t, server, http.MethodPost, "/iam/invitations", "tenant-a", "en-US", map[string]any{"email": "revoke-api@example.com"})
	require.Equal(t, http.StatusCreated, revocable.status, revocable.message)
	require.NoError(t, json.Unmarshal(revocable.data, &issued))
	assert.JSONEq(t, `[]`, string(mustInvitationRoleIDs(t, revocable.data)))
	revoked := invitationRequest(t, server, http.MethodDelete, "/iam/invitations/"+issued.Invitation.ID, "tenant-a", "en-US", nil)
	assert.Equal(t, http.StatusOK, revoked.status, revoked.message)

	operations := map[string]string{
		api.OpenAPI().Paths["/iam/invitations"].Get.OperationID:              "invitation_view",
		api.OpenAPI().Paths["/iam/invitations"].Post.OperationID:             "invitation_create",
		api.OpenAPI().Paths["/iam/invitations/{id}"].Delete.OperationID:      "invitation_revoke",
		api.OpenAPI().Paths["/iam/invitations/{id}/resend"].Post.OperationID: "invitation_resend",
		api.OpenAPI().Paths["/iam/invitations/accept"].Post.OperationID:      "",
	}
	assert.Equal(t, map[string]string{
		"organization-list-invitations":  "invitation_view",
		"organization-create-invitation": "invitation_create",
		"organization-revoke-invitation": "invitation_revoke",
		"organization-resend-invitation": "invitation_resend",
		"organization-accept-invitation": "",
	}, operations)
	assert.Empty(t, api.OpenAPI().Paths["/iam/invitations/accept"].Post.Security)
}

func mustInvitationRoleIDs(t *testing.T, data json.RawMessage) json.RawMessage {
	t.Helper()
	var value struct {
		Invitation struct {
			RoleIDs json.RawMessage `json:"role_ids"`
		} `json:"invitation"`
	}
	require.NoError(t, json.Unmarshal(data, &value))
	return value.Invitation.RoleIDs
}

func TestInvitationHTTPErrorsAreLocalized(t *testing.T) {
	fixture := newInvitationFixture(t)
	server, _ := newInvitationHTTPServer(t, fixture.service)
	token := "cpi1_missing." + "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	for _, item := range []struct {
		locale  string
		message string
	}{
		{"en-US", "The invitation credential is invalid, revoked, or already replaced. Use the latest invitation link or ask an administrator to resend it."},
		{"zh-CN", "邀请凭据无效、已撤销或已被新凭据替换，请使用最新邀请链接或联系管理员重新发送。"},
		{"ms-MY", "Kelayakan jemputan tidak sah, telah dibatalkan atau digantikan. Gunakan pautan terkini atau minta pentadbir menghantarnya semula."},
	} {
		response := invitationRequest(t, server, http.MethodPost, "/iam/invitations/accept", "", item.locale, map[string]any{"token": token, "login_name": "missing", "password": "correct horse battery staple"})
		assert.Equal(t, http.StatusBadRequest, response.status)
		assert.Equal(t, item.message, response.message)
	}
}

func TestInvitationErrorMapping(t *testing.T) {
	for _, item := range []struct {
		err    error
		status int
	}{
		{ErrInvitationNotFound, http.StatusNotFound},
		{ErrInvitationCredential, http.StatusBadRequest},
		{ErrInvitationExpired, http.StatusGone},
		{ErrInvitationState, http.StatusConflict},
		{ErrInvitationLoginConflict, http.StatusConflict},
		{ErrInvitationBindingMissing, http.StatusNotFound},
		{ErrInvitationBindingInactive, http.StatusConflict},
		{ErrInvitationInvalid, http.StatusUnprocessableEntity},
		{context.Canceled, http.StatusInternalServerError},
	} {
		statusError, ok := invitationError(item.err).(huma.StatusError)
		require.True(t, ok)
		assert.Equal(t, item.status, statusError.GetStatus())
	}
}

func newInvitationHTTPServer(t *testing.T, service *InvitationService) (*httptest.Server, huma.API) {
	t.Helper()
	require.NoError(t, i18n.InitEmbedded(i18n.Base))
	require.NoError(t, RegisterI18n())
	router := chi.NewMux()
	router.Use(respx.Timing)
	router.Use(respx.Locale)
	config := huma.DefaultConfig("invitation-test", "1.0.0")
	config.Transformers = append(config.Transformers, respx.LocalizeMessage)
	respx.Install()
	api := humachi.New(router, config)
	registrar := authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry())
	RegisterInvitationREST(api, service, registrar)
	require.NoError(t, authz.ValidateOperations(api, registrar.Registry()))
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server, api
}

func invitationRequest(t *testing.T, server *httptest.Server, method, path, tenantID, locale string, body any) struct {
	status  int
	message string
	data    json.RawMessage
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
	if tenantID != "" {
		request.Header.Set(authz.TenantHeader, tenantID)
	}
	request.Header.Set("Accept-Language", locale)
	response, err := server.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	encoded, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	var envelope invitationEnvelope
	require.NoError(t, json.Unmarshal(encoded, &envelope), "body: %s", encoded)
	return struct {
		status  int
		message string
		data    json.RawMessage
	}{response.StatusCode, envelope.Message, envelope.Data}
}
