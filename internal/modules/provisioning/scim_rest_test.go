package provisioning

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
	"github.com/chaos-plus/chaosplus/pkg/i18n"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSCIMNativeHTTPContract(t *testing.T) {
	env := newProvisioningEnvironment(t)
	require.NoError(t, i18n.InitEmbedded(i18n.Base))
	require.NoError(t, RegisterI18n())
	router := chi.NewRouter()
	router.Use(respx.Locale)
	api := humachi.New(router, huma.DefaultConfig("SCIM test", "1.0.0"))
	RegisterSCIMREST(api, env.service)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	response := scimRequest(t, server.Client(), http.MethodGet, server.URL+"/scim/v2/ServiceProviderConfig", "", nil, nil)
	assert.Equal(t, http.StatusOK, response.StatusCode)
	assert.Equal(t, SCIMContentType, response.Header.Get("Content-Type"))
	var config ServiceProviderConfig
	decodeHTTPBody(t, response, &config)
	assert.True(t, config.Patch.Supported)
	assert.True(t, config.Bulk.Supported)

	response = scimRequest(t, server.Client(), http.MethodGet, server.URL+"/scim/v2/Users", "", nil, map[string]string{"Accept-Language": "ms-MY"})
	assert.Equal(t, http.StatusUnauthorized, response.StatusCode)
	assert.Equal(t, `Bearer realm="SCIM"`, response.Header.Get("WWW-Authenticate"))
	var protocolError SCIMError
	decodeHTTPBody(t, response, &protocolError)
	assert.Equal(t, localized("ms-MY", "scim_unauthorized"), protocolError.Detail)
	assert.NotEqual(t, "scim_unauthorized", protocolError.Detail)

	input := activeUserInput("http-alice", "http-alice", "http-alice@example.test")
	body, err := json.Marshal(input)
	require.NoError(t, err)
	response = scimRequest(t, server.Client(), http.MethodPost, server.URL+"/scim/v2/Users", env.token, body, map[string]string{"Accept-Language": "zh-CN"})
	assert.Equal(t, http.StatusCreated, response.StatusCode)
	assert.Equal(t, weakETag(1), response.Header.Get("ETag"))
	data, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	assert.NotContains(t, string(data), `"code"`)
	var user UserResource
	require.NoError(t, json.Unmarshal(data, &user))
	assert.Equal(t, "http-alice", user.UserName)

	response = scimRequest(t, server.Client(), http.MethodGet, server.URL+user.Meta.Location, env.token, nil, nil)
	assert.Equal(t, http.StatusOK, response.StatusCode)
	assert.Equal(t, weakETag(1), response.Header.Get("ETag"))
	decodeHTTPBody(t, response, &user)

	input.DisplayName = "HTTP Alice Replaced"
	body, err = json.Marshal(input)
	require.NoError(t, err)
	response = scimRequest(t, server.Client(), http.MethodPut, server.URL+user.Meta.Location, env.token, body, map[string]string{"If-Match": weakETag(1)})
	assert.Equal(t, http.StatusOK, response.StatusCode)
	decodeHTTPBody(t, response, &user)
	assert.Equal(t, "HTTP Alice Replaced", user.DisplayName)
	assert.Equal(t, weakETag(2), user.Meta.Version)

	patchBody := []byte(`{"schemas":["` + PatchSchema + `"],"Operations":[{"op":"replace","path":"displayName","value":"HTTP Alice Patched"}]}`)
	response = scimRequest(t, server.Client(), http.MethodPatch, server.URL+user.Meta.Location, env.token, patchBody, map[string]string{"If-Match": weakETag(2)})
	assert.Equal(t, http.StatusOK, response.StatusCode)
	decodeHTTPBody(t, response, &user)
	assert.Equal(t, "HTTP Alice Patched", user.DisplayName)

	groupBody, err := json.Marshal(GroupInput{Schemas: []string{GroupSchema}, ExternalID: "http-group", DisplayName: "HTTP Group", Members: []SCIMGroupMember{{Value: user.ID}}})
	require.NoError(t, err)
	response = scimRequest(t, server.Client(), http.MethodPost, server.URL+"/scim/v2/Groups", env.token, groupBody, nil)
	assert.Equal(t, http.StatusCreated, response.StatusCode)
	var group GroupResource
	decodeHTTPBody(t, response, &group)
	response = scimRequest(t, server.Client(), http.MethodGet, server.URL+group.Meta.Location, env.token, nil, nil)
	assert.Equal(t, http.StatusOK, response.StatusCode)
	decodeHTTPBody(t, response, &group)
	groupInput := GroupInput{Schemas: []string{GroupSchema}, ExternalID: group.ExternalID, DisplayName: "HTTP Group Replaced", Members: group.Members}
	groupBody, err = json.Marshal(groupInput)
	require.NoError(t, err)
	response = scimRequest(t, server.Client(), http.MethodPut, server.URL+group.Meta.Location, env.token, groupBody, map[string]string{"If-Match": group.Meta.Version})
	assert.Equal(t, http.StatusOK, response.StatusCode)
	decodeHTTPBody(t, response, &group)
	groupPatch := []byte(`{"schemas":["` + PatchSchema + `"],"Operations":[{"op":"replace","path":"displayName","value":"HTTP Group Patched"}]}`)
	response = scimRequest(t, server.Client(), http.MethodPatch, server.URL+group.Meta.Location, env.token, groupPatch, map[string]string{"If-Match": group.Meta.Version})
	assert.Equal(t, http.StatusOK, response.StatusCode)
	decodeHTTPBody(t, response, &group)
	response = scimRequest(t, server.Client(), http.MethodGet, server.URL+"/scim/v2/Groups?filter=displayName%20co%20%22Patched%22", env.token, nil, nil)
	assert.Equal(t, http.StatusOK, response.StatusCode)
	var groupList ListResponse[GroupResource]
	decodeHTTPBody(t, response, &groupList)
	assert.Equal(t, 1, groupList.TotalResults)

	bulkUser, err := json.Marshal(activeUserInput("http-bulk", "http-bulk", "http-bulk@example.test"))
	require.NoError(t, err)
	bulkBody, err := json.Marshal(BulkRequest{Schemas: []string{BulkSchema}, Operations: []BulkOperation{{Method: http.MethodPost, BulkID: "http-bulk", Path: "/Users", Data: bulkUser}}})
	require.NoError(t, err)
	response = scimRequest(t, server.Client(), http.MethodPost, server.URL+"/scim/v2/Bulk", env.token, bulkBody, nil)
	assert.Equal(t, http.StatusOK, response.StatusCode)
	var bulkResponse BulkResponse
	decodeHTTPBody(t, response, &bulkResponse)
	require.Len(t, bulkResponse.Operations, 1)
	assert.Equal(t, "201", bulkResponse.Operations[0].Status)

	for _, path := range []string{
		"/scim/v2/Schemas", "/scim/v2/Schemas/" + UserSchema,
		"/scim/v2/ResourceTypes", "/scim/v2/ResourceTypes/User",
	} {
		response = scimRequest(t, server.Client(), http.MethodGet, server.URL+path, "", nil, nil)
		assert.Equal(t, http.StatusOK, response.StatusCode, path)
		require.NoError(t, response.Body.Close())
	}
	response = scimRequest(t, server.Client(), http.MethodGet, server.URL+"/scim/v2/Schemas/missing", "", nil, nil)
	assert.Equal(t, http.StatusNotFound, response.StatusCode)
	decodeHTTPBody(t, response, &protocolError)

	response = scimRequest(t, server.Client(), http.MethodGet, server.URL+"/scim/v2/Users?count=0", env.token, nil, nil)
	var list ListResponse[UserResource]
	decodeHTTPBody(t, response, &list)
	assert.Equal(t, 2, list.TotalResults)
	assert.Empty(t, list.Resources)
	assert.Zero(t, list.ItemsPerPage)

	response = scimRequest(t, server.Client(), http.MethodPost, server.URL+"/scim/v2/Users", env.token, []byte(`{"schemas":[]}{}`), map[string]string{"Accept-Language": "zh-CN"})
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	decodeHTTPBody(t, response, &protocolError)
	assert.Equal(t, localized("zh-CN", "scim_invalid_syntax"), protocolError.Detail)

	oversized := []byte(`{"padding":"` + strings.Repeat("x", maxBodyBytes) + `"}`)
	response = scimRequest(t, server.Client(), http.MethodPost, server.URL+"/scim/v2/Users", env.token, oversized, nil)
	assert.Equal(t, http.StatusRequestEntityTooLarge, response.StatusCode)
	decodeHTTPBody(t, response, &protocolError)
	assert.Equal(t, "tooMany", protocolError.ScimType)

	response = scimRequest(t, server.Client(), http.MethodPut, server.URL+user.Meta.Location, env.token, body, map[string]string{"If-Match": "1"})
	assert.Equal(t, http.StatusPreconditionFailed, response.StatusCode)
	decodeHTTPBody(t, response, &protocolError)
	assert.Equal(t, localized(i18n.Base, "scim_version_conflict"), protocolError.Detail)

	response = scimRequest(t, server.Client(), http.MethodDelete, server.URL+group.Meta.Location, env.token, nil, map[string]string{"If-Match": group.Meta.Version})
	assert.Equal(t, http.StatusNoContent, response.StatusCode)
	require.NoError(t, response.Body.Close())
	response = scimRequest(t, server.Client(), http.MethodDelete, server.URL+user.Meta.Location, env.token, nil, map[string]string{"If-Match": user.Meta.Version})
	assert.Equal(t, http.StatusNoContent, response.StatusCode)
	require.NoError(t, response.Body.Close())

	usersOperation := api.OpenAPI().Paths["/scim/v2/Users"].Post
	require.NotNil(t, usersOperation)
	assert.Equal(t, []map[string][]string{{scimBearerScheme: {}}}, usersOperation.Security)
	assert.Contains(t, usersOperation.RequestBody.Content, SCIMContentType)
	assert.Contains(t, usersOperation.Responses["201"].Content, SCIMContentType)
	assert.Empty(t, api.OpenAPI().Paths["/scim/v2/ServiceProviderConfig"].Get.Security)
}

func TestSCIMHTTPErrorPathsAreLocalized(t *testing.T) {
	env := newProvisioningEnvironment(t)
	require.NoError(t, i18n.InitEmbedded(i18n.Base))
	require.NoError(t, RegisterI18n())
	router := chi.NewRouter()
	router.Use(respx.Locale)
	api := humachi.New(router, huma.DefaultConfig("SCIM errors", "1.0.0"))
	RegisterSCIMREST(api, env.service)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	userBody, err := json.Marshal(activeUserInput("error-user", "error-user", "error-user@example.test"))
	require.NoError(t, err)
	groupBody, err := json.Marshal(GroupInput{Schemas: []string{GroupSchema}, DisplayName: "Error Group"})
	require.NoError(t, err)
	patchBody := []byte(`{"schemas":["` + PatchSchema + `"],"Operations":[{"op":"replace","path":"displayName","value":"Changed"}]}`)
	bulkBody := []byte(`{"schemas":["` + BulkSchema + `"],"Operations":[]}`)

	unauthorized := []struct {
		method string
		path   string
		body   []byte
	}{
		{http.MethodGet, "/scim/v2/Users", nil},
		{http.MethodPost, "/scim/v2/Users", userBody},
		{http.MethodGet, "/scim/v2/Users/missing", nil},
		{http.MethodPut, "/scim/v2/Users/missing", userBody},
		{http.MethodPatch, "/scim/v2/Users/missing", patchBody},
		{http.MethodDelete, "/scim/v2/Users/missing", nil},
		{http.MethodGet, "/scim/v2/Groups", nil},
		{http.MethodPost, "/scim/v2/Groups", groupBody},
		{http.MethodGet, "/scim/v2/Groups/missing", nil},
		{http.MethodPut, "/scim/v2/Groups/missing", groupBody},
		{http.MethodPatch, "/scim/v2/Groups/missing", patchBody},
		{http.MethodDelete, "/scim/v2/Groups/missing", nil},
		{http.MethodPost, "/scim/v2/Bulk", bulkBody},
	}
	for index, test := range unauthorized {
		locale := []string{"en-US", "zh-CN", "ms-MY"}[index%3]
		response := scimRequest(t, server.Client(), test.method, server.URL+test.path, "", test.body, map[string]string{"Accept-Language": locale})
		assert.Equal(t, http.StatusUnauthorized, response.StatusCode, "%s %s", test.method, test.path)
		var protocolError SCIMError
		decodeHTTPBody(t, response, &protocolError)
		assert.Equal(t, localized(locale, "scim_unauthorized"), protocolError.Detail)
	}

	tests := []struct {
		name    string
		method  string
		path    string
		body    []byte
		headers map[string]string
		status  int
		key     string
	}{
		{"invalid user start index", http.MethodGet, "/scim/v2/Users?startIndex=bad", nil, nil, http.StatusBadRequest, "scim_invalid_syntax"},
		{"invalid user filter", http.MethodGet, "/scim/v2/Users?filter=userName%20eq", nil, nil, http.StatusBadRequest, "scim_invalid_filter"},
		{"invalid group count", http.MethodGet, "/scim/v2/Groups?count=bad", nil, nil, http.StatusBadRequest, "scim_invalid_syntax"},
		{"invalid group filter", http.MethodGet, "/scim/v2/Groups?filter=displayName%20eq", nil, nil, http.StatusBadRequest, "scim_invalid_filter"},
		{"user media type", http.MethodPost, "/scim/v2/Users", userBody, map[string]string{"Content-Type": "text/plain"}, http.StatusBadRequest, "scim_invalid_syntax"},
		{"user malformed body", http.MethodPost, "/scim/v2/Users", []byte(`{"schemas":`), nil, http.StatusBadRequest, "scim_invalid_syntax"},
		{"user invalid body", http.MethodPost, "/scim/v2/Users", []byte(`{}`), nil, http.StatusBadRequest, "scim_invalid_syntax"},
		{"group media type", http.MethodPost, "/scim/v2/Groups", groupBody, map[string]string{"Content-Type": "text/plain"}, http.StatusBadRequest, "scim_invalid_syntax"},
		{"group malformed body", http.MethodPost, "/scim/v2/Groups", []byte(`{"schemas":`), nil, http.StatusBadRequest, "scim_invalid_syntax"},
		{"group invalid body", http.MethodPost, "/scim/v2/Groups", []byte(`{}`), nil, http.StatusBadRequest, "scim_invalid_syntax"},
		{"missing user", http.MethodGet, "/scim/v2/Users/missing", nil, nil, http.StatusNotFound, "scim_resource_not_found"},
		{"replace user etag", http.MethodPut, "/scim/v2/Users/missing", userBody, map[string]string{"If-Match": "1"}, http.StatusPreconditionFailed, "scim_version_conflict"},
		{"replace user malformed body", http.MethodPut, "/scim/v2/Users/missing", []byte(`{`), nil, http.StatusBadRequest, "scim_invalid_syntax"},
		{"replace missing user", http.MethodPut, "/scim/v2/Users/missing", userBody, nil, http.StatusNotFound, "scim_resource_not_found"},
		{"patch user etag", http.MethodPatch, "/scim/v2/Users/missing", patchBody, map[string]string{"If-Match": "1"}, http.StatusPreconditionFailed, "scim_version_conflict"},
		{"patch user malformed body", http.MethodPatch, "/scim/v2/Users/missing", []byte(`{`), nil, http.StatusBadRequest, "scim_invalid_syntax"},
		{"patch missing user", http.MethodPatch, "/scim/v2/Users/missing", patchBody, nil, http.StatusNotFound, "scim_resource_not_found"},
		{"delete user etag", http.MethodDelete, "/scim/v2/Users/missing", nil, map[string]string{"If-Match": "1"}, http.StatusPreconditionFailed, "scim_version_conflict"},
		{"delete missing user", http.MethodDelete, "/scim/v2/Users/missing", nil, nil, http.StatusNotFound, "scim_resource_not_found"},
		{"missing group", http.MethodGet, "/scim/v2/Groups/missing", nil, nil, http.StatusNotFound, "scim_resource_not_found"},
		{"replace group etag", http.MethodPut, "/scim/v2/Groups/missing", groupBody, map[string]string{"If-Match": "1"}, http.StatusPreconditionFailed, "scim_version_conflict"},
		{"replace group malformed body", http.MethodPut, "/scim/v2/Groups/missing", []byte(`{`), nil, http.StatusBadRequest, "scim_invalid_syntax"},
		{"replace missing group", http.MethodPut, "/scim/v2/Groups/missing", groupBody, nil, http.StatusNotFound, "scim_resource_not_found"},
		{"patch group etag", http.MethodPatch, "/scim/v2/Groups/missing", patchBody, map[string]string{"If-Match": "1"}, http.StatusPreconditionFailed, "scim_version_conflict"},
		{"patch group malformed body", http.MethodPatch, "/scim/v2/Groups/missing", []byte(`{`), nil, http.StatusBadRequest, "scim_invalid_syntax"},
		{"patch missing group", http.MethodPatch, "/scim/v2/Groups/missing", patchBody, nil, http.StatusNotFound, "scim_resource_not_found"},
		{"delete group etag", http.MethodDelete, "/scim/v2/Groups/missing", nil, map[string]string{"If-Match": "1"}, http.StatusPreconditionFailed, "scim_version_conflict"},
		{"delete missing group", http.MethodDelete, "/scim/v2/Groups/missing", nil, nil, http.StatusNotFound, "scim_resource_not_found"},
		{"bulk media type", http.MethodPost, "/scim/v2/Bulk", bulkBody, map[string]string{"Content-Type": "text/plain"}, http.StatusBadRequest, "scim_invalid_syntax"},
		{"bulk malformed body", http.MethodPost, "/scim/v2/Bulk", []byte(`{`), nil, http.StatusBadRequest, "scim_invalid_syntax"},
		{"bulk invalid body", http.MethodPost, "/scim/v2/Bulk", []byte(`{}`), nil, http.StatusBadRequest, "scim_invalid_syntax"},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			locale := []string{"en-US", "zh-CN", "ms-MY"}[index%3]
			headers := map[string]string{"Accept-Language": locale}
			for name, value := range test.headers {
				headers[name] = value
			}
			response := scimRequest(t, server.Client(), test.method, server.URL+test.path, env.token, test.body, headers)
			assert.Equal(t, test.status, response.StatusCode)
			var protocolError SCIMError
			decodeHTTPBody(t, response, &protocolError)
			assert.Equal(t, localized(locale, test.key), protocolError.Detail)
		})
	}

	response := scimRequest(t, server.Client(), http.MethodGet, server.URL+"/scim/v2/ResourceTypes/missing", "", nil, nil)
	assert.Equal(t, http.StatusNotFound, response.StatusCode)
	var protocolError SCIMError
	decodeHTTPBody(t, response, &protocolError)
}

func scimRequest(t *testing.T, client *http.Client, method, target, token string, body []byte, headers map[string]string) *http.Response {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, target, bytes.NewReader(body))
	require.NoError(t, err)
	if body != nil {
		request.Header.Set("Content-Type", SCIMContentType)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
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

func localized(locale, key string) string {
	return i18n.TContext(i18n.WithLocale(context.Background(), locale), key)
}
