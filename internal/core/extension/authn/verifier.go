package authn

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/spicedbx"
)

const (
	defaultHTTPTimeout = 5 * time.Second
	defaultClockSkew   = 30 * time.Second
)

var (
	ErrDisabled      = errors.New("authn disabled")
	ErrInvalidToken  = errors.New("invalid token")
	ErrExpiredToken  = errors.New("token expired")
	ErrUnknownKey    = errors.New("unknown jwks key")
	ErrInvalidIssuer = errors.New("invalid issuer")
	ErrInvalidAud    = errors.New("invalid audience")
)

// Config describes a Zitadel/OIDC issuer. JWKSURL is optional; when empty, the
// verifier discovers it from <issuer>/.well-known/openid-configuration.
type Config struct {
	Enabled       bool          `mapstructure:"enabled" description:"enable Zitadel/OIDC JWT authentication" default:"false"`
	Issuer        string        `mapstructure:"issuer" description:"OIDC issuer, e.g. http://10.0.0.100:38080"`
	Audience      []string      `mapstructure:"audience" description:"accepted JWT audience values; empty skips audience check"`
	ResourcesFile string        `mapstructure:"resources_file" description:"bootstrap-generated JSON containing Zitadel project and client IDs" default:""`
	JWKSURL       string        `mapstructure:"jwks_url" description:"OIDC JWKS URL; empty discovers from issuer"`
	HTTPTimeout   time.Duration `mapstructure:"http_timeout" description:"OIDC discovery/JWKS HTTP timeout" default:"5s"`
	ClockSkew     time.Duration `mapstructure:"clock_skew" description:"allowed token clock skew" default:"30s"`
	Web           WebConfig     `mapstructure:"web" group:"web"`
}

// WebConfig enables the browser-facing OIDC BFF. Access and refresh tokens are
// kept encrypted in Redis; the browser only receives an opaque session cookie.
type WebConfig struct {
	Enabled           bool          `mapstructure:"enabled" description:"enable browser OIDC BFF" default:"false"`
	ClientID          string        `mapstructure:"client_id" description:"OIDC public client id"`
	RedirectURL       string        `mapstructure:"redirect_url" description:"exact OIDC callback URL"`
	PostLoginURL      string        `mapstructure:"post_login_url" description:"default frontend URL after login"`
	PostLogoutURL     string        `mapstructure:"post_logout_url" description:"frontend URL after logout"`
	AllowedReturnURLs []string      `mapstructure:"allowed_return_urls" description:"exact frontend return URL allowlist"`
	AllowedOrigins    []string      `mapstructure:"allowed_origins" description:"origins allowed for cookie-authenticated writes"`
	CookieName        string        `mapstructure:"cookie_name" description:"opaque session cookie name" default:"cp_session"`
	CookieSecure      bool          `mapstructure:"cookie_secure" description:"require HTTPS for session cookies" default:"true"`
	SessionTTL        time.Duration `mapstructure:"session_ttl" description:"maximum browser session lifetime" default:"8h"`
	FlowTTL           time.Duration `mapstructure:"flow_ttl" description:"OIDC state lifetime" default:"5m"`
	EncryptionKey     string        `mapstructure:"encryption_key" description:"base64 or raw 32-byte AES key"`
	EncryptionKeyFile string        `mapstructure:"encryption_key_file" description:"file containing the BFF encryption key; mutually exclusive with encryption_key" default:""`
	Scopes            []string      `mapstructure:"scopes" description:"OIDC scopes; defaults to openid profile email offline_access plus API audience"`
}

// Claims contains the token fields this API needs. Raw keeps provider-specific
// Zitadel claims available without baking them into the core model.
type Claims struct {
	Issuer            string         `json:"iss"`
	Subject           string         `json:"sub"`
	Audience          []string       `json:"aud"`
	ExpiresAt         time.Time      `json:"exp"`
	NotBefore         time.Time      `json:"nbf,omitempty"`
	IssuedAt          time.Time      `json:"iat,omitempty"`
	PreferredUsername string         `json:"preferred_username,omitempty"`
	Email             string         `json:"email,omitempty"`
	EmailVerified     bool           `json:"email_verified,omitempty"`
	OrganizationID    string         `json:"urn:zitadel:iam:org:id,omitempty"`
	Raw               map[string]any `json:"raw,omitempty"`
}

func (c *Claims) SubjectRef() spicedbx.SubjectRef {
	return spicedbx.SubjectRef{Object: spicedbx.ObjectRef{Type: "user", ID: c.Subject}}
}

// Verifier verifies compact JWTs signed by the issuer's JWKS.
type Verifier struct {
	cfg    Config
	client *http.Client

	mu      sync.RWMutex
	jwksURL string
	keys    map[string]crypto.PublicKey
}

func NewVerifier(cfg Config) (*Verifier, error) {
	if !cfg.Enabled {
		return &Verifier{cfg: cfg}, nil
	}
	cfg.Issuer = strings.TrimRight(cfg.Issuer, "/")
	if cfg.Issuer == "" {
		return nil, fmt.Errorf("authn issuer is required")
	}
	if cfg.HTTPTimeout <= 0 {
		cfg.HTTPTimeout = defaultHTTPTimeout
	}
	if cfg.ClockSkew <= 0 {
		cfg.ClockSkew = defaultClockSkew
	}
	return &Verifier{
		cfg:    cfg,
		client: &http.Client{Timeout: cfg.HTTPTimeout},
		keys:   map[string]crypto.PublicKey{},
	}, nil
}

func (v *Verifier) VerifyAuthorization(ctx context.Context, header string) (*Claims, error) {
	if !v.cfg.Enabled {
		return nil, ErrDisabled
	}
	token, err := bearerToken(header)
	if err != nil {
		return nil, err
	}
	return v.Verify(ctx, token)
}

// Authenticate lets the JWT verifier act as a bearer-only request
// authenticator when browser sessions are disabled.
func (v *Verifier) Authenticate(ctx context.Context, authorization, _ string) (*Claims, error) {
	return v.VerifyAuthorization(ctx, authorization)
}

func (v *Verifier) Verify(ctx context.Context, token string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrInvalidToken
	}

	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
		Typ string `json:"typ"`
	}
	if err := decodeJSON(parts[0], &header); err != nil {
		return nil, fmt.Errorf("%w: header: %v", ErrInvalidToken, err)
	}
	if header.Kid == "" || header.Alg == "" {
		return nil, ErrInvalidToken
	}

	key, err := v.key(ctx, header.Kid)
	if err != nil {
		return nil, err
	}
	signed := parts[0] + "." + parts[1]
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("%w: signature encoding", ErrInvalidToken)
	}
	if err := verifySignature(header.Alg, key, []byte(signed), sig); err != nil {
		return nil, err
	}

	var raw map[string]any
	if err := decodeJSON(parts[1], &raw); err != nil {
		return nil, fmt.Errorf("%w: claims: %v", ErrInvalidToken, err)
	}
	claims, err := parseClaims(raw)
	if err != nil {
		return nil, err
	}
	if err := v.validate(claims, time.Now()); err != nil {
		return nil, err
	}
	return claims, nil
}

func (v *Verifier) validate(claims *Claims, now time.Time) error {
	if subtle.ConstantTimeCompare([]byte(claims.Issuer), []byte(v.cfg.Issuer)) != 1 {
		return ErrInvalidIssuer
	}
	if claims.Subject == "" {
		return ErrInvalidToken
	}
	if now.After(claims.ExpiresAt.Add(v.cfg.ClockSkew)) {
		return ErrExpiredToken
	}
	if !claims.NotBefore.IsZero() && now.Add(v.cfg.ClockSkew).Before(claims.NotBefore) {
		return ErrInvalidToken
	}
	if len(v.cfg.Audience) > 0 && !audienceAllowed(claims.Audience, v.cfg.Audience) {
		return ErrInvalidAud
	}
	return nil
}

func (v *Verifier) key(ctx context.Context, kid string) (crypto.PublicKey, error) {
	v.mu.RLock()
	key, ok := v.keys[kid]
	v.mu.RUnlock()
	if ok {
		return key, nil
	}
	if err := v.refreshKeys(ctx); err != nil {
		return nil, err
	}
	v.mu.RLock()
	key, ok = v.keys[kid]
	v.mu.RUnlock()
	if !ok {
		return nil, ErrUnknownKey
	}
	return key, nil
}

func (v *Verifier) refreshKeys(ctx context.Context) error {
	jwksURL, err := v.jwksURLValue(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURL, nil)
	if err != nil {
		return err
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch jwks: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("fetch jwks: status %d", resp.StatusCode)
	}
	var set struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		return fmt.Errorf("decode jwks: %w", err)
	}
	keys := make(map[string]crypto.PublicKey, len(set.Keys))
	for _, k := range set.Keys {
		key, err := k.publicKey()
		if err != nil {
			return err
		}
		keys[k.Kid] = key
	}
	v.mu.Lock()
	v.keys = keys
	v.mu.Unlock()
	return nil
}

func (v *Verifier) jwksURLValue(ctx context.Context) (string, error) {
	if v.cfg.JWKSURL != "" {
		return v.cfg.JWKSURL, nil
	}
	v.mu.RLock()
	cached := v.jwksURL
	v.mu.RUnlock()
	if cached != "" {
		return cached, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.cfg.Issuer+"/.well-known/openid-configuration", nil)
	if err != nil {
		return "", err
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("discover oidc: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("discover oidc: status %d", resp.StatusCode)
	}
	var doc struct {
		JWKSURI string `json:"jwks_uri"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return "", fmt.Errorf("decode oidc discovery: %w", err)
	}
	if doc.JWKSURI == "" {
		return "", fmt.Errorf("oidc discovery has no jwks_uri")
	}
	v.mu.Lock()
	v.jwksURL = doc.JWKSURI
	v.mu.Unlock()
	return doc.JWKSURI, nil
}

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func (k jwk) publicKey() (crypto.PublicKey, error) {
	if k.Kid == "" {
		return nil, fmt.Errorf("jwks key missing kid")
	}
	switch k.Kty {
	case "RSA":
		n, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			return nil, fmt.Errorf("decode rsa modulus: %w", err)
		}
		e, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			return nil, fmt.Errorf("decode rsa exponent: %w", err)
		}
		return &rsa.PublicKey{N: intFromBytes(n), E: int(intFromBytes(e).Int64())}, nil
	default:
		return nil, fmt.Errorf("unsupported jwks kty %q", k.Kty)
	}
}

func verifySignature(alg string, key crypto.PublicKey, signed, sig []byte) error {
	hash := sha256.Sum256(signed)
	switch alg {
	case "RS256":
		rsaKey, ok := key.(*rsa.PublicKey)
		if !ok {
			return ErrInvalidToken
		}
		return rsa.VerifyPKCS1v15(rsaKey, crypto.SHA256, hash[:], sig)
	default:
		return fmt.Errorf("%w: unsupported alg %s", ErrInvalidToken, alg)
	}
}

func parseClaims(raw map[string]any) (*Claims, error) {
	claims := &Claims{Raw: raw}
	claims.Issuer, _ = raw["iss"].(string)
	claims.Subject, _ = raw["sub"].(string)
	claims.PreferredUsername, _ = raw["preferred_username"].(string)
	claims.Email, _ = raw["email"].(string)
	claims.EmailVerified, _ = raw["email_verified"].(bool)
	claims.OrganizationID, _ = raw["urn:zitadel:iam:org:id"].(string)
	claims.Audience = stringSliceClaim(raw["aud"])
	claims.ExpiresAt = unixClaim(raw["exp"])
	claims.NotBefore = unixClaim(raw["nbf"])
	claims.IssuedAt = unixClaim(raw["iat"])
	if claims.Issuer == "" || claims.Subject == "" || claims.ExpiresAt.IsZero() {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

func stringSliceClaim(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func unixClaim(v any) time.Time {
	switch x := v.(type) {
	case float64:
		return time.Unix(int64(x), 0)
	case json.Number:
		n, _ := x.Int64()
		return time.Unix(n, 0)
	default:
		return time.Time{}
	}
}

func audienceAllowed(tokenAud, allowed []string) bool {
	for _, a := range allowed {
		for _, t := range tokenAud {
			if subtle.ConstantTimeCompare([]byte(a), []byte(t)) == 1 {
				return true
			}
		}
	}
	return false
}

func decodeJSON(part string, dst any) error {
	data, err := base64.RawURLEncoding.DecodeString(part)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

func intFromBytes(data []byte) *big.Int {
	return new(big.Int).SetBytes(data)
}
