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
)

const (
	defaultHTTPTimeout = 5 * time.Second
	defaultClockSkew   = 30 * time.Second

	SubjectTypePrincipal      = "principal"
	SubjectTypeOAuthClient    = "oauth_client"
	SubjectTypeService        = "service"
	SubjectTypeServiceAccount = "service_account"
)

var (
	ErrDisabled               = errors.New("authn disabled")
	ErrInvalidToken           = errors.New("invalid token")
	ErrExpiredToken           = errors.New("token expired")
	ErrUnknownKey             = errors.New("unknown jwks key")
	ErrInvalidIssuer          = errors.New("invalid issuer")
	ErrInvalidAud             = errors.New("invalid audience")
	ErrInvalidCredentials     = errors.New("invalid login credentials")
	ErrAdditionalVerification = errors.New("additional verification required")
	ErrRegistrationDisabled   = errors.New("self-service registration disabled")
	ErrInvalidRegistration    = errors.New("invalid self-service registration")
	ErrRegistrationConflict   = errors.New("self-service registration identity exists")
	ErrUnavailable            = errors.New("authentication unavailable")
)

// Config describes the local issuer and optional upstream JWT verification.
type Config struct {
	Enabled           bool                    `mapstructure:"enabled" description:"enable local identity and authentication" default:"false"`
	Issuer            string                  `mapstructure:"issuer" description:"canonical local OAuth/OIDC issuer URL"`
	Audience          []string                `mapstructure:"audience" description:"accepted JWT audience values; empty skips audience check"`
	JWKSURL           string                  `mapstructure:"jwks_url" description:"optional upstream JWKS URL for federation adapters"`
	HTTPTimeout       time.Duration           `mapstructure:"http_timeout" description:"upstream discovery/JWKS HTTP timeout" default:"5s"`
	ClockSkew         time.Duration           `mapstructure:"clock_skew" description:"allowed token clock skew" default:"30s"`
	SigningKey        string                  `mapstructure:"signing_key" description:"base64 Ed25519 seed; prefer signing_key_file" default:""`
	SigningKeyFile    string                  `mapstructure:"signing_key_file" description:"file containing the base64 Ed25519 seed" default:""`
	AccessTokenTTL    time.Duration           `mapstructure:"access_token_ttl" description:"local OAuth access token lifetime" default:"15m"`
	Web               WebConfig               `mapstructure:"web" group:"web"`
	MFA               MFAConfig               `mapstructure:"mfa" group:"mfa"`
	Passkey           PasskeyConfig           `mapstructure:"passkey" group:"passkey"`
	Notification      NotificationConfig      `mapstructure:"notification" group:"notification"`
	Recovery          RecoveryConfig          `mapstructure:"recovery" group:"recovery"`
	EmailVerification EmailVerificationConfig `mapstructure:"email_verification" group:"email_verification"`
	Registration      RegistrationConfig      `mapstructure:"registration" group:"registration"`
}

// RegistrationConfig enables email-verified self-service global principals.
// Tenant membership is deliberately granted only through invitations.
type RegistrationConfig struct {
	Enabled bool `mapstructure:"enabled" description:"enable email-verified self-service registration" default:"false"`
}

type Capabilities struct {
	Registration     bool `json:"registration"`
	PasswordRecovery bool `json:"password_recovery"`
	Passkey          bool `json:"passkey"`
}

// NotificationConfig controls the shared durable webhook delivery used by
// authentication notifications. Payloads remain encrypted while queued.
type NotificationConfig struct {
	URL               string        `mapstructure:"url" description:"HTTPS webhook receiving authentication notification events"`
	Authorization     string        `mapstructure:"authorization" description:"optional Authorization header; prefer authorization_file" default:""`
	AuthorizationFile string        `mapstructure:"authorization_file" description:"file containing the optional notification Authorization header" default:""`
	PollInterval      time.Duration `mapstructure:"poll_interval" description:"notification outbox polling interval" default:"5s"`
	RequestTimeout    time.Duration `mapstructure:"request_timeout" description:"notification webhook request timeout" default:"10s"`
	MaxAttempts       int           `mapstructure:"max_attempts" description:"notification delivery attempts before permanent failure" default:"10"`
}

// RecoveryConfig controls password recovery policy. Delivery uses the shared
// Notification configuration.
type RecoveryConfig struct {
	Enabled  bool          `mapstructure:"enabled" description:"enable password recovery" default:"false"`
	TokenTTL time.Duration `mapstructure:"token_ttl" description:"single-use password recovery credential lifetime" default:"15m"`
	Cooldown time.Duration `mapstructure:"cooldown" description:"post-recovery restriction window for high-risk account changes" default:"24h"`
	ResetURL string        `mapstructure:"reset_url" description:"absolute browser URL used to complete password recovery"`
}

// EmailVerificationConfig controls proof of ownership for the current primary
// email. Delivery uses the shared Notification configuration.
type EmailVerificationConfig struct {
	Enabled   bool          `mapstructure:"enabled" description:"enable primary email verification" default:"false"`
	TokenTTL  time.Duration `mapstructure:"token_ttl" description:"single-use email verification credential lifetime" default:"24h"`
	VerifyURL string        `mapstructure:"verify_url" description:"absolute browser URL used to complete email verification"`
}

// PasskeyConfig pins the WebAuthn relying party trust boundary. Values are
// deployment configuration and are never inferred from browser headers.
type PasskeyConfig struct {
	Enabled        bool          `mapstructure:"enabled" description:"enable passkey registration and passwordless login" default:"false"`
	RPID           string        `mapstructure:"rp_id" description:"WebAuthn relying party domain without scheme or port"`
	DisplayName    string        `mapstructure:"display_name" description:"relying party name shown by authenticators" default:"Chaosplus"`
	Origins        []string      `mapstructure:"origins" description:"exact trusted WebAuthn browser origins"`
	ChallengeTTL   time.Duration `mapstructure:"challenge_ttl" description:"one-time registration and login challenge lifetime" default:"5m"`
	MaxCredentials int           `mapstructure:"max_credentials" description:"maximum active passkeys per principal" default:"10"`
}

// WebConfig enables database-backed opaque browser sessions.
type WebConfig struct {
	Enabled           bool          `mapstructure:"enabled" description:"enable browser OIDC BFF" default:"false"`
	PostLoginURL      string        `mapstructure:"post_login_url" description:"default frontend URL after login"`
	PostLogoutURL     string        `mapstructure:"post_logout_url" description:"frontend URL after logout"`
	AllowedReturnURLs []string      `mapstructure:"allowed_return_urls" description:"exact frontend return URL allowlist"`
	AllowedOrigins    []string      `mapstructure:"allowed_origins" description:"origins allowed for cookie-authenticated writes"`
	CookieName        string        `mapstructure:"cookie_name" description:"opaque session cookie name" default:"cp_session"`
	CookieSecure      bool          `mapstructure:"cookie_secure" description:"require HTTPS for session cookies" default:"true"`
	SessionTTL        time.Duration `mapstructure:"session_ttl" description:"absolute browser session lifetime" default:"8h"`
	IdleTTL           time.Duration `mapstructure:"idle_ttl" description:"browser session inactivity lifetime" default:"30m"`
}

// MFAConfig controls trusted storage and short-lived state for local MFA.
type MFAConfig struct {
	Issuer            string        `mapstructure:"issuer" description:"TOTP issuer shown by authenticator applications" default:"Chaosplus"`
	EncryptionKey     string        `mapstructure:"encryption_key" description:"base64 32-byte AES key; prefer encryption_key_file" default:""`
	EncryptionKeyFile string        `mapstructure:"encryption_key_file" description:"file containing the base64 32-byte MFA encryption key" default:""`
	EnrollmentTTL     time.Duration `mapstructure:"enrollment_ttl" description:"lifetime of an unconfirmed TOTP enrollment" default:"10m"`
	ChallengeTTL      time.Duration `mapstructure:"challenge_ttl" description:"lifetime of a password-verified MFA login challenge" default:"5m"`
	RecoveryCodes     int           `mapstructure:"recovery_codes" description:"one-time recovery codes issued after enrollment" default:"10"`
	MaxAttempts       int           `mapstructure:"max_attempts" description:"failed codes allowed per login challenge" default:"5"`
}

// Claims contains the identity fields required by authentication and guards.
type Claims struct {
	Issuer            string         `json:"iss"`
	Subject           string         `json:"sub"`
	SubjectType       string         `json:"subject_type,omitempty"`
	Audience          []string       `json:"aud"`
	ExpiresAt         time.Time      `json:"exp"`
	NotBefore         time.Time      `json:"nbf,omitempty"`
	IssuedAt          time.Time      `json:"iat,omitempty"`
	PreferredUsername string         `json:"preferred_username,omitempty"`
	Email             string         `json:"email,omitempty"`
	EmailVerified     bool           `json:"email_verified,omitempty"`
	OrganizationID    string         `json:"organization_id,omitempty"`
	CredentialVersion int64          `json:"credential_version,omitempty"`
	AuthTime          time.Time      `json:"auth_time,omitempty"`
	ACR               int            `json:"acr,omitempty"`
	AMR               []string       `json:"amr,omitempty"`
	ClientID          string         `json:"client_id,omitempty"`
	NetworkZone       string         `json:"network_zone,omitempty"`
	Raw               map[string]any `json:"raw,omitempty"`
}

type Assurance struct {
	AuthTime    time.Time
	Level       int
	Methods     []string
	ClientID    string
	NetworkZone string
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
	claims.SubjectType, _ = raw["subject_type"].(string)
	claims.PreferredUsername, _ = raw["preferred_username"].(string)
	claims.Email, _ = raw["email"].(string)
	claims.EmailVerified, _ = raw["email_verified"].(bool)
	claims.OrganizationID, _ = raw["organization_id"].(string)
	claims.CredentialVersion = int64Claim(raw["credential_version"])
	claims.AuthTime = unixClaim(raw["auth_time"])
	claims.ACR = int(int64Claim(raw["acr"]))
	claims.AMR = stringSliceClaim(raw["amr"])
	claims.ClientID, _ = raw["client_id"].(string)
	claims.NetworkZone, _ = raw["network_zone"].(string)
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

func int64Claim(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case json.Number:
		n, _ := x.Int64()
		return n
	default:
		return 0
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
