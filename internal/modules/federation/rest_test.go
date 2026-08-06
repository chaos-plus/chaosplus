package federation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

type federationEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func localized(locale, key string) string {
	return i18n.TContext(i18n.WithLocale(context.Background(), locale), key)
}

func newManagementAPI(t *testing.T, env *federationEnvironment) (*http.Client, *httptest.Server) {
	t.Helper()
	require.NoError(t, i18n.InitEmbedded(i18n.Base))
	require.NoError(t, RegisterI18n())
	respx.Install()
	router := chi.NewRouter()
	router.Use(respx.Timing, respx.Locale)
	config := huma.DefaultConfig("federation test", "1.0.0")
	config.Transformers = append(config.Transformers, respx.LocalizeMessage)
	api := humachi.New(router, config)
	RegisterAdminREST(api, env.service, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server.Client(), server
}

func adminRequest(t *testing.T, client *http.Client, method, target, tenantID string, body []byte, headers map[string]string) *http.Response {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, target, bytes.NewReader(body))
	require.NoError(t, err)
	request.Header.Set(authz.TenantHeader, tenantID)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := client.Do(request)
	require.NoError(t, err)
	return response
}

func decodeHTTPBody(t *testing.T, response *http.Response, target any) {
	t.Helper()
	require.NoError(t, json.NewDecoder(response.Body).Decode(target))
	require.NoError(t, response.Body.Close())
}

func TestFederationManagementHTTPWorkflow(t *testing.T) {
	env := newFederationEnvironment(t)
	insertRole(t, env.db, "tenant-a", "role-a")
	client, server := newManagementAPI(t, env)

	create := []byte(`{"name":"GitLab","provider_type":"oidc","issuer":"https://gitlab.example","client_id":"app","client_secret":"s3cret","auto_provision":true,"default_role_id":"role-a","status":"active"}`)
	response := adminRequest(t, client, http.MethodPost, server.URL+"/iam/identity-providers", "tenant-a", create, nil)
	assert.Equal(t, http.StatusCreated, response.StatusCode)
	var envelope federationEnvelope
	decodeHTTPBody(t, response, &envelope)
	t.Logf("CREATE RESPONSE code=%d message=%q data=%s", envelope.Code, envelope.Message, string(envelope.Data))
	assert.Zero(t, envelope.Code)
	var provider Provider
	require.NoError(t, json.Unmarshal(envelope.Data, &provider))
	assert.Equal(t, "GitLab", provider.Name)
	assert.Equal(t, "https://gitlab.example", provider.Issuer)
	assert.True(t, provider.ClientSecretSet)

	response = adminRequest(t, client, http.MethodGet, server.URL+"/iam/identity-providers", "tenant-a", nil, nil)
	assert.Equal(t, http.StatusOK, response.StatusCode)
	decodeHTTPBody(t, response, &envelope)
	var providers []Provider
	require.NoError(t, json.Unmarshal(envelope.Data, &providers))
	require.Len(t, providers, 1)

	response = adminRequest(t, client, http.MethodPost, server.URL+"/iam/identity-providers", "tenant-a", create, map[string]string{"Accept-Language": "zh-CN"})
	assert.Equal(t, http.StatusConflict, response.StatusCode)
	decodeHTTPBody(t, response, &envelope)
	assert.Equal(t, localized("zh-CN", "federation_provider_issuer_exists"), envelope.Message)
	assert.NotEqual(t, "federation_provider_issuer_exists", envelope.Message)

	response = adminRequest(t, client, http.MethodPost, server.URL+"/iam/identity-providers", "tenant-a", []byte(`{"name":"Broken","issuer":"https://broken.example","client_id":"app","auto_provision":true,"default_role_id":"missing","status":"active"}`), nil)
	assert.Equal(t, http.StatusConflict, response.StatusCode)
	decodeHTTPBody(t, response, &envelope)
	assert.Equal(t, localized("en-US", "federation_provider_role_missing"), envelope.Message)

	response = adminRequest(t, client, http.MethodPost, server.URL+"/iam/identity-providers", "tenant-a", []byte(`{"name":"Bad","issuer":"not a url","client_id":"app","auto_provision":true,"status":"active"}`), nil)
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	decodeHTTPBody(t, response, &envelope)
	assert.Equal(t, localized("en-US", "federation_invalid_request"), envelope.Message)

	response = adminRequest(t, client, http.MethodPut, server.URL+"/iam/identity-providers/"+provider.ID, "tenant-a", []byte(`{"name":"GitLab Renamed","provider_type":"oidc","issuer":"https://gitlab.example","client_id":"app","auto_provision":true,"default_role_id":"role-a","status":"active"}`), nil)
	assert.Equal(t, http.StatusOK, response.StatusCode)
	decodeHTTPBody(t, response, &envelope)
	require.NoError(t, json.Unmarshal(envelope.Data, &provider))
	assert.Equal(t, "GitLab Renamed", provider.Name)

	response = adminRequest(t, client, http.MethodPut, server.URL+"/iam/identity-providers/missing", "tenant-a", []byte(`{"name":"X","issuer":"https://other.example","client_id":"app","auto_provision":true,"status":"active"}`), nil)
	assert.Equal(t, http.StatusNotFound, response.StatusCode)

	response = adminRequest(t, client, http.MethodDelete, server.URL+"/iam/identity-providers/"+provider.ID, "tenant-a", nil, nil)
	assert.Equal(t, http.StatusOK, response.StatusCode)
	decodeHTTPBody(t, response, &envelope)
	assert.Contains(t, string(envelope.Data), `"deleted":true`)

	response = adminRequest(t, client, http.MethodGet, server.URL+"/iam/identity-providers", "tenant-a", nil, nil)
	assert.Equal(t, http.StatusOK, response.StatusCode)
	decodeHTTPBody(t, response, &envelope)
	require.NoError(t, json.Unmarshal(envelope.Data, &providers))
	assert.Empty(t, providers)

	response = adminRequest(t, client, http.MethodDelete, server.URL+"/iam/identity-providers/"+provider.ID, "tenant-a", nil, nil)
	assert.Equal(t, http.StatusNotFound, response.StatusCode)
}

func TestFederationBrowserLoginHTTPFlow(t *testing.T) {
	env := newFederationEnvironment(t)
	provider := env.createProvider(t, env.providerInput("GitLab", env.idp.issuer))
	require.NoError(t, i18n.InitEmbedded(i18n.Base))
	require.NoError(t, RegisterI18n())
	respx.Install()
	router := chi.NewRouter()
	config := huma.DefaultConfig("browser test", "1.0.0")
	config.Transformers = append(config.Transformers, respx.LocalizeMessage)
	api := humachi.New(router, config)
	RegisterBrowserREST(api, env.service)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	startURL := server.URL + "/federation/" + provider.ID + "/start?return_url=" + url.QueryEscape("https://app.example/")
	response, err := client.Get(startURL)
	require.NoError(t, err)
	require.Equal(t, http.StatusFound, response.StatusCode)
	location := response.Header.Get("Location")
	parsedLocation, err := url.Parse(location)
	require.NoError(t, err)
	assert.Equal(t, env.idp.issuer+"/authorize", parsedLocation.Scheme+"://"+parsedLocation.Host+parsedLocation.Path)
	assert.Equal(t, server.URL+"/federation/"+provider.ID+"/callback", parsedLocation.Query().Get("redirect_uri"))
	var stateCookie *http.Cookie
	for _, cookie := range response.Cookies() {
		if cookie.Name == stateCookieName {
			stateCookie = cookie
		}
	}
	require.NotNil(t, stateCookie)
	require.NoError(t, response.Body.Close())

	state, err := openState(env.key, stateCookie.Value)
	require.NoError(t, err)
	assert.Equal(t, "https://app.example/", state.ReturnURL)

	// A reverse-proxy HTTPS prefix flows through to the IdP redirect_uri.
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, startURL, nil)
	require.NoError(t, err)
	request.Header.Set("X-Forwarded-Proto", "https")
	response, err = client.Do(request)
	require.NoError(t, err)
	require.Equal(t, http.StatusFound, response.StatusCode)
	parsedLocation, err = url.Parse(response.Header.Get("Location"))
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(parsedLocation.Query().Get("redirect_uri"), "https://"+server.Listener.Addr().String()))
	require.NoError(t, response.Body.Close())

	code, returnedState := env.idp.authorize(t, client, provider.Issuer, state.CodeVerifier, state.Nonce)
	require.Equal(t, state.Nonce, returnedState)
	callbackURL := server.URL + "/federation/" + provider.ID + "/callback?code=" + code + "&state=" + state.Nonce
	request, err = http.NewRequestWithContext(t.Context(), http.MethodGet, callbackURL, nil)
	require.NoError(t, err)
	request.Header.Set("Cookie", stateCookieName+"="+stateCookie.Value)
	response, err = client.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusFound, response.StatusCode)
	assert.Equal(t, "https://app.example/", response.Header.Get("Location"))
	var session *http.Cookie
	for _, cookie := range response.Cookies() {
		if cookie.Name == env.web.SessionCookieName() {
			session = cookie
		}
	}
	require.NotNil(t, session)
	claims, err := env.web.Authenticate(t.Context(), "", env.web.SessionCookie(session.Value))
	require.NoError(t, err)
	assert.Equal(t, "user@example.com", claims.Email)

	// Replayed or forged state is rejected with a localized 400.
	response, err = client.Get(server.URL + "/federation/" + provider.ID + "/callback?code=garbage&state=wrong")
	require.NoError(t, err)
	defer response.Body.Close()
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
}

func TestFederationErrorMapping(t *testing.T) {
	for err, status := range map[error]int{
		ErrInvalidProvider:        http.StatusBadRequest,
		ErrProviderNotFound:       http.StatusNotFound,
		ErrProviderIssuerExists:   http.StatusConflict,
		ErrProviderRoleMissing:    http.StatusConflict,
		ErrProviderDisabled:       http.StatusForbidden,
		ErrProvisioningDisabled:   http.StatusForbidden,
		ErrPrincipalInactive:      http.StatusForbidden,
		ErrProvisioningUnverified: http.StatusBadRequest,
		ErrOIDCDiscovery:          http.StatusBadRequest,
		ErrOIDCToken:              http.StatusBadRequest,
		ErrOIDCTokenInvalid:       http.StatusBadRequest,
		ErrOIDCState:              http.StatusBadRequest,
	} {
		mapped := federationError(err)
		var statusError huma.StatusError
		require.True(t, errors.As(mapped, &statusError))
		assert.Equal(t, status, statusError.GetStatus())
	}
	var statusError huma.StatusError
	require.True(t, errors.As(federationError(assert.AnError), &statusError))
	assert.Equal(t, http.StatusInternalServerError, statusError.GetStatus())
}

func TestProviderInputFromBody(t *testing.T) {
	body := providerBody{Name: "X", ProviderType: ProviderOIDC, Issuer: "https://x.example", ClientID: "c", ClientSecret: "s", Scopes: "openid", AutoProvision: true, DefaultRoleID: "r", Status: ProviderActive}
	input := providerInputFromBody(body)
	assert.Equal(t, ProviderInput{Name: "X", ProviderType: ProviderOIDC, Issuer: "https://x.example", ClientID: "c", ClientSecret: "s", Scopes: "openid", AutoProvision: true, DefaultRoleID: "r", Status: ProviderActive}, input)
}
