package authn

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/redis/go-redis/v9"
)

var (
	ErrInvalidSession = errors.New("invalid browser session")
	ErrInvalidFlow    = errors.New("invalid oidc flow")
	ErrCSRF           = errors.New("cross-site request rejected")
)

type sessionRecord struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
	AbsoluteEnd  time.Time `json:"absolute_end"`
}

type flowRecord struct {
	Verifier  string `json:"verifier"`
	Nonce     string `json:"nonce"`
	ReturnURL string `json:"return_url"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

type discoveryDocument struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	EndSessionEndpoint    string `json:"end_session_endpoint"`
}

type WebService struct {
	cfg        authnext.Config
	web        authnext.WebConfig
	verifier   *authnext.Verifier
	idVerifier *authnext.Verifier
	redis      redis.UniversalClient
	aead       cipher.AEAD
	client     *http.Client
	now        func() time.Time
	discovery  discoveryDocument
}

func NewWebService(ctx context.Context, cfg authnext.Config, verifier *authnext.Verifier, store redis.UniversalClient) (*WebService, error) {
	if verifier == nil {
		return nil, fmt.Errorf("web authn requires token verifier")
	}
	if !cfg.Web.Enabled {
		return &WebService{cfg: cfg, web: cfg.Web, verifier: verifier}, nil
	}
	if store == nil {
		return nil, fmt.Errorf("web authn requires redis")
	}
	w := cfg.Web
	if w.ClientID == "" || w.RedirectURL == "" || w.PostLoginURL == "" || len(cfg.Audience) == 0 {
		return nil, fmt.Errorf("web authn requires client_id, redirect_url, and post_login_url")
	}
	if !slices.Contains(w.AllowedReturnURLs, w.PostLoginURL) {
		return nil, fmt.Errorf("post_login_url must be in allowed_return_urls")
	}
	if w.CookieName == "" {
		w.CookieName = "cp_session"
	}
	if w.SessionTTL <= 0 {
		w.SessionTTL = 8 * time.Hour
	}
	if w.FlowTTL <= 0 {
		w.FlowTTL = 5 * time.Minute
	}
	key, err := encryptionKey(w.EncryptionKey)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	doc, err := discover(ctx, client, cfg.Issuer)
	if err != nil {
		return nil, err
	}
	idCfg := cfg
	idCfg.Audience = []string{w.ClientID}
	idCfg.Web.Enabled = false
	idVerifier, err := authnext.NewVerifier(idCfg)
	if err != nil {
		return nil, err
	}
	return &WebService{cfg: cfg, web: w, verifier: verifier, idVerifier: idVerifier, redis: store, aead: aead, client: client, now: time.Now, discovery: doc}, nil
}

func (s *WebService) Enabled() bool { return s != nil && s.web.Enabled }

func (s *WebService) Authenticate(ctx context.Context, authorization, cookieHeader string) (*authnext.Claims, error) {
	if strings.TrimSpace(authorization) != "" {
		return s.verifier.VerifyAuthorization(ctx, authorization)
	}
	if !s.Enabled() {
		return nil, authnext.ErrMissingBearer
	}
	id, err := cookieValue(cookieHeader, s.web.CookieName)
	if err != nil {
		return nil, ErrInvalidSession
	}
	record, err := s.loadSession(ctx, id)
	if err != nil {
		return nil, ErrInvalidSession
	}
	if !s.now().Before(record.AbsoluteEnd) {
		_ = s.redis.Del(ctx, sessionKey(id)).Err()
		return nil, ErrInvalidSession
	}
	claims, err := s.verifier.Verify(ctx, record.AccessToken)
	if err == nil {
		return claims, nil
	}
	if !errors.Is(err, authnext.ErrExpiredToken) || record.RefreshToken == "" {
		return nil, err
	}
	if err := s.refresh(ctx, id, &record); err != nil {
		return nil, ErrInvalidSession
	}
	return s.verifier.Verify(ctx, record.AccessToken)
}

func (s *WebService) ValidateCSRF(method, origin, cookieHeader, authorization string) error {
	if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions || method == http.MethodTrace || strings.TrimSpace(authorization) != "" {
		return nil
	}
	if _, err := cookieValue(cookieHeader, s.web.CookieName); err != nil {
		return nil
	}
	if origin == "" || !slices.Contains(s.web.AllowedOrigins, origin) {
		return ErrCSRF
	}
	return nil
}

func (s *WebService) Begin(ctx context.Context, mode, returnURL string) (string, string, error) {
	if !s.Enabled() {
		return "", "", authnext.ErrDisabled
	}
	if returnURL == "" {
		returnURL = s.web.PostLoginURL
	}
	if !slices.Contains(s.web.AllowedReturnURLs, returnURL) {
		return "", "", fmt.Errorf("%w: return url", ErrInvalidFlow)
	}
	state, err := randomToken(32)
	if err != nil {
		return "", "", err
	}
	verifier, err := randomToken(48)
	if err != nil {
		return "", "", err
	}
	nonce, err := randomToken(32)
	if err != nil {
		return "", "", err
	}
	if err := s.storeEncrypted(ctx, flowKey(state), flowRecord{Verifier: verifier, Nonce: nonce, ReturnURL: returnURL}, s.web.FlowTTL); err != nil {
		return "", "", err
	}
	challenge := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"response_type": {"code"}, "client_id": {s.web.ClientID}, "redirect_uri": {s.web.RedirectURL},
		"scope": {strings.Join(s.scopes(), " ")}, "state": {state}, "nonce": {nonce},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}, "code_challenge_method": {"S256"},
	}
	if mode == "register" {
		q.Set("prompt", "create")
	} else {
		q.Set("prompt", "login")
	}
	return s.discovery.AuthorizationEndpoint + "?" + q.Encode(), state, nil
}

func (s *WebService) Callback(ctx context.Context, code, state, flowCookie string) (string, string, error) {
	if code == "" || state == "" || subtle.ConstantTimeCompare([]byte(state), []byte(flowCookie)) != 1 {
		return "", "", ErrInvalidFlow
	}
	data, err := s.redis.GetDel(ctx, flowKey(state)).Bytes()
	if err != nil {
		return "", "", ErrInvalidFlow
	}
	var flow flowRecord
	if err := s.open(flowKey(state), data, &flow); err != nil {
		return "", "", ErrInvalidFlow
	}
	tokens, err := s.exchange(ctx, url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {s.web.RedirectURL},
		"client_id": {s.web.ClientID}, "code_verifier": {flow.Verifier},
	})
	if err != nil {
		return "", "", err
	}
	idClaims, err := s.idVerifier.Verify(ctx, tokens.IDToken)
	if err != nil || idClaims.Raw["nonce"] != flow.Nonce {
		return "", "", ErrInvalidFlow
	}
	if _, err := s.verifier.Verify(ctx, tokens.AccessToken); err != nil {
		return "", "", fmt.Errorf("verify access token: %w", err)
	}
	id, err := randomToken(32)
	if err != nil {
		return "", "", err
	}
	now := s.now().UTC()
	record := sessionRecord{AccessToken: tokens.AccessToken, RefreshToken: tokens.RefreshToken, ExpiresAt: now.Add(time.Duration(tokens.ExpiresIn) * time.Second), AbsoluteEnd: now.Add(s.web.SessionTTL)}
	if err := s.storeEncrypted(ctx, sessionKey(id), record, s.web.SessionTTL); err != nil {
		return "", "", err
	}
	return id, flow.ReturnURL, nil
}

func (s *WebService) Logout(ctx context.Context, cookieHeader string) {
	if id, err := cookieValue(cookieHeader, s.web.CookieName); err == nil {
		_ = s.redis.Del(ctx, sessionKey(id)).Err()
	}
}

func (s *WebService) SessionCookie(value string) string {
	return (&http.Cookie{Name: s.web.CookieName, Value: value, Path: "/", HttpOnly: true, Secure: s.web.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: int(s.web.SessionTTL.Seconds())}).String()
}

func (s *WebService) FlowCookie(value string) string {
	return (&http.Cookie{Name: s.web.CookieName + "_oidc", Value: value, Path: "/authn/oidc/callback", HttpOnly: true, Secure: s.web.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: int(s.web.FlowTTL.Seconds())}).String()
}

func (s *WebService) ClearCookie() string {
	return (&http.Cookie{Name: s.web.CookieName, Value: "", Path: "/", HttpOnly: true, Secure: s.web.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: -1}).String()
}

func (s *WebService) FlowCookieName() string { return s.web.CookieName + "_oidc" }
func (s *WebService) PostLogoutURL() string  { return s.web.PostLogoutURL }

func (s *WebService) FlowState(cookieHeader string) (string, error) {
	return cookieValue(cookieHeader, s.FlowCookieName())
}

func (s *WebService) loadSession(ctx context.Context, id string) (sessionRecord, error) {
	data, err := s.redis.Get(ctx, sessionKey(id)).Bytes()
	if err != nil {
		return sessionRecord{}, err
	}
	var record sessionRecord
	if err := s.open(sessionKey(id), data, &record); err != nil {
		return sessionRecord{}, err
	}
	return record, nil
}

func (s *WebService) refresh(ctx context.Context, id string, record *sessionRecord) error {
	lockKey := sessionKey(id) + ":refresh"
	locked, err := s.redis.SetNX(ctx, lockKey, "1", 10*time.Second).Result()
	if err != nil {
		return err
	}
	if !locked {
		deadline := time.NewTimer(2 * time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-deadline.C:
				return fmt.Errorf("refresh wait timeout")
			case <-ticker.C:
				exists, err := s.redis.Exists(ctx, lockKey).Result()
				if err != nil {
					return err
				}
				if exists == 0 {
					fresh, err := s.loadSession(ctx, id)
					if err != nil {
						return err
					}
					*record = fresh
					return nil
				}
			}
		}
	}
	defer s.redis.Del(context.Background(), lockKey)
	fresh, err := s.loadSession(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.verifier.Verify(ctx, fresh.AccessToken); err == nil {
		*record = fresh
		return nil
	}
	*record = fresh
	tokens, err := s.exchange(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {record.RefreshToken}, "client_id": {s.web.ClientID}})
	if err != nil {
		_ = s.redis.Del(ctx, sessionKey(id)).Err()
		return err
	}
	record.AccessToken = tokens.AccessToken
	if tokens.RefreshToken != "" {
		record.RefreshToken = tokens.RefreshToken
	}
	record.ExpiresAt = s.now().UTC().Add(time.Duration(tokens.ExpiresIn) * time.Second)
	remaining := record.AbsoluteEnd.Sub(s.now())
	if remaining <= 0 {
		return ErrInvalidSession
	}
	return s.storeEncrypted(ctx, sessionKey(id), *record, remaining)
}

func (s *WebService) exchange(ctx context.Context, values url.Values) (tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.discovery.TokenEndpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.client.Do(req)
	if err != nil {
		return tokenResponse{}, fmt.Errorf("oidc token request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return tokenResponse{}, fmt.Errorf("oidc token request status %d", resp.StatusCode)
	}
	var tokens tokenResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tokens); err != nil {
		return tokenResponse{}, err
	}
	if tokens.AccessToken == "" || tokens.ExpiresIn <= 0 {
		return tokenResponse{}, fmt.Errorf("oidc token response incomplete")
	}
	return tokens, nil
}

func (s *WebService) scopes() []string {
	if len(s.web.Scopes) > 0 {
		return s.web.Scopes
	}
	return []string{"openid", "profile", "email", "offline_access", "urn:zitadel:iam:org:project:id:" + s.cfg.Audience[0] + ":aud"}
}

func (s *WebService) storeEncrypted(ctx context.Context, key string, value any, ttl time.Duration) error {
	plain, err := json.Marshal(value)
	if err != nil {
		return err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	sealed := s.aead.Seal(nonce, nonce, plain, []byte(key))
	return s.redis.Set(ctx, key, sealed, ttl).Err()
}

func (s *WebService) open(key string, sealed []byte, dst any) error {
	if len(sealed) < s.aead.NonceSize() {
		return ErrInvalidSession
	}
	nonce, ciphertext := sealed[:s.aead.NonceSize()], sealed[s.aead.NonceSize():]
	plain, err := s.aead.Open(nil, nonce, ciphertext, []byte(key))
	if err != nil {
		return err
	}
	return json.Unmarshal(plain, dst)
}

func discover(ctx context.Context, client *http.Client, issuer string) (discoveryDocument, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(issuer, "/")+"/.well-known/openid-configuration", nil)
	if err != nil {
		return discoveryDocument{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return discoveryDocument{}, fmt.Errorf("discover oidc web endpoints: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return discoveryDocument{}, fmt.Errorf("discover oidc web endpoints: status %d", resp.StatusCode)
	}
	var doc discoveryDocument
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc); err != nil {
		return discoveryDocument{}, err
	}
	if doc.AuthorizationEndpoint == "" || doc.TokenEndpoint == "" {
		return discoveryDocument{}, fmt.Errorf("oidc discovery missing browser endpoints")
	}
	return doc, nil
}

func encryptionKey(value string) ([]byte, error) {
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if len(value) == 32 {
		return []byte(value), nil
	}
	return nil, fmt.Errorf("web authn encryption_key must encode exactly 32 bytes")
}

func randomToken(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func cookieValue(header, name string) (string, error) {
	req := http.Request{Header: http.Header{"Cookie": []string{header}}}
	cookie, err := req.Cookie(name)
	if err != nil || cookie.Value == "" {
		return "", http.ErrNoCookie
	}
	return cookie.Value, nil
}

func flowKey(state string) string { return "authn:flow:" + hashID(state) }
func sessionKey(id string) string { return "authn:session:" + hashID(id) }
func hashID(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
