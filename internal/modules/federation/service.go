package federation

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/policyx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/secretx"
	authnmod "github.com/chaos-plus/chaosplus/internal/modules/authn"
	saml "github.com/crewjam/saml"
	"github.com/uptrace/bun"
)

// Config controls the inbound federation bridge. The encryption key protects
// upstream client secrets and login state cookies; resolve it from a secret
// manager via encryption_key_file in production.
type Config struct {
	Enabled           bool          `mapstructure:"enabled" description:"enable inbound OIDC identity provider federation" default:"false"`
	EncryptionKey     string        `mapstructure:"encryption_key" description:"base64 32-byte key protecting provider client secrets and login state; prefer encryption_key_file" default:""`
	EncryptionKeyFile string        `mapstructure:"encryption_key_file" description:"file containing the base64 32-byte federation encryption key" default:""`
	HTTPTimeout       time.Duration `mapstructure:"http_timeout" description:"upstream discovery, JWKS, and token endpoint timeout" default:"10s"`
	ClockSkew         time.Duration `mapstructure:"clock_skew" description:"allowed ID token clock skew" default:"30s"`
	StateTTL          time.Duration `mapstructure:"state_ttl" description:"login state cookie lifetime" default:"10m"`
	SAML              SAMLConfig    `mapstructure:"saml" group:"saml"`
}

const federationCipherVersion = "v1"

type providerRow struct {
	bun.BaseModel          `bun:"table:iam_identity_providers"`
	ID                     string `bun:"id,pk"`
	TenantID               string
	Name                   string
	ProviderType           string
	Issuer                 string
	ClientID               string
	ClientSecretCiphertext string
	Scopes                 string
	AutoProvision          bool
	DefaultRoleID          string
	Status                 string
	CreatedAt              int64
	UpdatedAt              int64
}

type identityLinkRow struct {
	bun.BaseModel   `bun:"table:iam_identity_links"`
	ProviderID      string `bun:"provider_id,pk"`
	TenantID        string `bun:"tenant_id,pk"`
	ExternalSubject string `bun:"external_subject,pk"`
	PrincipalID     string
	Email           string
	DisplayName     string
	LastLoginAt     int64
	CreatedAt       int64
	UpdatedAt       int64
}

// ExternalPrincipalProvisioner is the identity capability federation needs:
// find or create the global principal plus tenant membership inside the
// caller's transaction.
type ExternalPrincipalProvisioner interface {
	EnsureExternalPrincipal(context.Context, bun.IDB, string, string, string, time.Time) (string, bool, error)
}

// LoginStart is the result of beginning a browser login: where to redirect and
// the state cookie the browser must send back.
type LoginStart struct {
	AuthorizationURL string
	StateCookie      string
}

// LoginComplete is the result of finishing the OIDC callback: the opaque
// browser session token and the application URL to return to.
type LoginComplete struct {
	SessionToken string
	ReturnURL    string
}

type Service struct {
	db         *bun.DB
	dialect    string
	audit      auditx.Appender
	identities ExternalPrincipalProvisioner
	authn      *authnmod.WebService
	key        []byte
	oidc       *oidcClient
	stateTTL   time.Duration
	now        func() time.Time
	samlMu     sync.Mutex
	samlCfg    SAMLConfig
	samlKey    *rsa.PrivateKey
	samlCert   *x509.Certificate
	samlSPs    sync.Map
	// SAML SP-initiated login state: cached upstream IdP metadata keyed by
	// provider ID, plus the lazily generated SP signing key for AuthnRequests.
	samlIdP sync.Map // providerID -> *saml.EntityDescriptor
	spMu    sync.Mutex
	spKey   *rsa.PrivateKey
	spCert  *x509.Certificate
}

// ParseEncryptionKey decodes the federation encryption key from the accepted
// base64 variants; it must decode to exactly 32 bytes.
func ParseEncryptionKey(encoded string) ([]byte, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, errors.New("federation encryption key is required when federation is enabled")
	}
	for _, decoder := range []*base64.Encoding{base64.RawStdEncoding, base64.StdEncoding, base64.RawURLEncoding, base64.URLEncoding} {
		key, err := decoder.DecodeString(encoded)
		if err == nil && len(key) == 32 {
			return key, nil
		}
	}
	return nil, errors.New("federation encryption key must be base64-encoded 32 bytes")
}

// ResolveEncryptionKey loads and validates the configured federation key so
// startup fails fast instead of surfacing a runtime secret error.
func ResolveEncryptionKey(cfg Config) ([]byte, error) {
	encoded, err := secretx.Resolve("federation.encryption_key", cfg.EncryptionKey, cfg.EncryptionKeyFile, 4096)
	if err != nil {
		return nil, err
	}
	return ParseEncryptionKey(encoded)
}

func NewService(db *bun.DB, audit auditx.Appender, identities ExternalPrincipalProvisioner, authn *authnmod.WebService, cfg Config, key []byte) *Service {
	if db == nil || audit == nil || identities == nil || authn == nil {
		panic("federation service requires database, audit appender, identity provisioner, and authentication service")
	}
	dialect := db.Dialect().Name().String()
	if dialect == "pg" {
		dialect = "postgres"
	}
	timeout := cfg.HTTPTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	skew := cfg.ClockSkew
	if skew <= 0 {
		skew = 30 * time.Second
	}
	stateTTL := cfg.StateTTL
	if stateTTL <= 0 {
		stateTTL = 10 * time.Minute
	}
	return &Service{
		db: db, dialect: dialect, audit: audit, identities: identities, authn: authn,
		key: key, stateTTL: stateTTL, now: time.Now,
		oidc: newOIDCClient(&http.Client{Timeout: timeout}, skew, 5*time.Minute),
	}
}

func (s *Service) ListProviders(ctx context.Context, tenantID string) ([]Provider, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" || len(tenantID) > 128 {
		return nil, ErrInvalidProvider
	}
	var rows []providerRow
	if err := s.db.NewSelect().Model(&rows).Where("tenant_id = ?", tenantID).Order("name ASC", "id ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list identity providers: %w", err)
	}
	result := make([]Provider, 0, len(rows))
	for _, row := range rows {
		result = append(result, providerFromRow(row))
	}
	return result, nil
}

func (s *Service) CreateProvider(ctx context.Context, tenantID string, input ProviderInput) (Provider, error) {
	row, secret, err := s.normalizeProvider(tenantID, input)
	if err != nil {
		return Provider{}, err
	}
	if err := s.validateDefaultRole(ctx, s.db, row.TenantID, row.DefaultRoleID); err != nil {
		return Provider{}, err
	}
	id, err := randomToken(18)
	if err != nil {
		return Provider{}, err
	}
	now := s.now().UTC().UnixMilli()
	row.ID = id
	row.CreatedAt = now
	row.UpdatedAt = now
	if secret != "" {
		row.ClientSecretCiphertext, err = s.encryptSecret(id, secret)
		if err != nil {
			return Provider{}, err
		}
	}
	if _, err := s.db.NewInsert().Model(&row).Exec(ctx); err != nil {
		if bunx.IsUniqueViolation(err) {
			return Provider{}, ErrProviderIssuerExists
		}
		return Provider{}, fmt.Errorf("create identity provider: %w", err)
	}
	return providerFromRow(row), nil
}

func (s *Service) UpdateProvider(ctx context.Context, tenantID, id string, input ProviderInput) (Provider, error) {
	tenantID, id = strings.TrimSpace(tenantID), strings.TrimSpace(id)
	if tenantID == "" || len(tenantID) > 128 || id == "" || len(id) > 128 {
		return Provider{}, ErrInvalidProvider
	}
	current, err := s.getProvider(ctx, tenantID, id)
	if err != nil {
		return Provider{}, err
	}
	row, secret, err := s.normalizeProvider(tenantID, input)
	if err != nil {
		return Provider{}, err
	}
	if err := s.validateDefaultRole(ctx, s.db, tenantID, row.DefaultRoleID); err != nil {
		return Provider{}, err
	}
	row.ID = current.ID
	row.TenantID = tenantID
	row.CreatedAt = current.CreatedAt
	row.UpdatedAt = s.now().UTC().UnixMilli()
	row.ClientSecretCiphertext = current.ClientSecretCiphertext
	if secret != "" {
		row.ClientSecretCiphertext, err = s.encryptSecret(row.ID, secret)
		if err != nil {
			return Provider{}, err
		}
	}
	result, err := s.db.NewUpdate().Table("iam_identity_providers").
		Set("name = ?", row.Name).Set("provider_type = ?", row.ProviderType).Set("issuer = ?", row.Issuer).
		Set("client_id = ?", row.ClientID).Set("client_secret_ciphertext = ?", row.ClientSecretCiphertext).
		Set("scopes = ?", row.Scopes).Set("auto_provision = ?", row.AutoProvision).
		Set("default_role_id = ?", row.DefaultRoleID).Set("status = ?", row.Status).Set("updated_at = ?", row.UpdatedAt).
		Where("tenant_id = ? AND id = ?", tenantID, id).Exec(ctx)
	if err != nil {
		if bunx.IsUniqueViolation(err) {
			return Provider{}, ErrProviderIssuerExists
		}
		return Provider{}, fmt.Errorf("update identity provider: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return Provider{}, ErrProviderNotFound
	}
	return providerFromRow(row), nil
}

func (s *Service) DeleteProvider(ctx context.Context, tenantID, id string) error {
	tenantID, id = strings.TrimSpace(tenantID), strings.TrimSpace(id)
	if tenantID == "" || len(tenantID) > 128 || id == "" || len(id) > 128 {
		return ErrInvalidProvider
	}
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewDelete().Table("iam_identity_links").Where("provider_id = ?", id).Exec(ctx); err != nil {
			return err
		}
		result, err := tx.NewDelete().Model((*providerRow)(nil)).Where("tenant_id = ? AND id = ?", tenantID, id).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return ErrProviderNotFound
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("delete identity provider: %w", err)
	}
	return nil
}

// StartLogin begins a browser login for an upstream identity provider: the
// OIDC authorization-code flow, or the SAML SP-initiated flow for SAML
// providers. It resolves the provider, seals the state cookie, and returns the
// upstream redirect URL.
func (s *Service) StartLogin(ctx context.Context, providerID, returnURL, callback string) (LoginStart, error) {
	provider, err := s.getProviderByID(ctx, strings.TrimSpace(providerID))
	if err != nil {
		return LoginStart{}, err
	}
	if provider.Status != ProviderActive {
		return LoginStart{}, ErrProviderDisabled
	}
	if provider.ProviderType == ProviderSAML {
		redirectURL, stateCookie, err := s.StartSAMLLogin(ctx, providerID, returnURL, callback)
		if err != nil {
			return LoginStart{}, err
		}
		return LoginStart{AuthorizationURL: redirectURL, StateCookie: stateCookie}, nil
	}
	if !validHTTPURL(callback) || !strings.HasSuffix(callback, callbackURL(provider.ID)) {
		return LoginStart{}, ErrInvalidProvider
	}
	resolvedReturnURL, err := s.authn.ResolveReturnURL(returnURL)
	if err != nil {
		return LoginStart{}, err
	}
	state, err := newLoginState(provider.ID, resolvedReturnURL, s.stateTTL, s.now().UTC())
	if err != nil {
		return LoginStart{}, err
	}
	sealed, err := sealState(s.key, state)
	if err != nil {
		return LoginStart{}, err
	}
	discovery, err := s.oidc.discovery(ctx, provider.Issuer)
	if err != nil {
		return LoginStart{}, err
	}
	query := url.Values{}
	query.Set("response_type", "code")
	query.Set("client_id", provider.ClientID)
	query.Set("redirect_uri", callback)
	query.Set("scope", provider.Scopes)
	query.Set("state", state.Nonce)
	query.Set("code_challenge", codeChallenge(state.CodeVerifier))
	query.Set("code_challenge_method", "S256")
	separator := "?"
	if strings.Contains(discovery.AuthorizationEndpoint, "?") {
		separator = "&"
	}
	return LoginStart{
		AuthorizationURL: discovery.AuthorizationEndpoint + separator + query.Encode(),
		StateCookie:      stateCookie(sealed, s.authn.CookieSecure(), int(s.stateTTL.Seconds())),
	}, nil
}

// CompleteLogin finishes the callback: it validates the state cookie, exchanges
// the code, verifies the ID token, provisions or links the identity, and issues
// a browser session.
func (s *Service) CompleteLogin(ctx context.Context, providerID, code, state, cookieHeader, callback string) (LoginComplete, error) {
	provider, err := s.getProviderByID(ctx, strings.TrimSpace(providerID))
	if err != nil {
		return LoginComplete{}, err
	}
	if provider.Status != ProviderActive {
		return LoginComplete{}, ErrProviderDisabled
	}
	stateValue, err := cookieValue(cookieHeader, stateCookieName)
	if err != nil {
		return LoginComplete{}, ErrOIDCState
	}
	sealedState, err := openState(s.key, stateValue)
	if err != nil {
		return LoginComplete{}, ErrOIDCState
	}
	if sealedState.ProviderID != provider.ID || !strings.EqualFold(sealedState.Nonce, state) || sealedState.ExpiresAt < s.now().UTC().Unix() {
		return LoginComplete{}, ErrOIDCState
	}
	var clientSecret string
	if provider.ClientSecretCiphertext != "" {
		clientSecret, err = s.decryptSecret(provider.ID, provider.ClientSecretCiphertext)
		if err != nil {
			return LoginComplete{}, err
		}
	}
	rawIDToken, err := s.oidc.exchangeCode(ctx, provider.Issuer, provider.ClientID, clientSecret, code, callback, sealedState.CodeVerifier)
	if err != nil {
		return LoginComplete{}, err
	}
	claims, err := s.oidc.verifyIDToken(ctx, provider.Issuer, provider.ClientID, rawIDToken, s.now().UTC())
	if err != nil {
		return LoginComplete{}, err
	}
	sessionToken, err := s.authenticate(ctx, provider, claims, "oidc")
	if err != nil {
		return LoginComplete{}, err
	}
	return LoginComplete{SessionToken: sessionToken, ReturnURL: sealedState.ReturnURL}, nil
}

// StateClearCookie returns a cookie that expires the login state after the
// callback completes (successfully or not).
func (s *Service) StateClearCookie() string {
	return stateClearCookie(s.authn.CookieSecure())
}

// StartSAMLLogin begins an SP-initiated SAML login for a SAML identity
// provider. It builds a signed HTTP-Redirect AuthnRequest to the IdP's SSO
// endpoint and returns the redirect URL plus the sealed state cookie the
// browser must send back to the callback.
func (s *Service) StartSAMLLogin(ctx context.Context, providerID, returnURL, callback string) (string, string, error) {
	provider, err := s.getProviderByID(ctx, strings.TrimSpace(providerID))
	if err != nil {
		return "", "", err
	}
	if provider.Status != ProviderActive {
		return "", "", ErrProviderDisabled
	}
	if provider.ProviderType != ProviderSAML {
		return "", "", ErrInvalidProvider
	}
	if !validHTTPURL(callback) || !strings.HasSuffix(callback, callbackURL(provider.ID)) {
		return "", "", ErrInvalidProvider
	}
	resolvedReturnURL, err := s.authn.ResolveReturnURL(returnURL)
	if err != nil {
		return "", "", err
	}
	now := s.now().UTC()
	// The state nonce doubles as the AuthnRequest ID so the callback can bind
	// the response's InResponseTo to the exact request the browser started.
	nonce, err := randomToken(18)
	if err != nil {
		return "", "", err
	}
	verifier, err := randomToken(32)
	if err != nil {
		return "", "", err
	}
	state := loginState{
		ProviderID: provider.ID, Nonce: "id-" + nonce, CodeVerifier: verifier,
		ReturnURL: resolvedReturnURL, ExpiresAt: now.Add(s.stateTTL).Unix(),
	}
	sealed, err := sealState(s.key, state)
	if err != nil {
		return "", "", err
	}
	sp, err := s.samlServiceProvider(ctx, provider, callback)
	if err != nil {
		return "", "", err
	}
	authnRequest := &saml.AuthnRequest{
		ID:                          state.Nonce,
		Version:                     "2.0",
		IssueInstant:                now,
		Destination:                 sp.GetSSOBindingLocation(saml.HTTPRedirectBinding),
		Issuer:                      &saml.Issuer{Format: samlNameIDEntity, Value: sp.EntityID},
		AssertionConsumerServiceURL: callback,
		ProtocolBinding:             saml.HTTPPostBinding,
	}
	redirectURL, err := authnRequest.Redirect(state.Nonce, sp)
	if err != nil {
		return "", "", fmt.Errorf("%w: build redirect request: %v", ErrSAMLResponse, err)
	}
	return redirectURL.String(), stateCookie(sealed, s.authn.CookieSecure(), int(s.stateTTL.Seconds())), nil
}

// CompleteSAMLLogin finishes an SP-initiated SAML login: it verifies the state
// cookie, parses and verifies the IdP's SAMLResponse, provisions or links the
// identity, and issues a browser session.
func (s *Service) CompleteSAMLLogin(ctx context.Context, providerID, samlResponse, cookieHeader, callback string) (string, string, error) {
	provider, err := s.getProviderByID(ctx, strings.TrimSpace(providerID))
	if err != nil {
		return "", "", err
	}
	if provider.Status != ProviderActive {
		return "", "", ErrProviderDisabled
	}
	if provider.ProviderType != ProviderSAML {
		return "", "", ErrInvalidProvider
	}
	if !validHTTPURL(callback) || !strings.HasSuffix(callback, callbackURL(provider.ID)) {
		return "", "", ErrInvalidProvider
	}
	stateValue, err := cookieValue(cookieHeader, stateCookieName)
	if err != nil {
		return "", "", ErrOIDCState
	}
	sealedState, err := openState(s.key, stateValue)
	if err != nil {
		return "", "", ErrOIDCState
	}
	if sealedState.ProviderID != provider.ID || sealedState.ExpiresAt < s.now().UTC().Unix() {
		return "", "", ErrOIDCState
	}
	assertion, err := s.parseSAMLResponse(ctx, provider, samlResponse, callback, sealedState.Nonce)
	if err != nil {
		return "", "", err
	}
	email := strings.TrimSpace(samlAttribute(assertion, "email"))
	if email == "" {
		email = strings.TrimSpace(samlAttribute(assertion, "Email"))
	}
	displayName := strings.TrimSpace(samlAttribute(assertion, "display_name"))
	if displayName == "" {
		displayName = strings.TrimSpace(samlAttribute(assertion, "displayName"))
	}
	if displayName == "" {
		displayName = strings.TrimSpace(samlAttribute(assertion, "uid"))
	}
	verified := true
	var authTime int64
	if len(assertion.AuthnStatements) > 0 {
		authTime = assertion.AuthnStatements[0].AuthnInstant.Unix()
	}
	claims := idTokenClaims{
		Subject: assertion.Subject.NameID.Value, Email: email, EmailVerified: &verified,
		Name: displayName, PreferredUsername: displayName, AuthTime: authTime,
	}
	sessionToken, err := s.authenticate(ctx, provider, claims, "saml")
	if err != nil {
		return "", "", err
	}
	return sessionToken, sealedState.ReturnURL, nil
}

func (s *Service) authenticate(ctx context.Context, provider providerRow, token idTokenClaims, method string) (string, error) {
	var principalID string
	now := s.now().UTC()
	provisioned := false
	defaultRoleMissing := false
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var link identityLinkRow
		err := tx.NewSelect().Model(&link).
			Where("provider_id = ? AND tenant_id = ? AND external_subject = ?", provider.ID, provider.TenantID, token.Subject).
			Scan(ctx)
		switch {
		case err == nil:
			principalID = link.PrincipalID
		case errors.Is(err, sql.ErrNoRows):
			if !provider.AutoProvision {
				return ErrProvisioningDisabled
			}
			if token.Email == "" || token.EmailVerified == nil || !*token.EmailVerified {
				return ErrProvisioningUnverified
			}
			displayName := strings.TrimSpace(token.Name)
			if displayName == "" {
				displayName = strings.TrimSpace(token.PreferredUsername)
			}
			id, created, err := s.identities.EnsureExternalPrincipal(ctx, tx, provider.TenantID, token.Email, displayName, now)
			if err != nil {
				return err
			}
			principalID = id
			provisioned = created
			if created && provider.DefaultRoleID != "" {
				granted, err := grantDefaultRole(ctx, tx, s.dialect, provider.TenantID, provider.DefaultRoleID, principalID, now)
				if err != nil {
					return err
				}
				defaultRoleMissing = !granted
			}
			link = identityLinkRow{
				ProviderID: provider.ID, TenantID: provider.TenantID, ExternalSubject: token.Subject,
				PrincipalID: principalID, Email: token.Email, DisplayName: displayName,
				LastLoginAt: now.UnixMilli(), CreatedAt: now.UnixMilli(), UpdatedAt: now.UnixMilli(),
			}
			if _, err := tx.NewInsert().Model(&link).Exec(ctx); err != nil {
				return fmt.Errorf("insert identity link: %w", err)
			}
		default:
			return err
		}
		if err := verifyPrincipalAndMember(ctx, tx, provider.TenantID, principalID); err != nil {
			return err
		}
		if _, err := tx.NewUpdate().Model((*identityLinkRow)(nil)).
			Set("last_login_at = ?", now.UnixMilli()).Set("updated_at = ?", now.UnixMilli()).
			Where("provider_id = ? AND tenant_id = ? AND external_subject = ?", provider.ID, provider.TenantID, token.Subject).Exec(ctx); err != nil {
			return err
		}
		event := auditx.NewEvent(ctx, provider.TenantID, "federation_login", "identity_provider", provider.ID)
		event.PrincipalID = principalID
		event.Detail["external_subject"] = token.Subject
		event.Detail["provider"] = provider.Name
		event.Detail["email"] = token.Email
		event.Detail["provisioned"] = provisioned
		if defaultRoleMissing {
			event.Detail["default_role_missing"] = true
		}
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return "", err
	}
	assurance := authnext.Assurance{AuthTime: now, Level: 1, Methods: []string{method}}
	if token.AuthTime > 0 {
		assurance.AuthTime = time.Unix(token.AuthTime, 0).UTC()
	}
	return s.authn.CreateSession(ctx, principalID, now, assurance)
}

func (s *Service) normalizeProvider(tenantID string, input ProviderInput) (providerRow, string, error) {
	tenantID = strings.TrimSpace(tenantID)
	name := strings.TrimSpace(input.Name)
	providerType := strings.TrimSpace(input.ProviderType)
	if providerType == "" {
		providerType = ProviderOIDC
	}
	issuer, err := normalizeIssuer(input.Issuer)
	clientID := strings.TrimSpace(input.ClientID)
	scopes := normalizeScopes(input.Scopes)
	defaultRoleID := strings.TrimSpace(input.DefaultRoleID)
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = ProviderActive
	}
	secret := strings.TrimSpace(input.ClientSecret)
	if tenantID == "" || len(tenantID) > 128 || name == "" || len(name) > 128 ||
		(providerType != ProviderOIDC && providerType != ProviderSAML) ||
		err != nil || clientID == "" || len(clientID) > 128 ||
		len(defaultRoleID) > 32 || (status != ProviderActive && status != ProviderDisabled) || len(secret) > 2048 {
		return providerRow{}, "", ErrInvalidProvider
	}
	// SAML providers don't require scopes (assertion-driven); OIDC does.
	if providerType == ProviderOIDC && (scopes == "" || len(scopes) > 255) {
		return providerRow{}, "", ErrInvalidProvider
	}
	return providerRow{
		TenantID: tenantID, Name: name, ProviderType: providerType, Issuer: issuer, ClientID: clientID,
		Scopes: scopes, AutoProvision: input.AutoProvision, DefaultRoleID: defaultRoleID, Status: status,
	}, secret, nil
}

// validateDefaultRole rejects provider changes that point at a role the tenant
// does not have, so JIT provisioning never silently grants nothing.
func (s *Service) validateDefaultRole(ctx context.Context, db bun.IDB, tenantID, roleID string) error {
	if roleID == "" {
		return nil
	}
	exists, err := db.NewSelect().Table("iam_roles").Where("tenant_id = ? AND id = ?", tenantID, roleID).Exists(ctx)
	if err != nil {
		return fmt.Errorf("validate default role: %w", err)
	}
	if !exists {
		return ErrProviderRoleMissing
	}
	return nil
}

func (s *Service) getProvider(ctx context.Context, tenantID, id string) (providerRow, error) {
	var row providerRow
	if err := s.db.NewSelect().Model(&row).Where("tenant_id = ? AND id = ?", tenantID, id).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return providerRow{}, ErrProviderNotFound
		}
		return providerRow{}, fmt.Errorf("get identity provider: %w", err)
	}
	return row, nil
}

func (s *Service) getProviderByID(ctx context.Context, id string) (providerRow, error) {
	if id == "" || len(id) > 128 {
		return providerRow{}, ErrProviderNotFound
	}
	var row providerRow
	if err := s.db.NewSelect().Model(&row).Where("id = ?", id).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return providerRow{}, ErrProviderNotFound
		}
		return providerRow{}, fmt.Errorf("get identity provider: %w", err)
	}
	return row, nil
}

func (s *Service) encryptSecret(providerID, secret string) (string, error) {
	block, err := aes.NewCipher(purposeKey(s.key, "client-secret"))
	if err != nil {
		return "", fmt.Errorf("create federation cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create federation AEAD: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate federation nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(secret), []byte("chaosplus:federation:secret\x00"+providerID))
	return federationCipherVersion + "." + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (s *Service) decryptSecret(providerID, encoded string) (string, error) {
	version, payload, ok := strings.Cut(encoded, ".")
	if !ok || version != federationCipherVersion || payload == "" {
		return "", errors.New("unsupported federation client secret ciphertext")
	}
	sealed, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return "", errors.New("invalid federation client secret ciphertext")
	}
	block, err := aes.NewCipher(purposeKey(s.key, "client-secret"))
	if err != nil {
		return "", fmt.Errorf("create federation cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create federation AEAD: %w", err)
	}
	if len(sealed) < gcm.NonceSize() {
		return "", errors.New("invalid federation client secret ciphertext")
	}
	nonce, ciphertext := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, []byte("chaosplus:federation:secret\x00"+providerID))
	if err != nil {
		return "", errors.New("decrypt federation client secret")
	}
	return string(plain), nil
}

func grantDefaultRole(ctx context.Context, tx bun.IDB, dialect, tenantID, roleID, principalID string, now time.Time) (bool, error) {
	exists, err := tx.NewSelect().Table("iam_roles").Where("tenant_id = ? AND id = ?", tenantID, roleID).Exists(ctx)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}
	if _, err := tx.NewRaw("INSERT INTO iam_role_members (tenant_id, role_id, user_subject, created_at) VALUES (?, ?, ?, ?)", tenantID, roleID, principalID, now.UnixMilli()).Exec(ctx); err != nil {
		return false, fmt.Errorf("grant federation default role: %w", err)
	}
	if err := policyx.Advance(ctx, tx, dialect, tenantID, now.UnixMilli()); err != nil {
		return false, err
	}
	return true, nil
}

func verifyPrincipalAndMember(ctx context.Context, db bun.IDB, tenantID, principalID string) error {
	var status string
	if err := db.NewSelect().Table("iam_principals").Column("status").Where("id = ?", principalID).Scan(ctx, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrPrincipalInactive
		}
		return err
	}
	if status != "active" {
		return ErrPrincipalInactive
	}
	if err := db.NewSelect().Table("iam_tenant_members").Column("status").Where("tenant_id = ? AND user_subject = ?", tenantID, principalID).Scan(ctx, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrPrincipalInactive
		}
		return err
	}
	if status != "active" {
		return ErrPrincipalInactive
	}
	return nil
}

func providerFromRow(row providerRow) Provider {
	return Provider{
		ID: row.ID, TenantID: row.TenantID, Name: row.Name, ProviderType: row.ProviderType,
		Issuer: row.Issuer, ClientID: row.ClientID, ClientSecretSet: row.ClientSecretCiphertext != "",
		Scopes: row.Scopes, AutoProvision: row.AutoProvision, DefaultRoleID: row.DefaultRoleID,
		Status: row.Status, CreatedAt: time.UnixMilli(row.CreatedAt).UTC(), UpdatedAt: time.UnixMilli(row.UpdatedAt).UTC(),
	}
}

func normalizeIssuer(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", errors.New("invalid issuer")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return strings.TrimRight(parsed.String(), "/"), nil
}

func normalizeScopes(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultScopes
	}
	fields := strings.Fields(value)
	seen := make(map[string]struct{}, len(fields))
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		if len(field) > 64 || strings.ContainsAny(field, " \t\r\n") {
			return ""
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		result = append(result, field)
	}
	if len(result) == 0 {
		return ""
	}
	return strings.Join(result, " ")
}

// purposeKey derives a domain-separated key so one federation encryption key
// never encrypts two different kinds of payload with the same key material.
func purposeKey(key []byte, purpose string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("chaosplus:federation:key:" + purpose))
	return mac.Sum(nil)
}

func randomToken(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate secure token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func cookieValue(header, name string) (string, error) {
	request := &http.Request{Header: http.Header{"Cookie": []string{header}}}
	cookie, err := request.Cookie(name)
	if err != nil || cookie.Value == "" {
		return "", http.ErrNoCookie
	}
	return cookie.Value, nil
}

// callbackURL is the canonical callback path. The API layer prepends the
// externally visible scheme and host before sending it to the IdP.
func callbackURL(providerID string) string {
	return "/federation/" + providerID + "/callback"
}
