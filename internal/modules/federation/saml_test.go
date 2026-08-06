package federation

import (
	"bytes"
	"compress/flate"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"encoding/xml"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
	authnmod "github.com/chaos-plus/chaosplus/internal/modules/authn"
	"github.com/chaos-plus/chaosplus/pkg/i18n"
	saml "github.com/crewjam/saml"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// samlTestSP models a downstream service provider with real RSA material and
// stable endpoint URLs.
type samlTestSP struct {
	key      *rsa.PrivateKey
	cert     *x509.Certificate
	entityID string
	acsURL   string
	sloURL   string
}

func newSAMLSPServer(t *testing.T) *samlTestSP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	cert := testRSACertificate(t, key)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	return &samlTestSP{
		key: key, cert: cert,
		entityID: server.URL + "/metadata",
		acsURL:   server.URL + "/acs",
		sloURL:   server.URL + "/slo",
	}
}

func testRSACertificate(t *testing.T, key *rsa.PrivateKey) *x509.Certificate {
	t.Helper()
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)
	now := time.Now().UTC()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "saml-test-sp"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}

// metadataXML builds a realistic SP metadata document with POST ACS, SLO, and
// requested email/uid attributes so assertion attribute mapping is exercised.
func (tsp *samlTestSP) metadataXML() string {
	certB64 := base64.StdEncoding.EncodeToString(tsp.cert.Raw)
	return fmt.Sprintf(`<md:EntityDescriptor xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata" entityID="%s">
  <md:SPSSODescriptor AuthnRequestsSigned="false" WantAssertionsSigned="true" protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <md:KeyDescriptor use="signing"><ds:KeyInfo xmlns:ds="http://www.w3.org/2000/09/xmldsig#"><ds:X509Data><ds:X509Certificate>%s</ds:X509Certificate></ds:X509Data></ds:KeyInfo></md:KeyDescriptor>
    <md:SingleLogoutService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST" Location="%s"/>
    <md:AssertionConsumerService index="0" isDefault="true" Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST" Location="%s"/>
    <md:AttributeConsumingService index="0"><md:ServiceName xml:lang="en">test</md:ServiceName><md:RequestedAttribute Name="email" NameFormat="urn:oasis:names:tc:SAML:2.0:attrname-format:basic" FriendlyName="email"/><md:RequestedAttribute Name="uid" NameFormat="urn:oasis:names:tc:SAML:2.0:attrname-format:basic" FriendlyName="uid"/></md:AttributeConsumingService>
  </md:SPSSODescriptor>
</md:EntityDescriptor>`, tsp.entityID, certB64, tsp.sloURL, tsp.acsURL)
}

func (tsp *samlTestSP) register(t *testing.T, env *federationEnvironment) SAMLServiceProvider {
	t.Helper()
	registered, err := env.service.CreateSAMLServiceProvider(t.Context(), "tenant-a", SAMLServiceProviderInput{
		Name: "portal", EntityID: tsp.entityID, MetadataXML: tsp.metadataXML(), Status: samlSPStatusActive,
	})
	require.NoError(t, err)
	return registered
}

func startSAML(t *testing.T, env *federationEnvironment, cfg SAMLConfig) {
	t.Helper()
	require.NoError(t, env.service.StartSAML(t.Context(), cfg))
}

func newSAMLServer(t *testing.T, env *federationEnvironment) *httptest.Server {
	t.Helper()
	require.NoError(t, i18n.InitEmbedded(i18n.Base))
	require.NoError(t, RegisterI18n())
	respx.Install()
	router := chi.NewRouter()
	router.Use(respx.Timing, respx.Locale)
	config := huma.DefaultConfig("federation saml test", "1.0.0")
	config.Transformers = append(config.Transformers, respx.LocalizeMessage)
	api := humachi.New(router, config)
	RegisterSAMLREST(api, env.service, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server
}

func noRedirectClient() *http.Client {
	return &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
}

func samlSessionCookie(t *testing.T, env *federationEnvironment) (string, string) {
	t.Helper()
	principalID, err := authnmod.EnsureBootstrapPrincipal(t.Context(), env.db, authnmod.BootstrapPrincipal{
		LoginName: "alice", Password: "correct horse battery staple", DisplayName: "Alice Example", Email: "alice@example.com",
	})
	require.NoError(t, err)
	token, err := env.web.CreateSession(t.Context(), principalID, time.Now().UTC(), authnext.Assurance{AuthTime: time.Now().UTC(), Level: 1, Methods: []string{"password"}})
	require.NoError(t, err)
	return principalID, env.web.SessionCookie(token)
}

func samlAuthnRequestParam(t *testing.T, tsp *samlTestSP, id string, issued time.Time, destination, acsURL string) string {
	t.Helper()
	if acsURL == "" {
		acsURL = tsp.acsURL
	}
	request := &saml.AuthnRequest{
		ID: id, Version: "2.0", IssueInstant: issued,
		Destination: destination, Issuer: &saml.Issuer{Value: tsp.entityID},
		AssertionConsumerServiceURL:   acsURL,
		AssertionConsumerServiceIndex: "0",
		ProtocolBinding:               saml.HTTPPostBinding,
	}
	raw, err := xml.Marshal(request)
	require.NoError(t, err)
	var compressed bytes.Buffer
	zw, err := flate.NewWriter(&compressed, flate.DefaultCompression)
	require.NoError(t, err)
	_, err = zw.Write(raw)
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return base64.StdEncoding.EncodeToString(compressed.Bytes())
}

func fetchIDPMetadata(t *testing.T, client *http.Client, server *httptest.Server, tenantID string) *saml.EntityDescriptor {
	t.Helper()
	response, err := client.Get(server.URL + samlMetadataPath(tenantID))
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	body := new(bytes.Buffer)
	_, err = body.ReadFrom(response.Body)
	require.NoError(t, err)
	var ed saml.EntityDescriptor
	require.NoError(t, xml.Unmarshal(body.Bytes(), &ed))
	return &ed
}

func extractSAMLResponse(t *testing.T, html string) string {
	t.Helper()
	start := strings.Index(html, `name="SAMLResponse" value="`)
	require.Greater(t, start, -1, "response form missing: %s", html)
	value := html[start+len(`name="SAMLResponse" value="`):]
	end := strings.Index(value, `"`)
	require.Greater(t, end, -1)
	require.NotEmpty(t, value[:end])
	return value[:end]
}

func attributeValue(t *testing.T, assertion *saml.Assertion, name string) string {
	t.Helper()
	for _, statement := range assertion.AttributeStatements {
		for _, attribute := range statement.Attributes {
			if attribute.Name == name && len(attribute.Values) > 0 {
				return attribute.Values[0].Value
			}
		}
	}
	return ""
}

func TestSAMLKeyLifecycle(t *testing.T) {
	env := newFederationEnvironment(t)
	ctx := t.Context()

	// Disabled SAML leaves the IdP unavailable.
	require.NoError(t, env.service.StartSAML(ctx, SAMLConfig{}))
	assert.Nil(t, env.service.samlKey)

	// Enabled SAML generates and persists a database key.
	require.NoError(t, env.service.StartSAML(ctx, SAMLConfig{Enabled: true}))
	require.NotNil(t, env.service.samlKey)
	firstCert := env.service.samlCert.Raw

	// A second start reloads the same key instead of regenerating it.
	require.NoError(t, env.service.StartSAML(ctx, SAMLConfig{Enabled: true}))
	require.Equal(t, firstCert, env.service.samlCert.Raw)

	// Rotation replaces the active certificate and keeps the key stable across restarts.
	rotated, err := env.service.RotateSAMLSigningKey(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, rotated.KeyID)
	assert.NotEqual(t, firstCert, env.service.samlCert.Raw)
	rotatedCert := env.service.samlCert.Raw
	require.NoError(t, env.service.StartSAML(ctx, SAMLConfig{Enabled: true}))
	require.Equal(t, rotatedCert, env.service.samlCert.Raw)

	// File-backed keys bypass the database and cannot be rotated through the API.
	keyPEM := pemEncodeKey(t, env.service.samlKey)
	certPEM := pemEncodeCert(t, env.service.samlCert)
	keyFile := filepath.Join(t.TempDir(), "saml.key")
	certFile := filepath.Join(t.TempDir(), "saml.crt")
	require.NoError(t, os.WriteFile(keyFile, keyPEM, 0o600))
	require.NoError(t, os.WriteFile(certFile, certPEM, 0o600))
	require.NoError(t, env.service.StartSAML(ctx, SAMLConfig{Enabled: true, SigningKeyFile: keyFile, CertificateFile: certFile}))
	require.NotNil(t, env.service.samlKey)
	_, err = env.service.RotateSAMLSigningKey(ctx)
	assert.ErrorIs(t, err, ErrSAMLKeyManagedExternally)

	// Key and certificate mismatch and half-configured files fail fast.
	other := newSAMLSPServer(t)
	otherKeyPEM := pemEncodeKey(t, other.key)
	require.NoError(t, os.WriteFile(keyFile, otherKeyPEM, 0o600))
	err = env.service.StartSAML(ctx, SAMLConfig{Enabled: true, SigningKeyFile: keyFile, CertificateFile: certFile})
	require.ErrorContains(t, err, "do not match")
	err = env.service.StartSAML(ctx, SAMLConfig{Enabled: true, SigningKeyFile: keyFile})
	require.ErrorContains(t, err, "must be configured together")
}

func TestSAMLSigningKeyPersistFailsWithoutEncryptionKey(t *testing.T) {
	env := newFederationEnvironment(t)
	// A missing federation encryption key surfaces at persistence time.
	service := &Service{db: env.db, now: time.Now}
	err := service.StartSAML(t.Context(), SAMLConfig{Enabled: true})
	require.Error(t, err)
}

func TestSAMLServiceProviderManagement(t *testing.T) {
	env := newFederationEnvironment(t)
	ctx := t.Context()
	tsp := newSAMLSPServer(t)

	invalid := []SAMLServiceProviderInput{
		{Name: "", EntityID: tsp.entityID, MetadataXML: tsp.metadataXML()},
		{Name: "x", EntityID: "", MetadataXML: tsp.metadataXML()},
		{Name: "x", EntityID: tsp.entityID, MetadataXML: ""},
		{Name: "x", EntityID: tsp.entityID, MetadataXML: "<not-xml"},
		{Name: "x", EntityID: tsp.entityID, MetadataXML: `<?xml version="1.0"?><!DOCTYPE md [<!ENTITY xxe SYSTEM "file:///etc/passwd">]><md:EntityDescriptor xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata" entityID="` + tsp.entityID + `"><md:SPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol"/></md:EntityDescriptor>`},
		{Name: "x", EntityID: tsp.entityID, MetadataXML: strings.Replace(tsp.metadataXML(), tsp.entityID, "https://other.example/metadata", 1)},
		{Name: "x", EntityID: tsp.entityID, MetadataXML: `<md:EntityDescriptor xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata" entityID="` + tsp.entityID + `"><md:SPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol"/></md:EntityDescriptor>`},
		{Name: "x", EntityID: tsp.entityID, MetadataXML: tsp.metadataXML(), Status: "bogus"},
	}
	for index, input := range invalid {
		_, err := env.service.CreateSAMLServiceProvider(ctx, "tenant-a", input)
		assert.ErrorIs(t, err, ErrInvalidSAMLSP, "case %d", index)
	}

	registered := tsp.register(t, env)
	assert.Equal(t, "portal", registered.Name)
	assert.Equal(t, tsp.entityID, registered.EntityID)
	assert.Equal(t, samlSPStatusActive, registered.Status)

	_, err := env.service.CreateSAMLServiceProvider(ctx, "tenant-a", SAMLServiceProviderInput{
		Name: "duplicate", EntityID: tsp.entityID, MetadataXML: tsp.metadataXML(),
	})
	assert.ErrorIs(t, err, ErrSAMLSPEntityIDExists)

	items, err := env.service.ListSAMLServiceProviders(ctx, "tenant-a")
	require.NoError(t, err)
	require.Len(t, items, 1)

	other := newSAMLSPServer(t)
	updated, err := env.service.UpdateSAMLServiceProvider(ctx, "tenant-a", registered.ID, SAMLServiceProviderInput{
		Name: "portal-v2", EntityID: other.entityID, MetadataXML: other.metadataXML(), Status: ProviderDisabled,
	})
	require.NoError(t, err)
	assert.Equal(t, "portal-v2", updated.Name)
	assert.Equal(t, ProviderDisabled, updated.Status)

	_, err = env.service.samlSPDescriptor(ctx, "tenant-a", other.entityID)
	assert.ErrorIs(t, err, os.ErrNotExist)

	_, err = env.service.UpdateSAMLServiceProvider(ctx, "tenant-a", "missing", SAMLServiceProviderInput{
		Name: "x", EntityID: tsp.entityID, MetadataXML: tsp.metadataXML(),
	})
	assert.ErrorIs(t, err, ErrSAMLSPNotFound)

	require.NoError(t, env.service.DeleteSAMLServiceProvider(ctx, "tenant-a", registered.ID))
	_, err = env.service.samlSPDescriptor(ctx, "tenant-a", other.entityID)
	assert.ErrorIs(t, err, os.ErrNotExist)
	err = env.service.DeleteSAMLServiceProvider(ctx, "tenant-a", registered.ID)
	assert.ErrorIs(t, err, ErrSAMLSPNotFound)

	_, err = env.service.ListSAMLServiceProviders(ctx, "")
	assert.ErrorIs(t, err, ErrInvalidSAMLSP)
	err = env.service.DeleteSAMLServiceProvider(ctx, "", "")
	assert.ErrorIs(t, err, ErrInvalidSAMLSP)
}

func TestSAMLSSOCompletesWithSignedAssertion(t *testing.T) {
	env := newFederationEnvironment(t)
	startSAML(t, env, SAMLConfig{Enabled: true})
	tsp := newSAMLSPServer(t)
	tsp.register(t, env)
	server := newSAMLServer(t, env)
	principalID, cookie := samlSessionCookie(t, env)
	ssoURL := server.URL + samlSSOPath("tenant-a")

	response := samlBrowserRequest(t, server, http.MethodGet, ssoURL+"?SAMLRequest="+url.QueryEscape(samlAuthnRequestParam(t, tsp, "id-request-1", time.Now().UTC(), ssoURL, "")), cookie)
	require.Equal(t, http.StatusOK, response.StatusCode)
	responseValue := extractSAMLResponse(t, samlReadBody(t, response))

	idpMetadata := fetchIDPMetadata(t, server.Client(), server, "tenant-a")
	sp := &saml.ServiceProvider{
		Key: tsp.key, Certificate: tsp.cert,
		MetadataURL: mustURL(tsp.entityID), AcsURL: mustURL(tsp.acsURL), SloURL: mustURL(tsp.sloURL),
		IDPMetadata: idpMetadata,
	}
	request := httptest.NewRequest(http.MethodPost, tsp.acsURL, strings.NewReader(url.Values{"SAMLResponse": {responseValue}}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	require.NoError(t, request.ParseForm())
	assertion, err := sp.ParseResponse(request, []string{"id-request-1"})
	require.NoError(t, err)
	require.NotNil(t, assertion.Subject)
	require.NotNil(t, assertion.Subject.NameID)
	assert.Equal(t, principalID, assertion.Subject.NameID.Value)
	assert.Equal(t, "alice@example.com", attributeValue(t, assertion, "email"))
}

func TestSAMLSSOPostBindingAndMetadata(t *testing.T) {
	env := newFederationEnvironment(t)
	startSAML(t, env, SAMLConfig{Enabled: true})
	tsp := newSAMLSPServer(t)
	tsp.register(t, env)
	server := newSAMLServer(t, env)
	_, cookie := samlSessionCookie(t, env)
	ssoURL := server.URL + samlSSOPath("tenant-a")

	// Metadata advertises entity ID, SSO/SLO endpoints, and the signing cert.
	metadataResponse, err := server.Client().Get(server.URL + samlMetadataPath("tenant-a"))
	require.NoError(t, err)
	body := samlReadBody(t, metadataResponse)
	assert.Equal(t, http.StatusOK, metadataResponse.StatusCode)
	assert.Contains(t, body, server.URL+samlMetadataPath("tenant-a"))
	assert.Contains(t, body, ssoURL)
	assert.Contains(t, body, server.URL+samlSLOPath("tenant-a"))
	assert.Contains(t, body, base64.StdEncoding.EncodeToString(env.service.samlCert.Raw))

	// HTTP-POST binding (raw base64, no deflate) also completes the flow.
	request := &saml.AuthnRequest{
		ID: "id-post-1", Version: "2.0", IssueInstant: time.Now().UTC(),
		Destination: ssoURL, Issuer: &saml.Issuer{Value: tsp.entityID},
		AssertionConsumerServiceURL: tsp.acsURL, ProtocolBinding: saml.HTTPPostBinding,
	}
	raw, err := xml.Marshal(request)
	require.NoError(t, err)
	response := samlBrowserRequest(t, server, http.MethodPost, ssoURL, cookie, "SAMLRequest="+url.QueryEscape(base64.StdEncoding.EncodeToString(raw))+"&RelayState=rs-1")
	require.Equal(t, http.StatusOK, response.StatusCode)
	html := samlReadBody(t, response)
	assert.Contains(t, html, `action="`+tsp.acsURL+`"`)
	responseValue := extractSAMLResponse(t, html)
	assert.NotEmpty(t, responseValue)
}

func TestSAMLSSORejectsInvalidRequests(t *testing.T) {
	env := newFederationEnvironment(t)
	startSAML(t, env, SAMLConfig{Enabled: true})
	tsp := newSAMLSPServer(t)
	tsp.register(t, env)
	server := newSAMLServer(t, env)
	_, cookie := samlSessionCookie(t, env)
	ssoURL := server.URL + samlSSOPath("tenant-a")

	cases := []struct {
		name       string
		param      string
		wantStatus int
		unauthed   bool
	}{
		{name: "unknown service provider", param: samlAuthnRequestParam(t, newSAMLSPServer(t), "id-x", time.Now().UTC(), ssoURL, ""), wantStatus: http.StatusBadRequest},
		{name: "expired request", param: samlAuthnRequestParam(t, tsp, "id-expired", time.Now().UTC().Add(-10*time.Minute), ssoURL, ""), wantStatus: http.StatusBadRequest},
		{name: "wrong destination", param: samlAuthnRequestParam(t, tsp, "id-dest", time.Now().UTC(), "https://evil.example/sso", ""), wantStatus: http.StatusBadRequest},
		{name: "wrong acs", param: samlAuthnRequestParam(t, tsp, "id-acs", time.Now().UTC(), ssoURL, "https://evil.example/acs"), wantStatus: http.StatusBadRequest},
		{name: "malformed base64", param: "%%%", wantStatus: http.StatusBadRequest},
		{name: "no request", param: "", wantStatus: http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := samlBrowserRequest(t, server, http.MethodGet, ssoURL+"?SAMLRequest="+url.QueryEscape(tc.param), cookie)
			assert.Equal(t, tc.wantStatus, response.StatusCode)
		})
	}

	// Unauthenticated browsers are redirected to the login page with the SSO
	// URL preserved for resumption.
	response := samlBrowserRequest(t, server, http.MethodGet, ssoURL+"?SAMLRequest="+url.QueryEscape(samlAuthnRequestParam(t, tsp, "id-anon", time.Now().UTC(), ssoURL, "")), "")
	assert.Equal(t, http.StatusFound, response.StatusCode)
	assert.Contains(t, response.Header.Get("Location"), "/login?return_url="+url.QueryEscape(ssoURL))
}

func TestSAMLSSORequiresEnabledIdP(t *testing.T) {
	env := newFederationEnvironment(t)
	server := newSAMLServer(t, env)
	response := samlBrowserRequest(t, server, http.MethodGet, server.URL+samlSSOPath("tenant-a")+"?SAMLRequest=x", "")
	assert.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
}

func TestSAMLSingleLogout(t *testing.T) {
	env := newFederationEnvironment(t)
	startSAML(t, env, SAMLConfig{Enabled: true})
	tsp := newSAMLSPServer(t)
	tsp.register(t, env)
	server := newSAMLServer(t, env)
	_, cookie := samlSessionCookie(t, env)
	sloURL := server.URL + samlSLOPath("tenant-a")

	notAfter := time.Now().UTC().Add(5 * time.Minute)
	logout := &saml.LogoutRequest{
		ID: "id-logout-1", Version: "2.0", IssueInstant: time.Now().UTC(),
		Destination: sloURL, Issuer: &saml.Issuer{Value: tsp.entityID},
		NameID:       &saml.NameID{Value: "alice", Format: samlNameIDPersistent},
		NotOnOrAfter: &notAfter,
	}
	raw, err := xml.Marshal(logout)
	require.NoError(t, err)
	var compressed bytes.Buffer
	zw, err := flate.NewWriter(&compressed, flate.DefaultCompression)
	require.NoError(t, err)
	_, err = zw.Write(raw)
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	response := samlBrowserRequest(t, server, http.MethodGet, sloURL+"?SAMLRequest="+url.QueryEscape(base64.StdEncoding.EncodeToString(compressed.Bytes())), cookie)
	require.Equal(t, http.StatusOK, response.StatusCode)
	html := samlReadBody(t, response)
	assert.Contains(t, html, `action="`+tsp.sloURL+`"`)
	responseValue := extractSAMLResponse(t, html)
	assert.NotEmpty(t, responseValue)

	// The browser session was revoked and the clearing cookie was sent.
	_, err = env.web.Authenticate(t.Context(), "", cookie)
	assert.Error(t, err)
}

func TestSAMLSingleLogoutInvalid(t *testing.T) {
	env := newFederationEnvironment(t)
	startSAML(t, env, SAMLConfig{Enabled: true})
	tsp := newSAMLSPServer(t)
	tsp.register(t, env)
	server := newSAMLServer(t, env)
	_, cookie := samlSessionCookie(t, env)
	sloURL := server.URL + samlSLOPath("tenant-a")

	notAfter := time.Now().UTC().Add(5 * time.Minute)
	logout := &saml.LogoutRequest{
		ID: "id-logout-bad", Version: "2.0", IssueInstant: time.Now().UTC(),
		Destination: sloURL, Issuer: &saml.Issuer{Value: newSAMLSPServer(t).entityID},
		NotOnOrAfter: &notAfter,
	}
	raw, err := xml.Marshal(logout)
	require.NoError(t, err)
	response := samlBrowserRequest(t, server, http.MethodGet, sloURL+"?SAMLRequest="+url.QueryEscape(base64.StdEncoding.EncodeToString(raw)), cookie)
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)

	// Expired LogoutRequest.
	logout.Issuer = &saml.Issuer{Value: tsp.entityID}
	logout.IssueInstant = time.Now().UTC().Add(-10 * time.Minute)
	raw, err = xml.Marshal(logout)
	require.NoError(t, err)
	response = samlBrowserRequest(t, server, http.MethodGet, sloURL+"?SAMLRequest="+url.QueryEscape(base64.StdEncoding.EncodeToString(raw)), cookie)
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
}

func samlBrowserRequest(t *testing.T, _ *httptest.Server, method, target, cookie string, bodies ...string) *http.Response {
	t.Helper()
	var bodyReader *bytes.Reader
	if len(bodies) > 0 {
		bodyReader = bytes.NewReader([]byte(bodies[0]))
	} else {
		bodyReader = bytes.NewReader(nil)
	}
	request, err := http.NewRequestWithContext(t.Context(), method, target, bodyReader)
	require.NoError(t, err)
	if cookie != "" {
		request.Header.Set("Cookie", cookie)
	}
	if len(bodies) > 0 {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	response, err := noRedirectClient().Do(request)
	require.NoError(t, err)
	return response
}

func samlReadBody(t *testing.T, response *http.Response) string {
	t.Helper()
	defer response.Body.Close()
	body := new(bytes.Buffer)
	_, err := body.ReadFrom(response.Body)
	require.NoError(t, err)
	return body.String()
}

func pemEncodeKey(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

func pemEncodeCert(t *testing.T, cert *x509.Certificate) []byte {
	t.Helper()
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
}

func (tsp *samlTestSP) metadataXMLNoSLO() string {
	noSLO := `    <md:SingleLogoutService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST" Location="` + tsp.sloURL + `"/>` + "\n"
	return strings.Replace(tsp.metadataXML(), noSLO, "", 1)
}

func (tsp *samlTestSP) metadataXMLRedirectSLO() string {
	redirect := `    <md:SingleLogoutService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="` + tsp.sloURL + `"/>` + "\n"
	return strings.Replace(tsp.metadataXML(), `    <md:SingleLogoutService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST" Location="`+tsp.sloURL+`"/>`+"\n", redirect, 1)
}

func TestSAMLSingleLogoutRedirectBinding(t *testing.T) {
	env := newFederationEnvironment(t)
	startSAML(t, env, SAMLConfig{Enabled: true})
	tsp := newSAMLSPServer(t)
	_, err := env.service.CreateSAMLServiceProvider(t.Context(), "tenant-a", SAMLServiceProviderInput{
		Name: "portal", EntityID: tsp.entityID, MetadataXML: tsp.metadataXMLRedirectSLO(), Status: samlSPStatusActive,
	})
	require.NoError(t, err)
	server := newSAMLServer(t, env)
	_, cookie := samlSessionCookie(t, env)
	sloURL := server.URL + samlSLOPath("tenant-a")

	notAfter := time.Now().UTC().Add(5 * time.Minute)
	logout := &saml.LogoutRequest{
		ID: "id-logout-redirect", Version: "2.0", IssueInstant: time.Now().UTC(),
		Destination: sloURL, Issuer: &saml.Issuer{Value: tsp.entityID},
		NameID:       &saml.NameID{Value: "alice", Format: samlNameIDPersistent},
		NotOnOrAfter: &notAfter,
	}
	raw, err := xml.Marshal(logout)
	require.NoError(t, err)
	var compressed bytes.Buffer
	zw, err := flate.NewWriter(&compressed, flate.DefaultCompression)
	require.NoError(t, err)
	_, err = zw.Write(raw)
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	response := samlBrowserRequest(t, server, http.MethodGet, sloURL+"?SAMLRequest="+url.QueryEscape(base64.StdEncoding.EncodeToString(compressed.Bytes())), cookie)
	require.Equal(t, http.StatusOK, response.StatusCode)
	html := samlReadBody(t, response)
	assert.Contains(t, html, `action="`+tsp.sloURL+`"`)
	assert.NotEmpty(t, extractSAMLResponse(t, html))
}

func TestSAMLKeyParsingBranches(t *testing.T) {
	env := newFederationEnvironment(t)

	// PKCS#8-encoded RSA keys parse like their PKCS#1 counterparts.
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	pkcs8, err := x509.MarshalPKCS8PrivateKey(rsaKey)
	require.NoError(t, err)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: testRSACertificate(t, rsaKey).Raw})
	parsedKey, parsedCert, err := parseSAMLKeyPair(string(keyPEM), string(certPEM))
	require.NoError(t, err)
	require.True(t, parsedKey.PublicKey.Equal(parsedCert.PublicKey))

	// Non-RSA PKCS#8 keys are rejected with a precise error.
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	ecPKCS8, err := x509.MarshalPKCS8PrivateKey(ecKey)
	require.NoError(t, err)
	_, _, err = parseSAMLKeyPair(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: ecPKCS8})), string(certPEM))
	require.ErrorContains(t, err, "must be RSA")

	// Malformed key, certificate, and unparseable DER all fail fast.
	_, _, err = parseSAMLKeyPair("not a pem", string(certPEM))
	require.ErrorContains(t, err, "invalid SAML signing key PEM")
	_, _, err = parseSAMLKeyPair(string(keyPEM), "not a pem")
	require.ErrorContains(t, err, "invalid SAML certificate PEM")
	_, _, err = parseSAMLKeyPair(string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: []byte("junk")})), string(certPEM))
	require.ErrorContains(t, err, "invalid SAML signing key PEM")
	// A service without the federation encryption key refuses to open any
	// ciphertext, even a structurally valid one.
	_, err = (&Service{}).decryptSAMLKey(federationCipherVersion + ".abc")
	require.ErrorContains(t, err, "encryption key is required")
	_, _, err = parseSAMLKeyPair(string(keyPEM), string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("junk")})))
	require.ErrorContains(t, err, "invalid SAML certificate PEM")

	// The service cipher round-trips and every decrypt failure is explicit.
	sealed, err := env.service.encryptSAMLKey("secret-pem")
	require.NoError(t, err)
	plain, err := env.service.decryptSAMLKey(sealed)
	require.NoError(t, err)
	assert.Equal(t, "secret-pem", plain)
	_, err = env.service.decryptSAMLKey("garbage")
	require.ErrorContains(t, err, "unsupported SAML key ciphertext")
	_, err = env.service.decryptSAMLKey(federationCipherVersion + ".!")
	require.ErrorContains(t, err, "invalid SAML key ciphertext")
	tampered := federationCipherVersion + "." + base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef-tampered"))
	_, err = env.service.decryptSAMLKey(tampered)
	require.ErrorContains(t, err, "failed authentication")
}

func TestSAMLSingleLogoutWithoutSLOEndpoint(t *testing.T) {
	env := newFederationEnvironment(t)
	startSAML(t, env, SAMLConfig{Enabled: true})
	tsp := newSAMLSPServer(t)
	_, err := env.service.CreateSAMLServiceProvider(t.Context(), "tenant-a", SAMLServiceProviderInput{
		Name: "portal", EntityID: tsp.entityID, MetadataXML: tsp.metadataXMLNoSLO(), Status: samlSPStatusActive,
	})
	require.NoError(t, err)
	server := newSAMLServer(t, env)
	_, cookie := samlSessionCookie(t, env)
	sloURL := server.URL + samlSLOPath("tenant-a")

	notAfter := time.Now().UTC().Add(5 * time.Minute)
	logout := &saml.LogoutRequest{
		ID: "id-logout-noslo", Version: "2.0", IssueInstant: time.Now().UTC(),
		Destination: sloURL, Issuer: &saml.Issuer{Value: tsp.entityID},
		NameID:       &saml.NameID{Value: "alice", Format: samlNameIDPersistent},
		NotOnOrAfter: &notAfter,
	}
	raw, err := xml.Marshal(logout)
	require.NoError(t, err)
	var compressed bytes.Buffer
	zw, err := flate.NewWriter(&compressed, flate.DefaultCompression)
	require.NoError(t, err)
	_, err = zw.Write(raw)
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	response := samlBrowserRequest(t, server, http.MethodGet, sloURL+"?SAMLRequest="+url.QueryEscape(base64.StdEncoding.EncodeToString(compressed.Bytes())), cookie)
	require.Equal(t, http.StatusOK, response.StatusCode)
	assert.Equal(t, "logged out", samlReadBody(t, response))
}

func TestSAMLSingleLogoutRejectsMalformedRequests(t *testing.T) {
	env := newFederationEnvironment(t)
	startSAML(t, env, SAMLConfig{Enabled: true})
	tsp := newSAMLSPServer(t)
	tsp.register(t, env)
	server := newSAMLServer(t, env)
	_, cookie := samlSessionCookie(t, env)
	sloURL := server.URL + samlSLOPath("tenant-a")

	deflate := func(raw []byte) string {
		t.Helper()
		var compressed bytes.Buffer
		zw, err := flate.NewWriter(&compressed, flate.DefaultCompression)
		require.NoError(t, err)
		_, err = zw.Write(raw)
		require.NoError(t, err)
		require.NoError(t, zw.Close())
		return base64.StdEncoding.EncodeToString(compressed.Bytes())
	}
	notAfter := time.Now().UTC().Add(5 * time.Minute)
	wrongVersion, err := xml.Marshal(&saml.LogoutRequest{
		ID: "id-v", Version: "1.1", IssueInstant: time.Now().UTC(), Destination: sloURL,
		Issuer: &saml.Issuer{Value: tsp.entityID}, NotOnOrAfter: &notAfter,
	})
	require.NoError(t, err)
	noIssuer, err := xml.Marshal(&saml.LogoutRequest{
		ID: "id-i", Version: "2.0", IssueInstant: time.Now().UTC(), Destination: sloURL,
		NotOnOrAfter: &notAfter,
	})
	require.NoError(t, err)
	wrongDestination, err := xml.Marshal(&saml.LogoutRequest{
		ID: "id-d", Version: "2.0", IssueInstant: time.Now().UTC(), Destination: "https://evil.example/slo",
		Issuer: &saml.Issuer{Value: tsp.entityID}, NotOnOrAfter: &notAfter,
	})
	require.NoError(t, err)

	cases := []struct {
		name    string
		payload string
	}{
		{name: "not xml", payload: deflate([]byte("<broken"))},
		{name: "wrong root", payload: deflate([]byte(`<?xml version="1.0"?><nope/>`))},
		{name: "wrong version", payload: deflate(wrongVersion)},
		{name: "no issuer", payload: deflate(noIssuer)},
		{name: "wrong destination", payload: deflate(wrongDestination)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := samlBrowserRequest(t, server, http.MethodGet, sloURL+"?SAMLRequest="+url.QueryEscape(tc.payload), cookie)
			assert.Equal(t, http.StatusBadRequest, response.StatusCode)
		})
	}
}

func TestSAMLSingleLogoutRequiresEnabledIdP(t *testing.T) {
	env := newFederationEnvironment(t)
	server := newSAMLServer(t, env)
	response := samlBrowserRequest(t, server, http.MethodGet, server.URL+samlSLOPath("tenant-a")+"?SAMLRequest=x", "")
	assert.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
}

func TestSAMLMetadataRequiresEnabledIdP(t *testing.T) {
	env := newFederationEnvironment(t)
	server := newSAMLServer(t, env)
	response, err := server.Client().Get(server.URL + samlMetadataPath("tenant-a"))
	require.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
}

func TestSAMLUpdateRejectsDuplicateEntityID(t *testing.T) {
	env := newFederationEnvironment(t)
	ctx := t.Context()
	first := newSAMLSPServer(t)
	second := newSAMLSPServer(t)
	registered := first.register(t, env)
	// An omitted status defaults to active, then updating onto an existing
	// entity ID violates the registry uniqueness constraint.
	_, err := env.service.CreateSAMLServiceProvider(ctx, "tenant-a", SAMLServiceProviderInput{
		Name: "portal-b", EntityID: second.entityID, MetadataXML: second.metadataXML(),
	})
	require.NoError(t, err)
	_, err = env.service.UpdateSAMLServiceProvider(ctx, "tenant-a", registered.ID, SAMLServiceProviderInput{
		Name: "portal", EntityID: second.entityID, MetadataXML: second.metadataXML(),
	})
	assert.ErrorIs(t, err, ErrSAMLSPEntityIDExists)
}

func TestSAMLRotationRequiresEnabledIdP(t *testing.T) {
	env := newFederationEnvironment(t)
	_, err := env.service.RotateSAMLSigningKey(t.Context())
	assert.ErrorIs(t, err, ErrSAMLUnavailable)
}

func TestSAMLFileKeyReadErrors(t *testing.T) {
	env := newFederationEnvironment(t)
	ctx := t.Context()
	keyFile := filepath.Join(t.TempDir(), "saml.key")
	certFile := filepath.Join(t.TempDir(), "saml.crt")

	require.NoError(t, os.WriteFile(certFile, []byte("x"), 0o600))
	err := env.service.StartSAML(ctx, SAMLConfig{Enabled: true, SigningKeyFile: keyFile, CertificateFile: certFile})
	require.ErrorContains(t, err, "read saml signing key")

	require.NoError(t, os.WriteFile(keyFile, []byte("x"), 0o600))
	require.NoError(t, os.Remove(certFile))
	err = env.service.StartSAML(ctx, SAMLConfig{Enabled: true, SigningKeyFile: keyFile, CertificateFile: certFile})
	require.ErrorContains(t, err, "read saml certificate")
}

func TestSAMLSSORespectsForwardedProto(t *testing.T) {
	env := newFederationEnvironment(t)
	startSAML(t, env, SAMLConfig{Enabled: true})
	tsp := newSAMLSPServer(t)
	tsp.register(t, env)
	server := newSAMLServer(t, env)
	httpsSSO := "https://" + strings.TrimPrefix(server.URL+samlSSOPath("tenant-a"), "http://")

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+samlSSOPath("tenant-a")+"?SAMLRequest="+url.QueryEscape(samlAuthnRequestParam(t, tsp, "id-fwd", time.Now().UTC(), httpsSSO, "")), nil)
	require.NoError(t, err)
	request.Header.Set("X-Forwarded-Proto", "https")
	response, err := noRedirectClient().Do(request)
	require.NoError(t, err)
	require.Equal(t, http.StatusFound, response.StatusCode)
	assert.True(t, strings.HasPrefix(response.Header.Get("Location"), "https://"), response.Header.Get("Location"))
}

func TestSAMLSSOPathRejectsUnsupportedMethods(t *testing.T) {
	env := newFederationEnvironment(t)
	startSAML(t, env, SAMLConfig{Enabled: true})
	server := newSAMLServer(t, env)
	// The public SSO path only accepts GET and POST; other verbs are refused
	// by the router before the binding parser runs.
	response := samlBrowserRequest(t, server, http.MethodPut, server.URL+samlSSOPath("tenant-a"), "")
	assert.Equal(t, http.StatusMethodNotAllowed, response.StatusCode)
}

func TestSAMLLoginPathNormalization(t *testing.T) {
	env := newFederationEnvironment(t)
	startSAML(t, env, SAMLConfig{Enabled: true, LoginPath: "custom-login"})
	tsp := newSAMLSPServer(t)
	tsp.register(t, env)
	server := newSAMLServer(t, env)
	ssoURL := server.URL + samlSSOPath("tenant-a")

	response := samlBrowserRequest(t, server, http.MethodGet, ssoURL+"?SAMLRequest="+url.QueryEscape(samlAuthnRequestParam(t, tsp, "id-anon2", time.Now().UTC(), ssoURL, "")), "")
	require.Equal(t, http.StatusFound, response.StatusCode)
	assert.Contains(t, response.Header.Get("Location"), "/custom-login?return_url=")
}

func TestSAMLSSOOverTLS(t *testing.T) {
	env := newFederationEnvironment(t)
	startSAML(t, env, SAMLConfig{Enabled: true})
	tsp := newSAMLSPServer(t)
	tsp.register(t, env)

	require.NoError(t, i18n.InitEmbedded(i18n.Base))
	require.NoError(t, RegisterI18n())
	respx.Install()
	router := chi.NewRouter()
	router.Use(respx.Timing, respx.Locale)
	config := huma.DefaultConfig("federation saml test", "1.0.0")
	config.Transformers = append(config.Transformers, respx.LocalizeMessage)
	api := humachi.New(router, config)
	RegisterSAMLREST(api, env.service, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	server := httptest.NewTLSServer(router)
	t.Cleanup(server.Close)
	client := server.Client()
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }

	ssoURL := server.URL + samlSSOPath("tenant-a")
	response, err := client.Get(ssoURL + "?SAMLRequest=" + url.QueryEscape(samlAuthnRequestParam(t, tsp, "id-tls", time.Now().UTC(), ssoURL, "")))
	require.NoError(t, err)
	require.Equal(t, http.StatusFound, response.StatusCode)
	assert.True(t, strings.HasPrefix(response.Header.Get("Location"), "https://"), response.Header.Get("Location"))
}

func TestSAMLSPServiceProviderLookupRequiresSetup(t *testing.T) {
	// Without a loaded IdP key or tenant scope the lookup fails closed.
	_, err := (samlSPProvider{service: &Service{}}).GetServiceProvider(httptest.NewRequest(http.MethodGet, "/", nil), "any")
	assert.ErrorIs(t, err, os.ErrNotExist)
	_, err = (samlSPProvider{service: &Service{samlCert: &x509.Certificate{}}}).GetServiceProvider(httptest.NewRequest(http.MethodGet, "/", nil), "any")
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func mustURL(raw string) url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return *u
}
