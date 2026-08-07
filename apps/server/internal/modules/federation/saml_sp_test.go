package federation

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// spLoginMetadata builds an SP metadata document for the upstream IdP registry
// whose only HTTP-POST assertion consumer is the federation callback URL.
func spLoginMetadata(t *testing.T, tsp *samlTestSP, entityID, acsURL string) string {
	t.Helper()
	certB64 := base64.StdEncoding.EncodeToString(tsp.cert.Raw)
	return fmt.Sprintf(`<md:EntityDescriptor xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata" entityID="%s">
  <md:SPSSODescriptor AuthnRequestsSigned="false" WantAssertionsSigned="true" protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <md:KeyDescriptor use="signing"><ds:KeyInfo xmlns:ds="http://www.w3.org/2000/09/xmldsig#"><ds:X509Data><ds:X509Certificate>%s</ds:X509Certificate></ds:X509Data></ds:KeyInfo></md:KeyDescriptor>
    <md:AssertionConsumerService index="0" isDefault="true" Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST" Location="%s"/>
    <md:AttributeConsumingService index="0"><md:ServiceName xml:lang="en">test</md:ServiceName><md:RequestedAttribute Name="email" NameFormat="urn:oasis:names:tc:SAML:2.0:attrname-format:basic" FriendlyName="email"/><md:RequestedAttribute Name="uid" NameFormat="urn:oasis:names:tc:SAML:2.0:attrname-format:basic" FriendlyName="uid"/></md:AttributeConsumingService>
  </md:SPSSODescriptor>
</md:EntityDescriptor>`, entityID, certB64, acsURL)
}

// TestSAMLServiceProviderLoginFlow exercises the full SP-initiated login round
// trip against the chaosplus SAML IdP acting as the upstream identity provider:
// StartSAMLLogin builds a signed redirect AuthnRequest, the IdP answers with a
// signed assertion, and CompleteSAMLLogin verifies it and provisions the user.
func TestSAMLServiceProviderLoginFlow(t *testing.T) {
	env := newFederationEnvironment(t)
	startSAML(t, env, SAMLConfig{Enabled: true})
	server := newSAMLServer(t, env)

	// The SP entity ID registered at the upstream IdP; the provider's ClientID
	// carries it through to audience validation.
	spEntityID := "https://iam.example/federation/sp"
	tsp := newSAMLSPServer(t)

	// The upstream IdP answers for the *next* SP: register it with an ACS
	// pointing at this provider's callback, then create the provider so we can
	// derive the callback URL from its generated ID.
	provider, err := env.service.CreateProvider(t.Context(), "tenant-a", ProviderInput{
		Name: "Upstream SAML IdP", ProviderType: ProviderSAML, Issuer: server.URL + samlMetadataPath("tenant-a"),
		ClientID: spEntityID, AutoProvision: true, Status: ProviderActive,
	})
	require.NoError(t, err)
	callback := "https://iam.example/federation/" + provider.ID + "/callback"

	_, err = env.service.CreateSAMLServiceProvider(t.Context(), "tenant-a", SAMLServiceProviderInput{
		Name: "upstream-sp", EntityID: spEntityID, MetadataXML: spLoginMetadata(t, tsp, spEntityID, callback), Status: samlSPStatusActive,
	})
	require.NoError(t, err)

	// Start the SP-initiated login and inspect the signed redirect request.
	redirectURL, stateCookie, err := env.service.StartSAMLLogin(t.Context(), provider.ID, "https://app.example/", callback)
	require.NoError(t, err)
	parsed, err := url.Parse(redirectURL)
	require.NoError(t, err)
	require.NotEmpty(t, parsed.Query().Get("SAMLRequest"))
	require.NotEmpty(t, parsed.Query().Get("SigAlg"))
	require.NotEmpty(t, parsed.Query().Get("Signature"))
	assert.Equal(t, server.URL+samlSSOPath("tenant-a"), parsed.Scheme+"://"+parsed.Host+parsed.Path)

	// Hand the request to the upstream IdP with an authenticated browser session.
	_, idpCookie := samlSessionCookie(t, env)
	response := samlBrowserRequest(t, server, http.MethodGet, server.URL+samlSSOPath("tenant-a")+"?SAMLRequest="+url.QueryEscape(parsed.Query().Get("SAMLRequest")), idpCookie)
	require.Equal(t, http.StatusOK, response.StatusCode)
	samlResponse := extractSAMLResponse(t, samlReadBody(t, response))

	// Complete the flow with the state cookie and the signed response.
	stateValue, err := cookieValue(stateCookie, stateCookieName)
	require.NoError(t, err)
	sessionToken, returnURL, err := env.service.CompleteSAMLLogin(t.Context(), provider.ID, samlResponse, "cp_federation_state="+stateValue, callback)
	require.NoError(t, err)
	assert.Equal(t, "https://app.example/", returnURL)
	require.NotEmpty(t, sessionToken)

	claims, err := env.web.Authenticate(t.Context(), "", env.web.SessionCookie(sessionToken))
	require.NoError(t, err)
	assert.Equal(t, "alice@example.com", claims.Email)
	assert.Equal(t, []string{"saml"}, claims.AMR)

	var link identityLinkRow
	require.NoError(t, env.db.NewSelect().Model(&link).Where("provider_id = ?", provider.ID).Scan(t.Context()))
	assert.Equal(t, claims.Subject, link.PrincipalID)
	assert.Equal(t, "alice@example.com", link.Email)
}

// TestSAMLServiceProviderLoginDenials covers the failure modes of the SP flow.
func TestSAMLServiceProviderLoginDenials(t *testing.T) {
	env := newFederationEnvironment(t)
	startSAML(t, env, SAMLConfig{Enabled: true})
	server := newSAMLServer(t, env)

	// A non-SAML provider is rejected by the SP flow.
	oidc, err := env.service.CreateProvider(t.Context(), "tenant-a", ProviderInput{
		Name: "OIDC Only", ProviderType: ProviderOIDC, Issuer: "https://oidc.example", ClientID: "app",
		AutoProvision: true, Status: ProviderActive,
	})
	require.NoError(t, err)
	_, _, err = env.service.StartSAMLLogin(t.Context(), oidc.ID, "https://app.example/", "https://iam.example/federation/"+oidc.ID+"/callback")
	assert.ErrorIs(t, err, ErrInvalidProvider)

	// A SAML provider whose metadata is unreachable fails fast at start.
	missing, err := env.service.CreateProvider(t.Context(), "tenant-a", ProviderInput{
		Name: "Missing Metadata", ProviderType: ProviderSAML, Issuer: server.URL + "/nope/metadata",
		ClientID: "sp", AutoProvision: true, Status: ProviderActive,
	})
	require.NoError(t, err)
	_, _, err = env.service.StartSAMLLogin(t.Context(), missing.ID, "https://app.example/", "https://iam.example/federation/"+missing.ID+"/callback")
	assert.ErrorIs(t, err, ErrSAMLResponse)

	// A SAML provider with a callback that is not this provider's path is
	// rejected before any upstream call.
	samlProvider, err := env.service.CreateProvider(t.Context(), "tenant-a", ProviderInput{
		Name: "Upstream SAML IdP", ProviderType: ProviderSAML, Issuer: server.URL + samlMetadataPath("tenant-a"),
		ClientID: "sp", AutoProvision: true, Status: ProviderActive,
	})
	require.NoError(t, err)
	_, _, err = env.service.StartSAMLLogin(t.Context(), samlProvider.ID, "https://app.example/", "https://evil.example/callback")
	assert.ErrorIs(t, err, ErrInvalidProvider)

	// A bogus SAMLResponse with a valid state cookie is rejected.
	cb := "https://iam.example/federation/" + samlProvider.ID + "/callback"
	redirectURL, stateCookie, err := env.service.StartSAMLLogin(t.Context(), samlProvider.ID, "https://app.example/", cb)
	require.NoError(t, err)
	require.NotEmpty(t, redirectURL)
	stateValue, err := cookieValue(stateCookie, stateCookieName)
	require.NoError(t, err)
	_, _, err = env.service.CompleteSAMLLogin(t.Context(), samlProvider.ID, "bm90LWEtc2FtbC1yZXNwb25zZQ", "cp_federation_state="+stateValue, cb)
	assert.ErrorIs(t, err, ErrSAMLResponse)

	// A missing state cookie is rejected like the OIDC flow.
	_, _, err = env.service.CompleteSAMLLogin(t.Context(), samlProvider.ID, "dGVzdA", "", cb)
	assert.ErrorIs(t, err, ErrOIDCState)
}

// TestSAMLServiceProviderLoginHTTPFlow drives the full browser flow through the
// public REST endpoints: the GET /start handler dispatches a SAML provider to
// StartSAMLLogin, and the POST /callback completes the assertion flow.
func TestSAMLServiceProviderLoginHTTPFlow(t *testing.T) {
	env := newFederationEnvironment(t)
	startSAML(t, env, SAMLConfig{Enabled: true})
	idpServer := newSAMLServer(t, env)

	require.NoError(t, i18n.InitEmbedded(i18n.Base))
	require.NoError(t, RegisterI18n())
	respx.Install()
	router := chi.NewRouter()
	router.Use(respx.Timing, respx.Locale)
	config := huma.DefaultConfig("saml sp browser test", "1.0.0")
	config.Transformers = append(config.Transformers, respx.LocalizeMessage)
	api := humachi.New(router, config)
	RegisterBrowserREST(api, env.service)
	fedServer := httptest.NewServer(router)
	t.Cleanup(fedServer.Close)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	spEntityID := "https://iam.example/federation/sp"
	tsp := newSAMLSPServer(t)
	provider, err := env.service.CreateProvider(t.Context(), "tenant-a", ProviderInput{
		Name: "Upstream SAML IdP", ProviderType: ProviderSAML, Issuer: idpServer.URL + samlMetadataPath("tenant-a"),
		ClientID: spEntityID, AutoProvision: true, Status: ProviderActive,
	})
	require.NoError(t, err)
	callback := fedServer.URL + "/federation/" + provider.ID + "/callback"
	_, err = env.service.CreateSAMLServiceProvider(t.Context(), "tenant-a", SAMLServiceProviderInput{
		Name: "upstream-sp", EntityID: spEntityID, MetadataXML: spLoginMetadata(t, tsp, spEntityID, callback), Status: samlSPStatusActive,
	})
	require.NoError(t, err)

	// GET /start redirects the browser to the upstream IdP with a signed request.
	startURL := fedServer.URL + "/federation/" + provider.ID + "/start?return_url=" + url.QueryEscape("https://app.example/")
	response, err := client.Get(startURL)
	require.NoError(t, err)
	require.Equal(t, http.StatusFound, response.StatusCode)
	location, err := url.Parse(response.Header.Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, idpServer.URL+samlSSOPath("tenant-a"), location.Scheme+"://"+location.Host+location.Path)
	require.NotEmpty(t, location.Query().Get("SAMLRequest"))
	require.NotEmpty(t, location.Query().Get("Signature"))
	var stateCookie *http.Cookie
	for _, cookie := range response.Cookies() {
		if cookie.Name == stateCookieName {
			stateCookie = cookie
		}
	}
	require.NotNil(t, stateCookie)
	require.NoError(t, response.Body.Close())

	// The upstream IdP answers with a signed assertion for the authenticated user.
	_, idpCookie := samlSessionCookie(t, env)
	idpResponse := samlBrowserRequest(t, idpServer, http.MethodGet, idpServer.URL+samlSSOPath("tenant-a")+"?SAMLRequest="+url.QueryEscape(location.Query().Get("SAMLRequest")), idpCookie)
	require.Equal(t, http.StatusOK, idpResponse.StatusCode)
	samlResponse := extractSAMLResponse(t, samlReadBody(t, idpResponse))

	// POST /callback completes the flow and sets the browser session.
	form := url.Values{"SAMLResponse": {samlResponse}}.Encode()
	callbackRequest, err := http.NewRequestWithContext(t.Context(), http.MethodPost, fedServer.URL+"/federation/"+provider.ID+"/callback", strings.NewReader(form))
	require.NoError(t, err)
	callbackRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	callbackRequest.Header.Set("Cookie", stateCookieName+"="+stateCookie.Value)
	callbackResponse, err := client.Do(callbackRequest)
	require.NoError(t, err)
	defer callbackResponse.Body.Close()
	require.Equal(t, http.StatusFound, callbackResponse.StatusCode)
	assert.Equal(t, "https://app.example/", callbackResponse.Header.Get("Location"))
	var session *http.Cookie
	for _, cookie := range callbackResponse.Cookies() {
		if cookie.Name == env.web.SessionCookieName() {
			session = cookie
		}
	}
	require.NotNil(t, session)
	claims, err := env.web.Authenticate(t.Context(), "", env.web.SessionCookie(session.Value))
	require.NoError(t, err)
	assert.Equal(t, "alice@example.com", claims.Email)
}
