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
	"log/slog"
	"net/http"
	"net/url"
	"path"
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
	AccessToken         string    `json:"access_token"`
	RefreshToken        string    `json:"refresh_token,omitempty"`
	IDToken             string    `json:"id_token,omitempty"`
	ZitadelSessionID    string    `json:"zitadel_session_id,omitempty"`
	ZitadelSessionToken string    `json:"zitadel_session_token,omitempty"`
	ExpiresAt           time.Time `json:"expires_at"`
	AbsoluteEnd         time.Time `json:"absolute_end"`
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
	RevocationEndpoint    string `json:"revocation_endpoint"`
}

type zitadelSessionResponse struct {
	SessionID    string `json:"sessionId"`
	SessionToken string `json:"sessionToken"`
}

type zitadelSessionState struct {
	Session struct {
		Factors struct {
			User struct {
				ID             string `json:"id"`
				LoginName      string `json:"loginName"`
				OrganizationID string `json:"organizationId"`
			} `json:"user"`
			Password struct {
				VerifiedAt string `json:"verifiedAt"`
			} `json:"password"`
		} `json:"factors"`
	} `json:"session"`
}

type zitadelLoginSettings struct {
	Settings struct {
		ForceMFA          bool `json:"forceMfa"`
		ForceMFALocalOnly bool `json:"forceMfaLocalOnly"`
	} `json:"settings"`
}

type zitadelCallbackResponse struct {
	CallbackURL string `json:"callbackUrl"`
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
	if w.DirectLoginEnabled && strings.TrimSpace(w.LoginClientToken) == "" {
		return nil, fmt.Errorf("direct web login requires login_client_token")
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

func (s *WebService) DirectLoginEnabled() bool {
	return s.Enabled() && s.web.DirectLoginEnabled
}

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

func (s *WebService) ValidateLoginOrigin(origin string) error {
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
	record := sessionRecord{AccessToken: tokens.AccessToken, RefreshToken: tokens.RefreshToken, IDToken: tokens.IDToken, ExpiresAt: now.Add(time.Duration(tokens.ExpiresIn) * time.Second), AbsoluteEnd: now.Add(s.web.SessionTTL)}
	if err := s.storeEncrypted(ctx, sessionKey(id), record, s.web.SessionTTL); err != nil {
		return "", "", err
	}
	return id, flow.ReturnURL, nil
}

// Login performs the browser-facing password step through Zitadel's Session
// API, while completing the authorization-code flow entirely on the server.
// The browser never handles the OIDC callback or any Zitadel token.
func (s *WebService) Login(ctx context.Context, loginName, password, returnURL string) (string, string, error) {
	if !s.DirectLoginEnabled() {
		return "", "", authnext.ErrDisabled
	}
	loginName = strings.TrimSpace(loginName)
	if loginName == "" || password == "" {
		return "", "", authnext.ErrInvalidCredentials
	}
	authorizationURL, state, err := s.Begin(ctx, "login", returnURL)
	if err != nil {
		return "", "", err
	}
	defer s.redis.Del(context.Background(), flowKey(state))

	authRequestID, err := s.initializeAuthRequest(ctx, authorizationURL)
	if err != nil {
		return "", "", err
	}
	zitadelSession, err := s.createPasswordSession(ctx, loginName, password)
	if err != nil {
		return "", "", err
	}
	keepZitadelSession := false
	defer func() {
		if !keepZitadelSession {
			s.deleteZitadelSession(context.Background(), zitadelSession.SessionID, zitadelSession.SessionToken)
		}
	}()

	callbackURL, err := s.finalizeAuthRequest(ctx, authRequestID, zitadelSession)
	if err != nil {
		return "", "", err
	}
	callback, err := url.Parse(callbackURL)
	if err != nil || !sameEndpoint(callback, s.web.RedirectURL) || callback.Query().Get("state") != state {
		return "", "", ErrInvalidFlow
	}
	sessionID, resolvedReturnURL, err := s.Callback(ctx, callback.Query().Get("code"), state, state)
	if err != nil {
		return "", "", err
	}
	record, err := s.loadSession(ctx, sessionID)
	if err != nil {
		return "", "", err
	}
	record.ZitadelSessionID = zitadelSession.SessionID
	record.ZitadelSessionToken = zitadelSession.SessionToken
	remaining := record.AbsoluteEnd.Sub(s.now())
	if remaining <= 0 || s.storeEncrypted(ctx, sessionKey(sessionID), record, remaining) != nil {
		_ = s.redis.Del(ctx, sessionKey(sessionID)).Err()
		return "", "", ErrInvalidSession
	}
	keepZitadelSession = true
	return sessionID, resolvedReturnURL, nil
}

func (s *WebService) initializeAuthRequest(ctx context.Context, authorizationURL string) (string, error) {
	client := *s.client
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, authorizationURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("initialize oidc auth request: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode/100 != 3 {
		return "", fmt.Errorf("initialize oidc auth request: status %d", resp.StatusCode)
	}
	location, err := resp.Location()
	if err != nil {
		return "", fmt.Errorf("initialize oidc auth request: %w", err)
	}
	requestID := location.Query().Get("authRequestID")
	if requestID == "" {
		requestID = location.Query().Get("authRequest")
	}
	if requestID == "" {
		return "", fmt.Errorf("initialize oidc auth request: missing request id")
	}
	return requestID, nil
}

func (s *WebService) createPasswordSession(ctx context.Context, loginName, password string) (zitadelSessionResponse, error) {
	payload := map[string]any{
		"checks": map[string]any{
			"user":     map[string]string{"loginName": loginName},
			"password": map[string]string{"password": password},
		},
		"lifetime": fmt.Sprintf("%ds", max(1, int64(s.web.SessionTTL.Seconds()))),
	}
	var created zitadelSessionResponse
	status, err := s.doZitadelJSON(ctx, http.MethodPost, "/v2/sessions", s.web.LoginClientToken, payload, &created)
	if err != nil {
		if status >= 400 && status < 500 {
			return zitadelSessionResponse{}, authnext.ErrInvalidCredentials
		}
		return zitadelSessionResponse{}, err
	}
	if created.SessionID == "" || created.SessionToken == "" {
		return zitadelSessionResponse{}, authnext.ErrInvalidCredentials
	}

	var state zitadelSessionState
	status, err = s.doZitadelJSON(ctx, http.MethodGet, "/v2/sessions/"+url.PathEscape(created.SessionID), created.SessionToken, nil, &state)
	if err != nil || status/100 != 2 || state.Session.Factors.User.ID == "" || state.Session.Factors.Password.VerifiedAt == "" {
		s.deleteZitadelSession(context.Background(), created.SessionID, created.SessionToken)
		return zitadelSessionResponse{}, authnext.ErrInvalidCredentials
	}

	settingsPath := "/v2/settings/login?ctx.orgId=" + url.QueryEscape(state.Session.Factors.User.OrganizationID)
	var settings zitadelLoginSettings
	if _, err := s.doZitadelJSON(ctx, http.MethodGet, settingsPath, s.web.LoginClientToken, nil, &settings); err != nil {
		s.deleteZitadelSession(context.Background(), created.SessionID, created.SessionToken)
		return zitadelSessionResponse{}, fmt.Errorf("read Zitadel login policy: %w", err)
	}
	if settings.Settings.ForceMFA || settings.Settings.ForceMFALocalOnly {
		s.deleteZitadelSession(context.Background(), created.SessionID, created.SessionToken)
		return zitadelSessionResponse{}, authnext.ErrAdditionalVerification
	}
	return created, nil
}

func (s *WebService) finalizeAuthRequest(ctx context.Context, authRequestID string, session zitadelSessionResponse) (string, error) {
	payload := map[string]any{"session": map[string]string{"sessionId": session.SessionID, "sessionToken": session.SessionToken}}
	var callback zitadelCallbackResponse
	_, err := s.doZitadelJSON(ctx, http.MethodPost, "/v2/oidc/auth_requests/"+url.PathEscape(authRequestID), s.web.LoginClientToken, payload, &callback)
	if err != nil {
		return "", fmt.Errorf("finalize oidc auth request: %w", err)
	}
	if callback.CallbackURL == "" {
		return "", ErrInvalidFlow
	}
	return callback.CallbackURL, nil
}

func (s *WebService) doZitadelJSON(ctx context.Context, method, requestPath, token string, payload, dst any) (int, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return 0, err
		}
		body = strings.NewReader(string(data))
	}
	endpoint := strings.TrimRight(s.cfg.Issuer, "/") + requestPath
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("Zitadel API request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return resp.StatusCode, fmt.Errorf("Zitadel API request status %d", resp.StatusCode)
	}
	if dst == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return resp.StatusCode, nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(dst); err != nil {
		return resp.StatusCode, err
	}
	return resp.StatusCode, nil
}

func sameEndpoint(callback *url.URL, configured string) bool {
	expected, err := url.Parse(configured)
	if err != nil || callback == nil {
		return false
	}
	return callback.Scheme == expected.Scheme && callback.Host == expected.Host && path.Clean(callback.Path) == path.Clean(expected.Path)
}

// Logout destroys the local session, best-effort revokes the refresh token at
// the IdP, and returns the RP-initiated logout URL the browser must visit so
// Zitadel's own SSO session ends too. Falls back to PostLogoutURL when the IdP
// exposes no end_session_endpoint or the session is already gone.
func (s *WebService) Logout(ctx context.Context, cookieHeader string) string {
	id, err := cookieValue(cookieHeader, s.web.CookieName)
	if err != nil {
		return s.web.PostLogoutURL
	}
	record, loadErr := s.loadSession(ctx, id)
	_ = s.redis.Del(ctx, sessionKey(id)).Err()
	if loadErr != nil {
		return s.web.PostLogoutURL
	}
	if record.RefreshToken != "" {
		s.revokeToken(ctx, record.RefreshToken)
	}
	if record.ZitadelSessionID != "" && record.ZitadelSessionToken != "" {
		s.deleteZitadelSession(ctx, record.ZitadelSessionID, record.ZitadelSessionToken)
	}
	if s.discovery.EndSessionEndpoint == "" || record.IDToken == "" {
		return s.web.PostLogoutURL
	}
	q := url.Values{"id_token_hint": {record.IDToken}, "client_id": {s.web.ClientID}}
	if s.web.PostLogoutURL != "" {
		q.Set("post_logout_redirect_uri", s.web.PostLogoutURL)
	}
	return s.discovery.EndSessionEndpoint + "?" + q.Encode()
}

func (s *WebService) deleteZitadelSession(ctx context.Context, sessionID, sessionToken string) {
	if sessionID == "" || sessionToken == "" {
		return
	}
	payload := map[string]string{"sessionToken": sessionToken}
	if _, err := s.doZitadelJSON(ctx, http.MethodDelete, "/v2/sessions/"+url.PathEscape(sessionID), sessionToken, payload, nil); err != nil {
		slog.Warn("Zitadel session deletion failed", "err", err)
	}
}

// revokeToken is best-effort: a failed revocation must not block logout, but it
// is logged so operators can see refresh tokens outliving their sessions.
func (s *WebService) revokeToken(ctx context.Context, token string) {
	if s.discovery.RevocationEndpoint == "" {
		return
	}
	values := url.Values{"token": {token}, "token_type_hint": {"refresh_token"}, "client_id": {s.web.ClientID}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.discovery.RevocationEndpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.client.Do(req)
	if err != nil {
		slog.Warn("oidc refresh token revocation failed", "err", err)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode/100 != 2 {
		slog.Warn("oidc refresh token revocation failed", "status", resp.StatusCode)
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
