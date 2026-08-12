package federation

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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

func newSAMLAdminAPI(t *testing.T, env *federationEnvironment) (*http.Client, *httptest.Server) {
	t.Helper()
	require.NoError(t, i18n.InitEmbedded(i18n.Base))
	require.NoError(t, RegisterI18n())
	respx.Install()
	router := chi.NewRouter()
	router.Use(respx.Timing, respx.Locale)
	config := huma.DefaultConfig("federation saml admin test", "1.0.0")
	config.Transformers = append(config.Transformers, respx.LocalizeMessage)
	api := humachi.New(router, config)
	RegisterSAMLAdminREST(api, env.service, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server.Client(), server
}

func TestSAMLServiceProviderHTTPWorkflow(t *testing.T) {
	env := newFederationEnvironment(t)
	startSAML(t, env, SAMLConfig{Enabled: true})
	client, server := newSAMLAdminAPI(t, env)
	tsp := newSAMLSPServer(t)

	createBody, err := json.Marshal(samlSPBody{Name: "portal", EntityID: tsp.entityID, MetadataXML: tsp.metadataXML()})
	require.NoError(t, err)
	response := adminRequest(t, client, http.MethodPost, server.URL+"/iam/saml/service-providers", wireID("tenant-a"), []byte(createBody), nil)
	assert.Equal(t, http.StatusCreated, response.StatusCode)
	var envelope federationEnvelope
	decodeHTTPBody(t, response, &envelope)
	var registered SAMLServiceProvider
	require.NoError(t, json.Unmarshal(envelope.Data, &registered))
	assert.Equal(t, "portal", registered.Name)
	assert.Equal(t, tsp.entityID, registered.EntityID)

	response = adminRequest(t, client, http.MethodGet, server.URL+"/iam/saml/service-providers", wireID("tenant-a"), nil, nil)
	assert.Equal(t, http.StatusOK, response.StatusCode)
	decodeHTTPBody(t, response, &envelope)
	var items []SAMLServiceProvider
	require.NoError(t, json.Unmarshal(envelope.Data, &items))
	require.Len(t, items, 1)

	response = adminRequest(t, client, http.MethodPost, server.URL+"/iam/saml/service-providers", wireID("tenant-a"), []byte(createBody), map[string]string{"Accept-Language": "zh-CN"})
	assert.Equal(t, http.StatusConflict, response.StatusCode)
	decodeHTTPBody(t, response, &envelope)
	assert.Equal(t, localized("zh-CN", "federation_saml_sp_entity_id_exists"), envelope.Message)

	invalidBody := `{"name":"bad","entity_id":"https://bad.example/metadata","metadata_xml":"<not-xml"}`
	response = adminRequest(t, client, http.MethodPost, server.URL+"/iam/saml/service-providers", wireID("tenant-a"), []byte(invalidBody), nil)
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	decodeHTTPBody(t, response, &envelope)
	assert.Equal(t, localized("en-US", "federation_saml_invalid_sp"), envelope.Message)

	other := newSAMLSPServer(t)
	updateBody, err := json.Marshal(samlSPBody{Name: "portal-v2", EntityID: other.entityID, MetadataXML: other.metadataXML(), Status: ProviderDisabled})
	require.NoError(t, err)
	response = adminRequest(t, client, http.MethodPut, server.URL+"/iam/saml/service-providers/" + registered.ID.String(), wireID("tenant-a"), []byte(updateBody), nil)
	assert.Equal(t, http.StatusOK, response.StatusCode)
	decodeHTTPBody(t, response, &envelope)
	var updated SAMLServiceProvider
	require.NoError(t, json.Unmarshal(envelope.Data, &updated))
	assert.Equal(t, "portal-v2", updated.Name)
	assert.Equal(t, ProviderDisabled, updated.Status)

	response = adminRequest(t, client, http.MethodDelete, server.URL+"/iam/saml/service-providers/" + registered.ID.String(), wireID("tenant-a"), nil, nil)
	assert.Equal(t, http.StatusOK, response.StatusCode)
	response = adminRequest(t, client, http.MethodDelete, server.URL+"/iam/saml/service-providers/" + registered.ID.String(), wireID("tenant-a"), nil, nil)
	assert.Equal(t, http.StatusNotFound, response.StatusCode)
	decodeHTTPBody(t, response, &envelope)
	assert.Equal(t, localized("en-US", "federation_saml_sp_not_found"), envelope.Message)
}

func TestSAMLSigningKeyRotationHTTP(t *testing.T) {
	env := newFederationEnvironment(t)
	startSAML(t, env, SAMLConfig{Enabled: true})
	client, server := newSAMLAdminAPI(t, env)
	firstCert := env.service.samlCert.Raw

	response := adminRequest(t, client, http.MethodPost, server.URL+"/iam/saml/signing-key/rotate", wireID("tenant-a"), nil, nil)
	assert.Equal(t, http.StatusCreated, response.StatusCode)
	var envelope federationEnvelope
	decodeHTTPBody(t, response, &envelope)
	var info SAMLKeyInfo
	require.NoError(t, json.Unmarshal(envelope.Data, &info))
	assert.NotEmpty(t, info.KeyID)
	assert.NotEqual(t, firstCert, env.service.samlCert.Raw)
}

func TestSAMLSigningKeyRotationFileBackedConflict(t *testing.T) {
	env := newFederationEnvironment(t)
	startSAML(t, env, SAMLConfig{Enabled: true})
	keyFile := t.TempDir() + "/saml.key"
	certFile := t.TempDir() + "/saml.crt"
	require.NoError(t, os.WriteFile(keyFile, pemEncodeKey(t, env.service.samlKey), 0o600))
	require.NoError(t, os.WriteFile(certFile, pemEncodeCert(t, env.service.samlCert), 0o600))
	require.NoError(t, env.service.StartSAML(t.Context(), SAMLConfig{Enabled: true, SigningKeyFile: keyFile, CertificateFile: certFile}))
	client, server := newSAMLAdminAPI(t, env)

	response := adminRequest(t, client, http.MethodPost, server.URL+"/iam/saml/signing-key/rotate", wireID("tenant-a"), nil, nil)
	assert.Equal(t, http.StatusConflict, response.StatusCode)
	var envelope federationEnvelope
	decodeHTTPBody(t, response, &envelope)
	assert.Equal(t, localized("en-US", "federation_saml_key_managed_externally"), envelope.Message)
}

func TestSAMLRotationUnavailableWhenDisabled(t *testing.T) {
	env := newFederationEnvironment(t)
	client, server := newSAMLAdminAPI(t, env)
	response := adminRequest(t, client, http.MethodPost, server.URL+"/iam/saml/signing-key/rotate", wireID("tenant-a"), nil, nil)
	assert.Equal(t, http.StatusConflict, response.StatusCode)
	var envelope federationEnvelope
	decodeHTTPBody(t, response, &envelope)
	assert.Equal(t, localized("en-US", "federation_saml_unavailable"), envelope.Message)
}
