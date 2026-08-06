package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/pkg/i18n"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOAuthOpenAPIContract(t *testing.T) {
	service, _, _ := newOAuthTestService(t)
	_, api := humatest.New(t)
	RegisterREST(api, service, nil)

	doc := api.OpenAPI()
	require.Contains(t, doc.Components.SecuritySchemes, authz.BearerScheme)
	require.Contains(t, doc.Components.SecuritySchemes, authz.SessionScheme)
	require.Contains(t, doc.Components.SecuritySchemes, authz.ClientBasicScheme)

	authorize := doc.Paths["/oauth/authorize"].Get
	require.Contains(t, authorize.Responses, "302")
	for _, name := range []string{"client_id", "redirect_uri", "response_type", "code_challenge", "code_challenge_method"} {
		assert.True(t, requiredParameter(authorize, name), name)
	}

	token := doc.Paths["/oauth/token"].Post
	form := token.RequestBody.Content["application/x-www-form-urlencoded"].Schema
	require.NotNil(t, form)
	assert.Equal(t, "object", form.Type)
	assert.Contains(t, form.Properties, "grant_type")
	assert.Contains(t, form.Properties, "code_verifier")
	assert.Contains(t, form.Required, "grant_type")
	assert.Contains(t, token.Responses, "400")
	assert.Contains(t, token.Security[0], authz.ClientBasicScheme)

	for _, path := range []string{"/oauth/revoke", "/oauth/introspect"} {
		schema := doc.Paths[path].Post.RequestBody.Content["application/x-www-form-urlencoded"].Schema
		assert.Equal(t, "object", schema.Type)
		assert.Contains(t, schema.Required, "token")
	}
	assert.Contains(t, doc.Paths["/oauth/userinfo"].Get.Security[0], authz.BearerScheme)
}

func TestOAuthErrorsUseProtocolBody(t *testing.T) {
	service, _, _ := newOAuthTestService(t)
	_, api := humatest.New(t)
	RegisterREST(api, service, nil)

	response := api.Post("/oauth/token", "Content-Type: application/x-www-form-urlencoded", strings.NewReader("grant_type=authorization_code&client_id=missing"))
	assert.Equal(t, http.StatusBadRequest, response.Code)
	assert.Equal(t, "application/json", response.Header().Get("Content-Type"))
	var body map[string]any
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	assert.Equal(t, "invalid_grant", body["error"])
	assert.NotContains(t, body, "code")
	assert.NotContains(t, body, "meta")
}

func TestOAuthHTTPAuthorizationCodeLifecycle(t *testing.T) {
	service, authentication, _ := newOAuthTestService(t)
	client, _, err := service.CreateClient(t.Context(), "tenant", "Browser", []string{"https://client.example/callback"}, []string{"authorization_code", "refresh_token"}, []string{"openid", "profile"}, true)
	require.NoError(t, err)
	session, _, err := authentication.Login(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)

	_, api := humatest.New(t)
	RegisterREST(api, service, nil)
	assert.Equal(t, http.StatusOK, api.Get("/.well-known/openid-configuration").Code)
	assert.Equal(t, http.StatusOK, api.Get("/.well-known/jwks.json").Code)

	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"
	digest := sha256.Sum256([]byte(verifier))
	query := url.Values{
		"client_id": {client.ID}, "redirect_uri": {"https://client.example/callback"},
		"response_type": {"code"}, "scope": {"openid profile"}, "state": {"state-value"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])}, "code_challenge_method": {"S256"}, "nonce": {"nonce-value"},
	}
	authorized := api.Get("/oauth/authorize?"+query.Encode(), "Cookie: "+authentication.SessionCookie(session))
	require.Equal(t, http.StatusFound, authorized.Code, authorized.Body.String())
	location, err := url.Parse(authorized.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "state-value", location.Query().Get("state"))

	form := url.Values{
		"grant_type": {"authorization_code"}, "client_id": {client.ID}, "code": {location.Query().Get("code")},
		"redirect_uri": {"https://client.example/callback"}, "code_verifier": {verifier},
	}
	tokenResponse := api.Post("/oauth/token", "Content-Type: application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	require.Equal(t, http.StatusOK, tokenResponse.Code, tokenResponse.Body.String())
	var tokens TokenResponse
	require.NoError(t, json.Unmarshal(tokenResponse.Body.Bytes(), &tokens))
	require.NotEmpty(t, tokens.AccessToken)
	require.NotEmpty(t, tokens.RefreshToken)
	require.NotEmpty(t, tokens.IDToken)
	assert.Equal(t, http.StatusOK, api.Get("/oauth/userinfo", "Authorization: Bearer "+tokens.AccessToken).Code)

	resource, secret, err := service.CreateClient(t.Context(), "tenant", "Resource Server", nil, []string{"client_credentials"}, []string{"introspect"}, false)
	require.NoError(t, err)
	basic := base64.StdEncoding.EncodeToString([]byte(resource.ID + ":" + secret))
	introspection := api.Post("/oauth/introspect", "Authorization: Basic "+basic, "Content-Type: application/x-www-form-urlencoded", strings.NewReader(url.Values{"token": {tokens.AccessToken}}.Encode()))
	require.Equal(t, http.StatusOK, introspection.Code, introspection.Body.String())
	assert.Contains(t, introspection.Body.String(), `"active":true`)

	revoked := api.Post("/oauth/revoke", "Content-Type: application/x-www-form-urlencoded", strings.NewReader(url.Values{"client_id": {client.ID}, "token": {tokens.RefreshToken}}.Encode()))
	assert.Equal(t, http.StatusOK, revoked.Code, revoked.Body.String())
	refresh := api.Post("/oauth/token", "Content-Type: application/x-www-form-urlencoded", strings.NewReader(url.Values{
		"grant_type": {"refresh_token"}, "client_id": {client.ID}, "refresh_token": {tokens.RefreshToken},
	}.Encode()))
	assert.Equal(t, http.StatusBadRequest, refresh.Code)
	assert.Equal(t, http.StatusUnauthorized, api.Get("/oauth/userinfo", "Authorization: Bearer invalid").Code)
}

func TestOAuthClientManagementHTTPWorkflow(t *testing.T) {
	service, _, _ := newOAuthTestService(t)
	_, api := humatest.New(t)
	RegisterREST(api, service, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	tenant := authz.TenantHeader + ": tenant"

	created := api.Post("/iam/oauth-clients", tenant, map[string]any{
		"name": "Worker", "grant_types": []string{"client_credentials"},
		"scopes": []string{"jobs.read"}, "public_client": false,
	})
	require.Equal(t, http.StatusOK, created.Code, created.Body.String())
	var envelope struct {
		Data clientCreateData `json:"data"`
	}
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &envelope))
	require.NotEmpty(t, envelope.Data.Client.ID)
	require.NotEmpty(t, envelope.Data.ClientSecret)
	id := envelope.Data.Client.ID

	assert.Equal(t, http.StatusOK, api.Get("/iam/oauth-clients", tenant).Code)
	assert.Equal(t, http.StatusOK, api.Put("/iam/oauth-clients/"+id, tenant, map[string]any{
		"name": "Worker 2", "grant_types": []string{"client_credentials"},
		"scopes": []string{"jobs.read", "jobs.write"}, "public_client": false, "status": "active",
	}).Code)
	assert.Equal(t, http.StatusOK, api.Post("/iam/oauth-clients/"+id+"/rotate-secret", tenant).Code)
	assert.Equal(t, http.StatusOK, api.Delete("/iam/oauth-clients/"+id, tenant).Code)
	assert.Equal(t, http.StatusNotFound, api.Delete("/iam/oauth-clients/"+id, tenant).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Post("/iam/oauth-clients", tenant, map[string]any{
		"name": "", "grant_types": []string{"client_credentials"}, "scopes": []string{"jobs.read"},
	}).Code)
}

func TestProtocolErrorContract(t *testing.T) {
	require.NoError(t, i18n.InitEmbedded(i18n.Base))
	require.NoError(t, RegisterI18n())
	err := oauthError(i18n.WithLocale(context.Background(), "zh-CN"), http.StatusUnauthorized, "invalid_client")
	assert.Equal(t, "invalid_client", err.Error())
	var protocol *protocolError
	require.True(t, errors.As(err, &protocol))
	assert.Equal(t, http.StatusUnauthorized, protocol.GetStatus())
	assert.Equal(t, "OAuth 客户端认证失败，请核对客户端标识符和凭据后重试。", protocol.Description)
	assert.Equal(t, "401", httpStatus(http.StatusUnauthorized))
}

func TestOAuthManagementDatabaseFailures(t *testing.T) {
	service, _, _ := newOAuthTestService(t)
	_, api := humatest.New(t)
	RegisterREST(api, service, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	tenant := authz.TenantHeader + ": tenant"
	require.NoError(t, service.db.Close())

	assert.Equal(t, http.StatusUnprocessableEntity, api.Get("/iam/oauth-clients", tenant).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Post("/iam/oauth-clients", tenant, map[string]any{
		"name": "Worker", "grant_types": []string{"client_credentials"}, "scopes": []string{"jobs.read"}, "public_client": false,
	}).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Put("/iam/oauth-clients/client", tenant, map[string]any{
		"name": "Worker", "grant_types": []string{"client_credentials"}, "scopes": []string{"jobs.read"}, "public_client": false, "status": "active",
	}).Code)
	assert.Equal(t, http.StatusNotFound, api.Post("/iam/oauth-clients/client/rotate-secret", tenant).Code)
	assert.Equal(t, http.StatusNotFound, api.Delete("/iam/oauth-clients/client", tenant).Code)
}

func TestOAuthHTTPProtocolFailureBranches(t *testing.T) {
	service, authentication, _ := newOAuthTestService(t)
	client, _, err := service.CreateClient(t.Context(), "tenant", "Browser", []string{"https://client.example/callback"}, []string{"authorization_code"}, []string{"openid"}, true)
	require.NoError(t, err)
	session, _, err := authentication.Login(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	_, api := humatest.New(t)
	RegisterREST(api, service, nil)
	challenge := strings.Repeat("a", 43)
	query := url.Values{
		"client_id": {"missing"}, "redirect_uri": {client.RedirectURIs[0]}, "response_type": {"code"},
		"scope": {"openid"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"},
	}
	assert.Equal(t, http.StatusBadRequest, api.Get("/oauth/authorize?"+query.Encode(), "Cookie: "+authentication.SessionCookie(session)).Code)
	assert.Equal(t, http.StatusBadRequest, api.Post("/oauth/token", "Content-Type: application/x-www-form-urlencoded", strings.NewReader("%")).Code)
	assert.Equal(t, http.StatusUnauthorized, api.Post("/oauth/revoke", "Content-Type: application/x-www-form-urlencoded", strings.NewReader("token=value&client_id=missing")).Code)
	assert.Equal(t, http.StatusUnauthorized, api.Post("/oauth/introspect", "Content-Type: application/x-www-form-urlencoded", strings.NewReader("token=value&client_id=missing")).Code)
}

func TestOAuthHTTPTokenServerFailure(t *testing.T) {
	service, authentication, _ := newOAuthTestService(t)
	client, _, err := service.CreateClient(t.Context(), "tenant", "Browser", []string{"https://client.example/callback"}, []string{"authorization_code"}, []string{"openid"}, true)
	require.NoError(t, err)
	session, _, err := authentication.Login(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"
	digest := sha256.Sum256([]byte(verifier))
	redirect, err := service.Authorize(t.Context(), authentication.SessionCookie(session), client.ID, client.RedirectURIs[0], "code", "openid", "", base64.RawURLEncoding.EncodeToString(digest[:]), "S256", "")
	require.NoError(t, err)
	location, err := url.Parse(redirect)
	require.NoError(t, err)
	_, err = service.db.ExecContext(t.Context(), `CREATE TRIGGER reject_oauth_code_consumption
		BEFORE UPDATE ON iam_oauth_codes BEGIN SELECT RAISE(ABORT, 'token storage unavailable'); END`)
	require.NoError(t, err)

	_, api := humatest.New(t)
	RegisterREST(api, service, nil)
	form := url.Values{
		"grant_type": {"authorization_code"}, "client_id": {client.ID}, "code": {location.Query().Get("code")},
		"redirect_uri": {client.RedirectURIs[0]}, "code_verifier": {verifier},
	}
	assert.Equal(t, http.StatusInternalServerError, api.Post("/oauth/token", "Content-Type: application/x-www-form-urlencoded", strings.NewReader(form.Encode())).Code)
}

func requiredParameter(operation *huma.Operation, name string) bool {
	for _, parameter := range operation.Parameters {
		if parameter.In == "query" && parameter.Name == name {
			return parameter.Required
		}
	}
	return false
}
