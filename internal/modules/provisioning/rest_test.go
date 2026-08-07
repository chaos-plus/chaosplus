package provisioning

import (
	"bytes"
	"encoding/json"
	"errors"
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

type adminEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func TestSCIMManagementHTTPWorkflow(t *testing.T) {
	env := newProvisioningEnvironment(t)
	require.NoError(t, i18n.InitEmbedded(i18n.Base))
	require.NoError(t, RegisterI18n())
	respx.Install()
	router := chi.NewRouter()
	router.Use(respx.Timing, respx.Locale)
	config := huma.DefaultConfig("management test", "1.0.0")
	config.Transformers = append(config.Transformers, respx.LocalizeMessage)
	api := humachi.New(router, config)
	RegisterAdminREST(api, env.service, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	response := adminRequest(t, server.Client(), http.MethodPost, server.URL+"/iam/scim/directories", "tenant-a", []byte(`{"name":"Secondary"}`), nil)
	assert.Equal(t, http.StatusCreated, response.StatusCode)
	var envelope adminEnvelope
	decodeHTTPBody(t, response, &envelope)
	assert.Zero(t, envelope.Code)
	var directory Directory
	require.NoError(t, json.Unmarshal(envelope.Data, &directory))
	assert.Equal(t, "Secondary", directory.Name)
	response = adminRequest(t, server.Client(), http.MethodGet, server.URL+"/iam/scim/directories", "tenant-a", nil, nil)
	assert.Equal(t, http.StatusOK, response.StatusCode)
	decodeHTTPBody(t, response, &envelope)
	var directories []Directory
	require.NoError(t, json.Unmarshal(envelope.Data, &directories))
	assert.Len(t, directories, 2)

	response = adminRequest(t, server.Client(), http.MethodPost, server.URL+"/iam/scim/directories/"+directory.ID+"/credentials", "tenant-a", []byte(`{"name":"automation"}`), nil)
	assert.Equal(t, http.StatusCreated, response.StatusCode)
	decodeHTTPBody(t, response, &envelope)
	var issued CredentialSecret
	require.NoError(t, json.Unmarshal(envelope.Data, &issued))
	assert.NotEmpty(t, issued.Token)
	assert.NotContains(t, issued.Credential.Name, issued.Token)

	response = adminRequest(t, server.Client(), http.MethodGet, server.URL+"/iam/scim/directories/"+directory.ID+"/credentials", "tenant-a", nil, nil)
	decodeHTTPBody(t, response, &envelope)
	assert.NotContains(t, string(envelope.Data), issued.Token)
	var credentials []Credential
	require.NoError(t, json.Unmarshal(envelope.Data, &credentials))
	require.Len(t, credentials, 1)

	response = adminRequest(t, server.Client(), http.MethodDelete, server.URL+"/iam/scim/directories/"+directory.ID+"/credentials/"+issued.Credential.ID, "tenant-a", nil, nil)
	assert.Equal(t, http.StatusOK, response.StatusCode)
	decodeHTTPBody(t, response, &envelope)
	assert.Contains(t, string(envelope.Data), `"revoked":true`)

	response = adminRequest(t, server.Client(), http.MethodPut, server.URL+"/iam/scim/directories/"+directory.ID, "tenant-a", []byte(`{"name":"Secondary","status":"disabled","version":1}`), nil)
	assert.Equal(t, http.StatusOK, response.StatusCode)
	decodeHTTPBody(t, response, &envelope)
	require.NoError(t, json.Unmarshal(envelope.Data, &directory))
	assert.Equal(t, DirectoryDisabled, directory.Status)

	response = adminRequest(t, server.Client(), http.MethodPost, server.URL+"/iam/scim/directories", "tenant-a", []byte(`{"name":"Secondary"}`), map[string]string{"Accept-Language": "zh-CN"})
	assert.Equal(t, http.StatusConflict, response.StatusCode)
	decodeHTTPBody(t, response, &envelope)
	assert.Equal(t, localized("zh-CN", "scim_directory_name_exists"), envelope.Message)
	assert.NotEqual(t, "scim_directory_name_exists", envelope.Message)
}

func TestProvisioningManagementErrorMapping(t *testing.T) {
	for err, status := range map[error]int{
		ErrInvalidDirectory:  http.StatusUnprocessableEntity,
		ErrDirectoryMissing:  http.StatusNotFound,
		ErrDirectoryName:     http.StatusConflict,
		ErrDirectoryVersion:  http.StatusConflict,
		ErrCredentialMissing: http.StatusNotFound,
		ErrCredentialLimit:   http.StatusConflict,
	} {
		mapped := provisioningError(err)
		var statusError huma.StatusError
		require.True(t, errors.As(mapped, &statusError))
		assert.Equal(t, status, statusError.GetStatus())
	}
	var statusError huma.StatusError
	require.True(t, errors.As(provisioningError(assert.AnError), &statusError))
	assert.Equal(t, http.StatusInternalServerError, statusError.GetStatus())
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
