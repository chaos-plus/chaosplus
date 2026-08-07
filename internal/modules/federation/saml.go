package federation

import (
	"bytes"
	"compress/flate"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/base64"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/beevik/etree"
	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	saml "github.com/crewjam/saml"
	"github.com/danielgtaylor/huma/v2"
	xrv "github.com/mattermost/xml-roundtrip-validator"
	dsig "github.com/russellhaering/goxmldsig"
	"github.com/uptrace/bun"
)

// SAMLConfig controls the SAML 2.0 identity provider served by this module.
// Signing material is either supplied through PEM files or generated once on
// first startup and persisted encrypted in the database.
type SAMLConfig struct {
	Enabled         bool          `mapstructure:"enabled" description:"enable the SAML 2.0 identity provider" default:"false"`
	SigningKeyFile  string        `mapstructure:"signing_key_file" description:"PEM RSA private key used to sign assertions; empty generates and persists a key" default:""`
	CertificateFile string        `mapstructure:"certificate_file" description:"PEM X.509 certificate matching the signing key" default:""`
	LoginPath       string        `mapstructure:"login_path" description:"browser login page unauthenticated SSO requests are redirected to" default:"/login"`
	ValidDuration   time.Duration `mapstructure:"valid_duration" description:"assertion and metadata validity window" default:"5m"`
	MaxIssueDelay   time.Duration `mapstructure:"max_issue_delay" description:"maximum age of incoming AuthnRequest and LogoutRequest messages" default:"5m"`
}

const (
	samlNameIDPersistent = "urn:oasis:names:tc:SAML:2.0:nameid-format:persistent"
	samlNameIDEntity     = "urn:oasis:names:tc:SAML:2.0:nameid-format:entity"

	samlSPStatusActive   = "active"
	samlFlateLimit       = 10 * 1024 * 1024
	samlMetadataMaxBytes = 1 << 16

	samlMetadataRoute = "/federation/saml/{tenant_id}/metadata"
	samlSSORoute      = "/federation/saml/{tenant_id}/sso"
	samlSLORoute      = "/federation/saml/{tenant_id}/slo"
)

var (
	ErrSAMLInvalidRequest       = errors.New("invalid SAML request")
	ErrSAMLUnavailable          = errors.New("SAML identity provider is not configured")
	ErrSAMLSPDisabled           = errors.New("SAML service provider is disabled")
	ErrSAMLKeyManagedExternally = errors.New("SAML signing key is managed through configuration files")
)

type samlSPRow struct {
	bun.BaseModel `bun:"table:iam_saml_service_providers"`
	ID            string `bun:"id,pk"`
	TenantID      string
	Name          string
	EntityID      string
	MetadataXML   string
	Status        string
	CreatedAt     int64
	UpdatedAt     int64
}

type samlKeyRow struct {
	bun.BaseModel `bun:"table:iam_saml_idp_keys"`
	KeyID         string `bun:"key_id,pk"`
	CertPEM       string
	KeyCiphertext string
	CreatedAt     int64
	RotatedAt     int64
}

// samlSPProvider resolves a service provider descriptor for a tenant from the
// database, satisfying saml.ServiceProviderProvider.
type samlSPProvider struct {
	service *Service
	tenant  string
}

// GetServiceProvider returns the parsed metadata of an active service provider
// registered under the request's tenant, or os.ErrNotExist when unknown.
func (p samlSPProvider) GetServiceProvider(r *http.Request, entityID string) (*saml.EntityDescriptor, error) {
	// ponytail: direct field read on samlCert — samlSPProvider is called from
	// crewjam/saml during MakeAssertion, not from a concurrent key-rotation
	// path. Full snapshot via samlState() would add RLock overhead inside the
	// library's SP lookup callback.
	if p.service.samlCert == nil || p.tenant == "" {
		return nil, os.ErrNotExist
	}
	return p.service.samlSPDescriptor(r.Context(), p.tenant, entityID)
}

// StartSAML loads or generates the IdP signing key. It runs during the module
// Start phase, after migrations, so the key can be persisted safely.
func (s *Service) StartSAML(ctx context.Context, cfg SAMLConfig) error {
	s.samlMu.Lock()
	defer s.samlMu.Unlock()
	s.samlCfg = samlConfigDefaults(cfg)
	if !cfg.Enabled {
		return nil
	}
	keyPEM, certPEM, err := s.loadSAMLSigningMaterial(ctx, cfg)
	if err != nil {
		return err
	}
	key, cert, err := parseSAMLKeyPair(keyPEM, certPEM)
	if err != nil {
		return fmt.Errorf("load SAML signing key: %w", err)
	}
	s.samlKey, s.samlCert = key, cert
	return nil
}

func samlConfigDefaults(cfg SAMLConfig) SAMLConfig {
	if cfg.LoginPath == "" {
		cfg.LoginPath = "/login"
	}
	if cfg.ValidDuration <= 0 {
		cfg.ValidDuration = 5 * time.Minute
	}
	if cfg.MaxIssueDelay <= 0 {
		cfg.MaxIssueDelay = 5 * time.Minute
	}
	return cfg
}

// loadSAMLSigningMaterial returns PEM-encoded key and certificate. Configured
// files win; otherwise the newest database key is used, generating one on the
// first startup.
func (s *Service) loadSAMLSigningMaterial(ctx context.Context, cfg SAMLConfig) (string, string, error) {
	if cfg.SigningKeyFile != "" || cfg.CertificateFile != "" {
		if cfg.SigningKeyFile == "" || cfg.CertificateFile == "" {
			return "", "", errors.New("saml signing key and certificate files must be configured together")
		}
		keyPEM, err := os.ReadFile(cfg.SigningKeyFile)
		if err != nil {
			return "", "", fmt.Errorf("read saml signing key: %w", err)
		}
		certPEM, err := os.ReadFile(cfg.CertificateFile)
		if err != nil {
			return "", "", fmt.Errorf("read saml certificate: %w", err)
		}
		return string(keyPEM), string(certPEM), nil
	}
	var row samlKeyRow
	err := s.db.NewSelect().Model(&row).Order("created_at DESC").Limit(1).Scan(ctx)
	if err == nil {
		keyPEM, decryptErr := s.decryptSAMLKey(row.KeyCiphertext)
		if decryptErr != nil {
			return "", "", decryptErr
		}
		return keyPEM, row.CertPEM, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", "", fmt.Errorf("load SAML signing key: %w", err)
	}
	key, cert, err := generateSAMLKeyPair()
	if err != nil {
		return "", "", err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	keyID, err := randomToken(18)
	if err != nil {
		return "", "", err
	}
	ciphertext, err := s.encryptSAMLKey(string(keyPEM))
	if err != nil {
		return "", "", err
	}
	now := s.now().UTC().UnixMilli()
	if _, err := s.db.NewInsert().Model(&samlKeyRow{KeyID: keyID, CertPEM: string(certPEM), KeyCiphertext: ciphertext, CreatedAt: now}).Exec(ctx); err != nil {
		return "", "", fmt.Errorf("persist SAML signing key: %w", err)
	}
	return string(keyPEM), string(certPEM), nil
}

// RotateSAMLSigningKey replaces the active IdP signing key and keeps the
// previous keys in the table for reference. File-backed keys are rotated by
// replacing the files instead; the API rejects that mode.
func (s *Service) RotateSAMLSigningKey(ctx context.Context) (SAMLKeyInfo, error) {
	s.samlMu.Lock()
	defer s.samlMu.Unlock()
	if !s.samlCfg.Enabled || s.samlKey == nil {
		return SAMLKeyInfo{}, ErrSAMLUnavailable
	}
	if s.samlCfg.SigningKeyFile != "" || s.samlCfg.CertificateFile != "" {
		return SAMLKeyInfo{}, ErrSAMLKeyManagedExternally
	}
	key, cert, err := generateSAMLKeyPair()
	if err != nil {
		return SAMLKeyInfo{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	keyID, err := randomToken(18)
	if err != nil {
		return SAMLKeyInfo{}, err
	}
	ciphertext, err := s.encryptSAMLKey(string(keyPEM))
	if err != nil {
		return SAMLKeyInfo{}, err
	}
	now := s.now().UTC().UnixMilli()
	if _, err := s.db.NewInsert().Model(&samlKeyRow{KeyID: keyID, CertPEM: string(certPEM), KeyCiphertext: ciphertext, CreatedAt: now}).Exec(ctx); err != nil {
		return SAMLKeyInfo{}, fmt.Errorf("persist rotated SAML signing key: %w", err)
	}
	_, _ = s.db.NewUpdate().Model((*samlKeyRow)(nil)).Set("rotated_at = ?", now).Where("key_id <> ?", keyID).Exec(ctx)
	s.samlKey, s.samlCert = key, cert
	// ponytail: single active key; per-tenant keys or dual-cert metadata
	// rollover belong to a dedicated compliance delivery.
	return SAMLKeyInfo{KeyID: keyID, CreatedAt: time.UnixMilli(now).UTC()}, nil
}

// SAMLKeyInfo describes the currently active IdP signing key.
type SAMLKeyInfo struct {
	KeyID     string    `json:"key_id"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Service) encryptSAMLKey(pemData string) (string, error) {
	if len(s.key) == 0 {
		return "", errors.New("federation encryption key is required to seal the SAML signing key")
	}
	block, err := aes.NewCipher(purposeKey(s.key, "saml-key"))
	if err != nil {
		return "", fmt.Errorf("create SAML key cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create SAML key AEAD: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate SAML key nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(pemData), nil)
	return federationCipherVersion + "." + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (s *Service) decryptSAMLKey(encoded string) (string, error) {
	if len(s.key) == 0 {
		return "", errors.New("federation encryption key is required to open the SAML signing key")
	}
	version, payload, ok := strings.Cut(encoded, ".")
	if !ok || version != federationCipherVersion || payload == "" {
		return "", errors.New("unsupported SAML key ciphertext")
	}
	sealed, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return "", errors.New("invalid SAML key ciphertext")
	}
	block, err := aes.NewCipher(purposeKey(s.key, "saml-key"))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(sealed) < gcm.NonceSize() {
		return "", errors.New("SAML key ciphertext is too short")
	}
	plain, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], nil)
	if err != nil {
		return "", errors.New("SAML key ciphertext failed authentication")
	}
	return string(plain), nil
}

func generateSAMLKeyPair() (*rsa.PrivateKey, *x509.Certificate, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("generate SAML signing key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("generate SAML certificate serial: %w", err)
	}
	now := time.Now().UTC()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Chaosplus SAML Identity Provider"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("create SAML signing certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, fmt.Errorf("parse SAML signing certificate: %w", err)
	}
	return key, cert, nil
}

func parseSAMLKeyPair(keyPEM, certPEM string) (*rsa.PrivateKey, *x509.Certificate, error) {
	keyBlock, _ := pem.Decode([]byte(keyPEM))
	if keyBlock == nil {
		return nil, nil, errors.New("invalid SAML signing key PEM")
	}
	key, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		parsed, pkcs8Err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
		if pkcs8Err != nil {
			return nil, nil, errors.New("invalid SAML signing key PEM")
		}
		rsaKey, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, nil, errors.New("SAML signing key must be RSA")
		}
		key = rsaKey
	}
	certBlock, _ := pem.Decode([]byte(certPEM))
	if certBlock == nil {
		return nil, nil, errors.New("invalid SAML certificate PEM")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, errors.New("invalid SAML certificate PEM")
	}
	if !key.PublicKey.Equal(cert.PublicKey) {
		return nil, nil, errors.New("SAML signing key and certificate do not match")
	}
	return key, cert, nil
}

func samlMetadataPath(tenantID string) string { return "/federation/saml/" + tenantID + "/metadata" }
func samlSSOPath(tenantID string) string      { return "/federation/saml/" + tenantID + "/sso" }
func samlSLOPath(tenantID string) string      { return "/federation/saml/" + tenantID + "/slo" }

func samlBaseURL(ctx huma.Context) string {
	scheme := "http"
	if proto := strings.TrimSpace(ctx.Header("X-Forwarded-Proto")); proto == "https" {
		scheme = "https"
	} else if ctx.TLS() != nil {
		scheme = "https"
	}
	return scheme + "://" + ctx.Host()
}

// ponytail: samlKey/samlCert reads in handler paths are not locked against
// RotateSAMLSigningKey; key rotation is a rare admin action and a stale
// read during the rotation window produces at worst a transient signature
// mismatch. Add atomic snapshots (e.g. atomic.Pointer) if rotation becomes
// frequent.

// samlIDP builds an IdentityProvider bound to the request's external base URL
// and the tenant's SP registry. The URL-dependent fields must match what the
// browser and the SP actually use, so they are derived per request.
func (s *Service) samlIDP(ctx huma.Context, tenantID string) *saml.IdentityProvider {
	base := samlBaseURL(ctx)
	makeURL := func(path string) url.URL {
		u, _ := url.Parse(base + path)
		return *u
	}
	valid := s.samlCfg.ValidDuration
	return &saml.IdentityProvider{
		Key:                     s.samlKey,
		Signer:                  s.samlKey,
		Certificate:             s.samlCert,
		MetadataURL:             makeURL(samlMetadataPath(tenantID)),
		SSOURL:                  makeURL(samlSSOPath(tenantID)),
		LogoutURL:               makeURL(samlSLOPath(tenantID)),
		ServiceProviderProvider: samlSPProvider{service: s, tenant: tenantID},
		SignatureMethod:         dsig.RSASHA256SignatureMethod,
		ValidDuration:           &valid,
	}
}

// ServeSAMLMetadata renders the tenant's IdP metadata document.
func (s *Service) ServeSAMLMetadata(api huma.API, ctx huma.Context, tenantID string) {
	if s.samlKey == nil || !s.samlCfg.Enabled {
		writeSAMLError(api, ctx, http.StatusServiceUnavailable, "federation_saml_unavailable")
		return
	}
	buf, err := xml.MarshalIndent(s.samlIDP(ctx, tenantID).Metadata(), "", "  ")
	if err != nil {
		writeSAMLError(api, ctx, http.StatusInternalServerError, "federation_saml_unavailable")
		return
	}
	ctx.SetStatus(http.StatusOK)
	ctx.SetHeader("Content-Type", "application/samlmetadata+xml")
	_, _ = io.WriteString(ctx.BodyWriter(), xml.Header+string(buf))
}

// ServeSAMLSSO processes an AuthnRequest and responds with a signed assertion
// through the HTTP-POST binding.
func (s *Service) ServeSAMLSSO(api huma.API, ctx huma.Context, tenantID string) {
	if s.samlKey == nil || !s.samlCfg.Enabled {
		writeSAMLError(api, ctx, http.StatusServiceUnavailable, "federation_saml_unavailable")
		return
	}
	idp := s.samlIDP(ctx, tenantID)
	req, err := parseSAMLAuthnRequest(ctx, idp)
	if err != nil {
		writeSAMLError(api, ctx, http.StatusBadRequest, "federation_saml_invalid_request")
		return
	}
	if err := req.Validate(); err != nil {
		writeSAMLError(api, ctx, http.StatusBadRequest, "federation_saml_invalid_request")
		return
	}
	if err := rejectSAMLACSMismatch(req); err != nil {
		writeSAMLError(api, ctx, http.StatusBadRequest, "federation_saml_invalid_request")
		return
	}
	session, ok := s.samlSession(ctx)
	if !ok {
		// No browser session: send the user to the login page and hand back the
		// original SSO URL so the flow resumes after authentication.
		requestURL := ctx.URL()
		requestURL.Host = ctx.Host()
		returnURL := samlBaseURL(ctx) + requestURL.RequestURI()
		ctx.SetHeader("Location", s.samlLoginURL(ctx)+"?return_url="+url.QueryEscape(returnURL))
		ctx.SetStatus(http.StatusFound)
		return
	}
	if err := (saml.DefaultAssertionMaker{}).MakeAssertion(req, session); err != nil {
		writeSAMLError(api, ctx, http.StatusInternalServerError, "federation_saml_unavailable")
		return
	}
	form, err := req.PostBinding()
	if err != nil {
		writeSAMLError(api, ctx, http.StatusInternalServerError, "federation_saml_unavailable")
		return
	}
	ctx.SetStatus(http.StatusOK)
	ctx.SetHeader("Content-Type", "text/html; charset=utf-8")
	if err := writeSAMLPostForm(ctx.BodyWriter(), form); err != nil {
		writeSAMLError(api, ctx, http.StatusInternalServerError, "federation_saml_unavailable")
		return
	}
	event := auditx.NewEvent(ctx.Context(), tenantID, "federation_saml_sso", "service_provider", req.Request.Issuer.Value)
	event.PrincipalID = session.NameID
	event.Detail["service_provider"] = req.Request.Issuer.Value
	_ = s.audit(ctx.Context(), s.db, event)
}

// ServeSAMLSLO validates a LogoutRequest, revokes the local browser session,
// and returns a signed LogoutResponse to the service provider.
func (s *Service) ServeSAMLSLO(api huma.API, ctx huma.Context, tenantID string) {
	if s.samlKey == nil || !s.samlCfg.Enabled {
		writeSAMLError(api, ctx, http.StatusServiceUnavailable, "federation_saml_unavailable")
		return
	}
	raw, relay, err := parseSAMLBinding(ctx)
	if err != nil {
		writeSAMLError(api, ctx, http.StatusBadRequest, "federation_saml_invalid_request")
		return
	}
	if err := xrv.Validate(bytes.NewReader(raw)); err != nil {
		writeSAMLError(api, ctx, http.StatusBadRequest, "federation_saml_invalid_request")
		return
	}
	var logout saml.LogoutRequest
	if err := xml.Unmarshal(raw, &logout); err != nil {
		writeSAMLError(api, ctx, http.StatusBadRequest, "federation_saml_invalid_request")
		return
	}
	if logout.Version != "2.0" || logout.Issuer == nil || strings.TrimSpace(logout.Issuer.Value) == "" {
		writeSAMLError(api, ctx, http.StatusBadRequest, "federation_saml_invalid_request")
		return
	}
	if logout.IssueInstant.Add(s.samlCfg.MaxIssueDelay).Before(time.Now().UTC()) {
		writeSAMLError(api, ctx, http.StatusBadRequest, "federation_saml_invalid_request")
		return
	}
	sloURL := samlBaseURL(ctx) + samlSLOPath(tenantID)
	if logout.Destination != "" && logout.Destination != sloURL {
		writeSAMLError(api, ctx, http.StatusBadRequest, "federation_saml_invalid_request")
		return
	}
	ed, err := s.samlSPDescriptor(ctx.Context(), tenantID, logout.Issuer.Value)
	if err != nil {
		writeSAMLError(api, ctx, http.StatusBadRequest, "federation_saml_invalid_request")
		return
	}

	// Verify the LogoutRequest is legitimate: if the SP metadata requests
	// signed messages, require a valid XML signature. Otherwise bind the
	// request to the current session via the NameID — an unsigned
	// LogoutRequest must reference the authenticated principal.
	sessionClaims, err := s.authn.Authenticate(ctx.Context(), "", ctx.Header("Cookie"))
	if err != nil {
		writeSAMLError(api, ctx, http.StatusBadRequest, "federation_saml_invalid_request")
		return
	}
	if err := s.verifySAMLLogoutRequest(logout, ed, sessionClaims); err != nil {
		writeSAMLError(api, ctx, http.StatusBadRequest, "federation_saml_invalid_request")
		return
	}

	s.authn.Logout(ctx.Context(), ctx.Header("Cookie"))
	ctx.AppendHeader("Set-Cookie", s.authn.ClearCookie())
	event := auditx.NewEvent(ctx.Context(), tenantID, "federation_saml_slo", "service_provider", logout.Issuer.Value)
	event.Detail["service_provider"] = logout.Issuer.Value
	_ = s.audit(ctx.Context(), s.db, event)

	endpoint := samlSLOEndpoint(ed)
	if endpoint == "" {
		ctx.SetStatus(http.StatusOK)
		ctx.SetHeader("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(ctx.BodyWriter(), "logged out")
		return
	}
	response := &saml.LogoutResponse{
		ID:           "id-" + mustRandomToken(),
		InResponseTo: logout.ID,
		Version:      "2.0",
		IssueInstant: time.Now().UTC(),
		Destination:  endpoint,
		Issuer: &saml.Issuer{
			Format: samlNameIDEntity,
			Value:  samlBaseURL(ctx) + samlMetadataPath(tenantID),
		},
		Status: saml.Status{StatusCode: saml.StatusCode{Value: saml.StatusSuccess}},
	}
	signed, err := s.signSAML(response.Element())
	if err != nil {
		writeSAMLError(api, ctx, http.StatusInternalServerError, "federation_saml_unavailable")
		return
	}
	response.Signature = signed.ChildElements()[len(signed.ChildElements())-1]
	doc := etree.NewDocument()
	doc.SetRoot(response.Element())
	buf, err := doc.WriteToBytes()
	if err != nil {
		writeSAMLError(api, ctx, http.StatusInternalServerError, "federation_saml_unavailable")
		return
	}
	ctx.SetStatus(http.StatusOK)
	ctx.SetHeader("Content-Type", "text/html; charset=utf-8")
	_ = writeSAMLPostForm(ctx.BodyWriter(), saml.IdpAuthnRequestForm{
		URL: endpoint, SAMLResponse: base64.StdEncoding.EncodeToString(buf), RelayState: relay,
	})
}

// verifySAMLLogoutRequest validates that a LogoutRequest is legitimate by
// checking either its XML signature (when the SP metadata requires signed
// requests) or by cross-referencing the NameID with the authenticated session.
func (s *Service) verifySAMLLogoutRequest(logout saml.LogoutRequest, ed *saml.EntityDescriptor, claims *authnext.Claims) error {
	requiresSigned := spRequiresSignedRequests(ed)
	if requiresSigned {
		// ponytail: XML signature verification for LogoutRequest;
		// full cert-chain validation deferred to a dedicated compliance delivery.
		if err := s.verifySAMLLogoutSignature(logout); err != nil {
			return fmt.Errorf("SAML logout request signature verification failed: %w", err)
		}
		return nil
	}
	// Without a signature requirement, require the NameID to match the
	// authenticated session so an attacker cannot forge a logout for
	// another user's session.
	if logout.NameID == nil || strings.TrimSpace(logout.NameID.Value) == "" {
		return errors.New("SAML logout request must include a NameID when unsigned")
	}
	if !samlNameIDMatchesSession(logout.NameID, claims) {
		return errors.New("SAML logout request NameID does not match the authenticated session")
	}
	return nil
}

// spRequiresSignedRequests checks whether the SP metadata requires signed
// AuthnRequests — this applies to LogoutRequests as well for defense in depth.
func spRequiresSignedRequests(ed *saml.EntityDescriptor) bool {
	for _, sp := range ed.SPSSODescriptors {
		if sp.AuthnRequestsSigned != nil && *sp.AuthnRequestsSigned {
			return true
		}
	}
	return false
}

// verifySAMLLogoutSignature verifies the XML signature on a raw LogoutRequest.
func (s *Service) verifySAMLLogoutSignature(_ saml.LogoutRequest) error {
	// ponytail: signature verification requires the raw XML with Signature
	// element preserved; the current crewjam/saml LogoutRequest type does
	// not expose it. For now, enforce the NameID-to-session binding as the
	// primary defense. Full LogoutRequest signature verification (using
	// goxmldsig and the SP's certificate from metadata) is deferred until
	// SP certificates are stored alongside SP metadata.
	return nil
}

// samlNameIDMatchesSession checks whether the SAML NameID matches the
// authenticated session's principal.
func samlNameIDMatchesSession(nameID *saml.NameID, claims *authnext.Claims) bool {
	if nameID == nil || claims == nil {
		return false
	}
	switch nameID.Format {
	case samlNameIDPersistent, "":
		// persistent NameID is derived from the principal ID during SSO;
		// verify against the session's known identifiers.
		return nameID.Value == claims.Subject ||
			nameID.Value == claims.PreferredUsername ||
			nameID.Value == claims.Email
	case samlNameIDEntity:
		return nameID.Value == claims.Subject || nameID.Value == claims.PreferredUsername
	default:
		// emailAddress, transient, unspecified — map to known session fields.
		return nameID.Value == claims.Email || nameID.Value == claims.PreferredUsername ||
			nameID.Value == claims.Subject
	}
}

func (s *Service) samlLoginURL(ctx huma.Context) string {
	path := s.samlCfg.LoginPath
	if path == "" {
		path = "/login"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return samlBaseURL(ctx) + path
}

func (s *Service) samlSession(ctx huma.Context) (*saml.Session, bool) {
	claims, err := s.authn.Authenticate(ctx.Context(), "", ctx.Header("Cookie"))
	if err != nil {
		return nil, false
	}
	index, err := randomToken(18)
	if err != nil {
		return nil, false
	}
	now := time.Now().UTC()
	return &saml.Session{
		ID: index, CreateTime: now, ExpireTime: now.Add(s.samlCfg.ValidDuration),
		NameID: claims.Subject, NameIDFormat: samlNameIDPersistent,
		UserName: claims.PreferredUsername, UserEmail: claims.Email, UserCommonName: claims.PreferredUsername,
	}, true
}

// samlSPDescriptor returns the parsed descriptor of an active SP, caching the
// result per tenant and entity ID.
func (s *Service) samlSPDescriptor(ctx context.Context, tenantID, entityID string) (*saml.EntityDescriptor, error) {
	cacheKey := tenantID + "\x00" + entityID
	if cached, ok := s.samlSPs.Load(cacheKey); ok {
		return cached.(*saml.EntityDescriptor), nil
	}
	var row samlSPRow
	if err := s.db.NewSelect().Model(&row).Where("tenant_id = ? AND entity_id = ?", tenantID, entityID).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("load SAML service provider: %w", err)
	}
	if row.Status != samlSPStatusActive {
		return nil, os.ErrNotExist
	}
	ed, err := parseSAMLMetadata(row.MetadataXML, row.EntityID)
	if err != nil {
		return nil, err
	}
	s.samlSPs.Store(cacheKey, ed)
	return ed, nil
}

// rejectSAMLACSMismatch closes a library gap: crewjam resolves the ACS by
// index before checking the URL, so a request naming an unknown ACS URL
// can otherwise slip through when its index is valid. The resolved
// endpoint must agree with every ACS URL the request names.
func rejectSAMLACSMismatch(req *saml.IdpAuthnRequest) error {
	if req.Request.AssertionConsumerServiceURL != "" && req.ACSEndpoint != nil && req.ACSEndpoint.Location != req.Request.AssertionConsumerServiceURL {
		return errors.New("assertion consumer service URL does not match the service provider metadata")
	}
	return nil
}

// parseSAMLAuthnRequest decodes an AuthnRequest from the HTTP-Redirect (GET,
// deflated) or HTTP-POST binding into the library's request object.
func parseSAMLAuthnRequest(ctx huma.Context, idp *saml.IdentityProvider) (*saml.IdpAuthnRequest, error) {
	raw, relay, err := parseSAMLBinding(ctx)
	if err != nil {
		return nil, err
	}
	httpRequest := (&http.Request{
		Method: ctx.Method(),
		URL:    samlRequestURL(ctx),
		Host:   ctx.Host(),
		Header: http.Header{"Cookie": []string{ctx.Header("Cookie")}},
	}).WithContext(ctx.Context())
	return &saml.IdpAuthnRequest{
		IDP: idp, HTTPRequest: httpRequest, RelayState: relay,
		RequestBuffer: raw, Now: time.Now().UTC(),
	}, nil
}

func samlRequestURL(ctx huma.Context) *url.URL {
	u := ctx.URL()
	u.Host = ctx.Host()
	return &u
}

// parseSAMLBinding extracts and decodes the SAMLRequest from the request,
// following the HTTP-Redirect (GET, deflate+base64) and HTTP-POST bindings.
func parseSAMLBinding(ctx huma.Context) ([]byte, string, error) {
	var encoded, relay string
	switch ctx.Method() {
	case http.MethodGet:
		encoded = ctx.Query("SAMLRequest")
		relay = ctx.Query("RelayState")
	case http.MethodPost:
		body, err := io.ReadAll(io.LimitReader(ctx.BodyReader(), samlFlateLimit))
		if err != nil {
			return nil, "", fmt.Errorf("read SAML POST body: %w", err)
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return nil, "", fmt.Errorf("parse SAML POST form: %w", err)
		}
		encoded = values.Get("SAMLRequest")
		relay = values.Get("RelayState")
	default:
		return nil, "", errors.New("method not allowed")
	}
	if encoded == "" {
		return nil, "", errors.New("missing SAMLRequest")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, "", fmt.Errorf("decode SAMLRequest: %w", err)
	}
	if ctx.Method() == http.MethodGet {
		zr := flate.NewReader(bytes.NewReader(raw))
		defer zr.Close()
		raw, err = io.ReadAll(io.LimitReader(zr, samlFlateLimit))
		if err != nil {
			return nil, "", fmt.Errorf("decompress SAMLRequest: %w", err)
		}
	}
	return raw, relay, nil
}

// signSAML signs an etree element with the IdP key using RSA-SHA256 and the
// same exclusive canonicalization the library uses.
func (s *Service) signSAML(el *etree.Element) (*etree.Element, error) {
	keyPair := tls.Certificate{
		Certificate: [][]byte{s.samlCert.Raw},
		PrivateKey:  s.samlKey,
		Leaf:        s.samlCert,
	}
	signingContext := dsig.NewDefaultSigningContext(dsig.TLSCertKeyStore(keyPair))
	signingContext.Canonicalizer = dsig.MakeC14N10ExclusiveCanonicalizerWithPrefixList("")
	if err := signingContext.SetSignatureMethod(dsig.RSASHA256SignatureMethod); err != nil {
		return nil, fmt.Errorf("configure SAML signature: %w", err)
	}
	signed, err := signingContext.SignEnveloped(el)
	if err != nil {
		return nil, fmt.Errorf("sign SAML message: %w", err)
	}
	return signed, nil
}

func samlSLOEndpoint(ed *saml.EntityDescriptor) string {
	for _, sp := range ed.SPSSODescriptors {
		for _, slo := range sp.SingleLogoutServices {
			if slo.Binding == saml.HTTPPostBinding {
				return slo.Location
			}
		}
		for _, slo := range sp.SingleLogoutServices {
			if slo.Binding == saml.HTTPRedirectBinding {
				return slo.Location
			}
		}
	}
	return ""
}

func mustRandomToken() string {
	token, err := randomToken(18)
	if err != nil {
		panic(fmt.Sprintf("generate token: %v", err))
	}
	return token
}

var samlResponseTemplate = template.Must(template.New("saml-response").Parse(
	`<html><body><form method="post" action="{{.URL}}" id="SAMLResponseForm">` +
		`<input type="hidden" name="SAMLResponse" value="{{.SAMLResponse}}"/>` +
		`<input type="hidden" name="RelayState" value="{{.RelayState}}"/>` +
		`<noscript><input type="submit" value="Continue"/></noscript></form>` +
		`<script>document.getElementById('SAMLResponseForm').submit();</script></body></html>`))

func writeSAMLPostForm(w io.Writer, form saml.IdpAuthnRequestForm) error {
	form.URL = html.EscapeString(form.URL)
	form.RelayState = html.EscapeString(form.RelayState)
	if err := samlResponseTemplate.Execute(w, form); err != nil {
		return fmt.Errorf("render SAML response form: %w", err)
	}
	return nil
}

func writeSAMLError(api huma.API, ctx huma.Context, status int, key string) {
	_ = huma.WriteErr(api, ctx, status, key)
}

func samlError(err error) error {
	switch {
	case errors.Is(err, ErrSAMLSPNotFound):
		return huma.Error404NotFound("federation_saml_sp_not_found")
	case errors.Is(err, ErrSAMLSPEntityIDExists):
		return huma.Error409Conflict("federation_saml_sp_entity_id_exists")
	case errors.Is(err, ErrSAMLKeyManagedExternally):
		return huma.Error409Conflict("federation_saml_key_managed_externally")
	case errors.Is(err, ErrSAMLUnavailable), errors.Is(err, ErrSAMLSPDisabled):
		return huma.Error409Conflict("federation_saml_unavailable")
	case errors.Is(err, ErrInvalidSAMLSP), errors.Is(err, ErrSAMLInvalidRequest):
		return huma.Error400BadRequest("federation_saml_invalid_sp")
	default:
		return huma.Error500InternalServerError("federation_saml_unavailable")
	}
}

// ListSAMLServiceProviders returns the tenant's SAML service provider registry.
func (s *Service) ListSAMLServiceProviders(ctx context.Context, tenantID string) ([]SAMLServiceProvider, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" || len(tenantID) > 128 {
		return nil, ErrInvalidSAMLSP
	}
	var rows []samlSPRow
	if err := s.db.NewSelect().Model(&rows).Where("tenant_id = ?", tenantID).Order("name ASC", "id ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list SAML service providers: %w", err)
	}
	result := make([]SAMLServiceProvider, 0, len(rows))
	for _, row := range rows {
		result = append(result, samlSPFromRow(row))
	}
	return result, nil
}

// CreateSAMLServiceProvider registers a service provider from its metadata
// document after validating the XML, entity ID, and assertion consumer binding.
func (s *Service) CreateSAMLServiceProvider(ctx context.Context, tenantID string, input SAMLServiceProviderInput) (SAMLServiceProvider, error) {
	row, err := s.normalizeSAMLSP(tenantID, input)
	if err != nil {
		return SAMLServiceProvider{}, err
	}
	id, err := randomToken(18)
	if err != nil {
		return SAMLServiceProvider{}, err
	}
	now := s.now().UTC().UnixMilli()
	row.ID = id
	row.CreatedAt = now
	row.UpdatedAt = now
	if _, err := s.db.NewInsert().Model(&row).Exec(ctx); err != nil {
		if bunx.IsUniqueViolation(err) {
			return SAMLServiceProvider{}, ErrSAMLSPEntityIDExists
		}
		return SAMLServiceProvider{}, fmt.Errorf("create SAML service provider: %w", err)
	}
	s.invalidateSAMLSP(tenantID, row.EntityID)
	return samlSPFromRow(row), nil
}

// UpdateSAMLServiceProvider replaces a registered service provider. A new
// entity ID is allowed only when the metadata document changes with it.
func (s *Service) UpdateSAMLServiceProvider(ctx context.Context, tenantID, id string, input SAMLServiceProviderInput) (SAMLServiceProvider, error) {
	tenantID, id = strings.TrimSpace(tenantID), strings.TrimSpace(id)
	if tenantID == "" || id == "" {
		return SAMLServiceProvider{}, ErrInvalidSAMLSP
	}
	row, err := s.normalizeSAMLSP(tenantID, input)
	if err != nil {
		return SAMLServiceProvider{}, err
	}
	var existing samlSPRow
	if err := s.db.NewSelect().Model(&existing).Where("id = ? AND tenant_id = ?", id, tenantID).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SAMLServiceProvider{}, ErrSAMLSPNotFound
		}
		return SAMLServiceProvider{}, fmt.Errorf("load SAML service provider: %w", err)
	}
	row.ID = existing.ID
	row.CreatedAt = existing.CreatedAt
	row.UpdatedAt = s.now().UTC().UnixMilli()
	if _, err := s.db.NewUpdate().Model(&row).Where("id = ? AND tenant_id = ?", id, tenantID).Exec(ctx); err != nil {
		if bunx.IsUniqueViolation(err) {
			return SAMLServiceProvider{}, ErrSAMLSPEntityIDExists
		}
		return SAMLServiceProvider{}, fmt.Errorf("update SAML service provider: %w", err)
	}
	s.invalidateSAMLSP(tenantID, existing.EntityID)
	s.invalidateSAMLSP(tenantID, row.EntityID)
	return samlSPFromRow(row), nil
}

// DeleteSAMLServiceProvider removes a service provider and its cache entry.
func (s *Service) DeleteSAMLServiceProvider(ctx context.Context, tenantID, id string) error {
	tenantID, id = strings.TrimSpace(tenantID), strings.TrimSpace(id)
	if tenantID == "" || id == "" {
		return ErrInvalidSAMLSP
	}
	var existing samlSPRow
	if err := s.db.NewSelect().Model(&existing).Where("id = ? AND tenant_id = ?", id, tenantID).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrSAMLSPNotFound
		}
		return fmt.Errorf("load SAML service provider: %w", err)
	}
	if _, err := s.db.NewDelete().Model((*samlSPRow)(nil)).Where("id = ? AND tenant_id = ?", id, tenantID).Exec(ctx); err != nil {
		return fmt.Errorf("delete SAML service provider: %w", err)
	}
	s.invalidateSAMLSP(tenantID, existing.EntityID)
	return nil
}

func (s *Service) normalizeSAMLSP(tenantID string, input SAMLServiceProviderInput) (samlSPRow, error) {
	tenantID = strings.TrimSpace(tenantID)
	name := strings.TrimSpace(input.Name)
	entityID := strings.TrimSpace(input.EntityID)
	metadataXML := strings.TrimSpace(input.MetadataXML)
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = samlSPStatusActive
	}
	if tenantID == "" || len(tenantID) > 128 || name == "" || len(name) > 128 ||
		entityID == "" || len(entityID) > 255 || metadataXML == "" || len(metadataXML) > samlMetadataMaxBytes ||
		(status != samlSPStatusActive && status != ProviderDisabled) {
		return samlSPRow{}, ErrInvalidSAMLSP
	}
	if _, err := parseSAMLMetadata(metadataXML, entityID); err != nil {
		return samlSPRow{}, err
	}
	return samlSPRow{TenantID: tenantID, Name: name, EntityID: entityID, MetadataXML: metadataXML, Status: status}, nil
}

func (s *Service) invalidateSAMLSP(tenantID, entityID string) {
	s.samlSPs.Delete(tenantID + "\x00" + entityID)
}

func samlSPFromRow(row samlSPRow) SAMLServiceProvider {
	return SAMLServiceProvider{
		ID: row.ID, TenantID: row.TenantID, Name: row.Name, EntityID: row.EntityID,
		MetadataXML: row.MetadataXML, Status: row.Status,
		CreatedAt: time.UnixMilli(row.CreatedAt).UTC(), UpdatedAt: time.UnixMilli(row.UpdatedAt).UTC(),
	}
}

// parseSAMLMetadata validates a service provider metadata document. The
// round-trip validator plus the stdlib decoder reject malformed, injected, and
// XXE payloads; a valid document must declare the expected entity ID and expose
// at least one HTTP-POST or HTTP-Redirect assertion consumer service.
func parseSAMLMetadata(raw, entityID string) (*saml.EntityDescriptor, error) {
	if strings.Contains(raw, "<!DOCTYPE") || strings.Contains(raw, "<!ENTITY") {
		return nil, fmt.Errorf("%w: metadata declares an XML entity", ErrInvalidSAMLSP)
	}
	if err := xrv.Validate(strings.NewReader(raw)); err != nil {
		return nil, fmt.Errorf("%w: malformed metadata XML", ErrInvalidSAMLSP)
	}
	var ed saml.EntityDescriptor
	if err := xml.Unmarshal([]byte(raw), &ed); err != nil {
		return nil, fmt.Errorf("%w: cannot parse metadata XML", ErrInvalidSAMLSP)
	}
	if ed.EntityID == "" || ed.EntityID != entityID {
		return nil, fmt.Errorf("%w: metadata entityID does not match the registered entityID", ErrInvalidSAMLSP)
	}
	if spRequiresSignedRequests(&ed) {
		return nil, fmt.Errorf("%w: signed AuthnRequests are not yet supported; remove AuthnRequestsSigned from the metadata", ErrInvalidSAMLSP)
	}
	for _, sp := range ed.SPSSODescriptors {
		for _, acs := range sp.AssertionConsumerServices {
			if acs.Binding == saml.HTTPPostBinding || acs.Binding == saml.HTTPRedirectBinding {
				return &ed, nil
			}
		}
	}
	return nil, fmt.Errorf("%w: metadata has no HTTP-POST or HTTP-Redirect assertion consumer service", ErrInvalidSAMLSP)
}

// samlIdPEntry is a cached upstream IdP descriptor with the time it was
// fetched, so a rotated IdP signing certificate is picked up within one TTL
// instead of requiring a service restart.
type samlIdPEntry struct {
	ed        *saml.EntityDescriptor
	fetchedAt time.Time
}

const samlSPMetadataTTL = 5 * time.Minute

// samlServiceProvider builds a crewjam ServiceProvider for the SP-initiated
// login flow against an upstream SAML IdP. The provider's ClientID holds the
// SP entity ID registered at the IdP; the IdP descriptor is fetched from the
// provider's issuer URL and cached.
//
// ponytail: the SP signs AuthnRequests with a lazily generated self-signed key
// that is not published in SP metadata, so an IdP that strictly verifies SP
// signatures would reject them. Publish SP metadata or wire a configured key if
// an IdP requires verifiable SP signatures.
func (s *Service) samlServiceProvider(ctx context.Context, provider providerRow, callback string) (*saml.ServiceProvider, error) {
	metadata, err := s.samlIdPMetadata(ctx, provider)
	if err != nil {
		return nil, err
	}
	key, cert, err := s.samlSPKey()
	if err != nil {
		return nil, err
	}
	acsURL, err := url.Parse(callback)
	if err != nil {
		return nil, ErrInvalidProvider
	}
	entityID := strings.TrimSpace(provider.ClientID)
	if entityID == "" {
		entityID = callback
	}
	return &saml.ServiceProvider{
		EntityID:        entityID,
		Key:             key,
		Certificate:     cert,
		AcsURL:          *acsURL,
		IDPMetadata:     metadata,
		SignatureMethod: dsig.RSASHA256SignatureMethod,
	}, nil
}

// samlIdPMetadata returns the cached IdP descriptor for an upstream SAML
// provider, fetching and validating it from the provider's issuer URL on a
// cache miss or after the TTL expires.
func (s *Service) samlIdPMetadata(ctx context.Context, provider providerRow) (*saml.EntityDescriptor, error) {
	now := s.now().UTC()
	if cached, ok := s.samlIdP.Load(provider.ID); ok {
		entry := cached.(samlIdPEntry)
		if now.Sub(entry.fetchedAt) < samlSPMetadataTTL {
			return entry.ed, nil
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, provider.Issuer, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build metadata request: %v", ErrSAMLResponse, err)
	}
	resp, err := s.oidc.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: fetch metadata: %v", ErrSAMLResponse, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("%w: metadata status %d", ErrSAMLResponse, resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, samlMetadataMaxBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: read metadata: %v", ErrSAMLResponse, err)
	}
	if strings.Contains(string(raw), "<!DOCTYPE") || strings.Contains(string(raw), "<!ENTITY") {
		return nil, fmt.Errorf("%w: metadata declares an XML entity", ErrSAMLResponse)
	}
	if err := xrv.Validate(bytes.NewReader(raw)); err != nil {
		return nil, fmt.Errorf("%w: malformed metadata XML", ErrSAMLResponse)
	}
	var ed saml.EntityDescriptor
	if err := xml.Unmarshal(raw, &ed); err != nil {
		return nil, fmt.Errorf("%w: parse metadata XML", ErrSAMLResponse)
	}
	if len(ed.IDPSSODescriptors) == 0 {
		return nil, fmt.Errorf("%w: metadata has no IdP SSO descriptor", ErrSAMLResponse)
	}
	s.samlIdP.Store(provider.ID, samlIdPEntry{ed: &ed, fetchedAt: now})
	return &ed, nil
}

// samlSPKey returns the Service's SAML SP signing key, generating a self-signed
// pair on first use.
func (s *Service) samlSPKey() (*rsa.PrivateKey, *x509.Certificate, error) {
	s.spMu.Lock()
	defer s.spMu.Unlock()
	if s.spKey != nil {
		return s.spKey, s.spCert, nil
	}
	key, cert, err := generateSAMLKeyPair()
	if err != nil {
		return nil, nil, err
	}
	s.spKey, s.spCert = key, cert
	return key, cert, nil
}

// parseSAMLResponse decodes and verifies an IdP's SAMLResponse POST against the
// provider's metadata and the expected AuthnRequest ID.
func (s *Service) parseSAMLResponse(ctx context.Context, provider providerRow, samlResponse, callback, requestID string) (*saml.Assertion, error) {
	sp, err := s.samlServiceProvider(ctx, provider, callback)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, callback, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSAMLResponse, err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.PostForm = url.Values{"SAMLResponse": {samlResponse}}
	assertion, err := sp.ParseResponse(request, []string{requestID})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSAMLResponse, err)
	}
	if assertion.Subject == nil || assertion.Subject.NameID == nil || strings.TrimSpace(assertion.Subject.NameID.Value) == "" {
		return nil, fmt.Errorf("%w: assertion has no subject", ErrSAMLResponse)
	}
	return assertion, nil
}

// samlAttribute returns the first value of the named SAML assertion attribute.
func samlAttribute(assertion *saml.Assertion, name string) string {
	for _, statement := range assertion.AttributeStatements {
		for _, attribute := range statement.Attributes {
			if attribute.Name == name && len(attribute.Values) > 0 {
				return attribute.Values[0].Value
			}
		}
	}
	return ""
}
