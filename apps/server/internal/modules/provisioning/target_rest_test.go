package provisioning

import (
	"encoding/json"
	"fmt"
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

func TestTargetManagementHTTPWorkflow(t *testing.T) {
	env := newProvisioningEnvironment(t)
	require.NoError(t, i18n.InitEmbedded(i18n.Base))
	require.NoError(t, RegisterI18n())
	respx.Install()
	router := chi.NewRouter()
	router.Use(respx.Timing, respx.Locale)
	config := huma.DefaultConfig("target test", "1.0.0")
	config.Transformers = append(config.Transformers, respx.LocalizeMessage)
	api := humachi.New(router, config)
	RegisterTargetREST(api, env.service, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	createBody := []byte(fmt.Sprintf(`{"name":"Okta","base_url":"%s","bearer_token":"super-secret"}`, server.URL))
	response := adminRequest(t, server.Client(), http.MethodPost, server.URL+"/iam/scim/targets", wireID("tenant-a"), createBody, nil)
	assert.Equal(t, http.StatusCreated, response.StatusCode)
	var envelope adminEnvelope
	decodeHTTPBody(t, response, &envelope)
	assert.Zero(t, envelope.Code)
	var issued TargetSecret
	require.NoError(t, json.Unmarshal(envelope.Data, &issued))
	assert.NotEmpty(t, issued.BearerToken)
	assert.Equal(t, "super-secret", issued.BearerToken)

	// The list response never exposes the bearer token again.
	response = adminRequest(t, server.Client(), http.MethodGet, server.URL+"/iam/scim/targets", wireID("tenant-a"), nil, nil)
	assert.Equal(t, http.StatusOK, response.StatusCode)
	decodeHTTPBody(t, response, &envelope)
	assert.NotContains(t, string(envelope.Data), "super-secret")
	var targets []Target
	require.NoError(t, json.Unmarshal(envelope.Data, &targets))
	require.Len(t, targets, 1)
	assert.Equal(t, "Okta", targets[0].Name)

	// Replace with a stale version is rejected.
	stale := []byte(fmt.Sprintf(`{"name":"Okta Prod","base_url":"%s","status":"active","version":9}`, server.URL))
	response = adminRequest(t, server.Client(), http.MethodPut, server.URL+"/iam/scim/targets/"+issued.Target.ID, wireID("tenant-a"), stale, nil)
	assert.Equal(t, http.StatusConflict, response.StatusCode)
	decodeHTTPBody(t, response, &envelope)
	assert.Equal(t, localized("en-US", "scim_target_version_conflict"), envelope.Message)

	replaceBody := []byte(fmt.Sprintf(`{"name":"Okta Prod","base_url":"%s","status":"active","version":%d}`, server.URL, issued.Target.Version))
	response = adminRequest(t, server.Client(), http.MethodPut, server.URL+"/iam/scim/targets/"+issued.Target.ID, wireID("tenant-a"), replaceBody, nil)
	assert.Equal(t, http.StatusOK, response.StatusCode)
	decodeHTTPBody(t, response, &envelope)
	var replaced Target
	require.NoError(t, json.Unmarshal(envelope.Data, &replaced))
	assert.Equal(t, "Okta Prod", replaced.Name)
	assert.Equal(t, int64(2), replaced.Version)

	// Create a local user and push it through the API against a real HTTP
	// SCIM provider endpoint.
	provider, providerServer := newSCIMProviderEndpoint(t, http.StatusOK, `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"id":"remote-api-user","userName":"api-alice"}`)
	pushTarget, err := env.service.CreateTarget(t.Context(), testID("tenant-a"), "Okta API", providerServer.URL, "api-token")
	require.NoError(t, err)
	user, err := env.service.CreateUser(t.Context(), env.auth, activeUserInput("ext-api-alice", "api-alice", "api-alice@example.test"))
	require.NoError(t, err)

	pushBody := []byte(fmt.Sprintf(`{"resource_type":"User","resource_id":"%s"}`, user.ID))
	response = adminRequest(t, server.Client(), http.MethodPost, server.URL+"/iam/scim/targets/"+pushTarget.Target.ID+"/push", wireID("tenant-a"), pushBody, nil)
	assert.Equal(t, http.StatusOK, response.StatusCode)
	decodeHTTPBody(t, response, &envelope)
	var pushed PushResult
	require.NoError(t, json.Unmarshal(envelope.Data, &pushed))
	assert.Equal(t, "remote-api-user", pushed.RemoteID)
	assert.Equal(t, "Bearer api-token", provider.last(t).Headers.Get("Authorization"))

	// An unreachable remote surfaces as 502 with a localized message.
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	deadTarget, err := env.service.CreateTarget(t.Context(), testID("tenant-a"), "Dead", deadURL, "dead-token")
	require.NoError(t, err)
	response = adminRequest(t, server.Client(), http.MethodPost, server.URL+"/iam/scim/targets/"+deadTarget.Target.ID+"/push", wireID("tenant-a"), pushBody, map[string]string{"Accept-Language": "ms-MY"})
	assert.Equal(t, http.StatusBadGateway, response.StatusCode)
	decodeHTTPBody(t, response, &envelope)
	assert.Equal(t, localized("ms-MY", "scim_target_remote_failed"), envelope.Message)

	// Deprovision the mapped user through the API.
	response = adminRequest(t, server.Client(), http.MethodPost, server.URL+"/iam/scim/targets/"+pushTarget.Target.ID+"/deprovision", wireID("tenant-a"), pushBody, nil)
	assert.Equal(t, http.StatusOK, response.StatusCode)
	assert.Equal(t, http.MethodDelete, provider.last(t).Method)

	// Invalid payload is a 422.
	response = adminRequest(t, server.Client(), http.MethodPost, server.URL+"/iam/scim/targets/"+pushTarget.Target.ID+"/push", wireID("tenant-a"), []byte(`{"resource_type":"Widget","resource_id":"x"}`), nil)
	assert.Equal(t, http.StatusUnprocessableEntity, response.StatusCode)

	// Delete the target and confirm it is gone.
	response = adminRequest(t, server.Client(), http.MethodDelete, server.URL+"/iam/scim/targets/"+issued.Target.ID, wireID("tenant-a"), nil, nil)
	assert.Equal(t, http.StatusOK, response.StatusCode)
	response = adminRequest(t, server.Client(), http.MethodGet, server.URL+"/iam/scim/targets", wireID("tenant-a"), nil, nil)
	decodeHTTPBody(t, response, &envelope)
	var remaining []Target
	require.NoError(t, json.Unmarshal(envelope.Data, &remaining))
	assert.Len(t, remaining, 2)
}
