package authn

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
)

type oidcHarness struct {
	server          *httptest.Server
	key             *rsa.PrivateKey
	clientID        string
	audience        string
	nonce           string
	challenge       string
	refreshCalls    atomic.Int32
	revokeCalls     atomic.Int32
	revokedToken    string
	revokeStatus    int
	badNonce        bool
	refreshDelay    time.Duration
	tokenStatus     int
	badTokenJSON    bool
	incompleteToken bool
	directState     string
	directMFA       bool
	directDeleted   atomic.Bool
	mu              sync.Mutex
}

func newOIDCHarness(t *testing.T) *oidcHarness {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	h := &oidcHarness{key: key, clientID: "web-client", audience: "api-audience"}
	h.server = httptest.NewServer(http.HandlerFunc(h.serveHTTP))
	t.Cleanup(h.server.Close)
	return h
}

func (h *oidcHarness) serveHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		_ = json.NewEncoder(w).Encode(map[string]string{"authorization_endpoint": h.server.URL + "/authorize", "token_endpoint": h.server.URL + "/token", "jwks_uri": h.server.URL + "/jwks", "end_session_endpoint": h.server.URL + "/end_session", "revocation_endpoint": h.server.URL + "/revoke"})
	case "/revoke":
		_ = r.ParseForm()
		h.mu.Lock()
		h.revokedToken = r.Form.Get("token")
		h.mu.Unlock()
		h.revokeCalls.Add(1)
		if h.revokeStatus != 0 {
			http.Error(w, "revoke failed", h.revokeStatus)
		}
	case "/jwks":
		e := big.NewInt(int64(h.key.PublicKey.E)).Bytes()
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{"kty": "RSA", "kid": "test-key", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(h.key.PublicKey.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(e)}}})
	case "/authorize":
		h.nonce = r.URL.Query().Get("nonce")
		h.challenge = r.URL.Query().Get("code_challenge")
		h.directState = r.URL.Query().Get("state")
		http.Redirect(w, r, h.server.URL+"/ui/login/login?authRequestID=oidc-direct", http.StatusFound)
	case "/v2/sessions":
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer login-client-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var payload struct {
			Checks struct {
				User struct {
					LoginName string `json:"loginName"`
				} `json:"user"`
				Password struct {
					Password string `json:"password"`
				} `json:"password"`
			} `json:"checks"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload.Checks.User.LoginName != "alice" || payload.Checks.Password.Password != "correct-password" {
			http.Error(w, "invalid credentials", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"sessionId": "direct-session", "sessionToken": "direct-token"})
	case "/v2/sessions/direct-session":
		if r.Method == http.MethodDelete {
			h.directDeleted.Store(true)
			_ = json.NewEncoder(w).Encode(map[string]any{"details": map[string]string{"sequence": "1"}})
			return
		}
		if r.Header.Get("Authorization") != "Bearer direct-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"session": map[string]any{"factors": map[string]any{"user": map[string]string{"id": "user-1", "loginName": "alice", "organizationId": "org-1"}, "password": map[string]string{"verifiedAt": time.Now().UTC().Format(time.RFC3339)}}}})
	case "/v2/settings/login":
		if r.Header.Get("Authorization") != "Bearer login-client-token" || r.URL.Query().Get("ctx.orgId") != "org-1" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"settings": map[string]bool{"forceMfa": h.directMFA}})
	case "/v2/oidc/auth_requests/oidc-direct":
		if r.Header.Get("Authorization") != "Bearer login-client-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		callback := "http://127.0.0.1/callback?code=direct-code&state=" + url.QueryEscape(h.directState)
		_ = json.NewEncoder(w).Encode(map[string]string{"callbackUrl": callback})
	case "/token":
		if h.tokenStatus != 0 {
			http.Error(w, "token failed", h.tokenStatus)
			return
		}
		if h.badTokenJSON {
			_, _ = w.Write([]byte("{"))
			return
		}
		if h.incompleteToken {
			_ = json.NewEncoder(w).Encode(tokenResponse{})
			return
		}
		_ = r.ParseForm()
		if r.Form.Get("client_id") != h.clientID {
			http.Error(w, "bad client", http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") == "authorization_code" {
			challenge := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(challenge[:]) != h.challenge {
				http.Error(w, "bad verifier", http.StatusBadRequest)
				return
			}
		} else {
			h.refreshCalls.Add(1)
			time.Sleep(h.refreshDelay)
			if r.Form.Get("refresh_token") == "" {
				http.Error(w, "missing refresh", http.StatusBadRequest)
				return
			}
		}
		nonce := h.nonce
		if h.badNonce {
			nonce = "wrong"
		}
		now := time.Now()
		_ = json.NewEncoder(w).Encode(tokenResponse{
			AccessToken:  h.jwt(map[string]any{"iss": h.server.URL, "sub": "user-1", "aud": h.audience, "exp": now.Add(time.Hour).Unix(), "iat": now.Unix(), "preferred_username": "acceptance"}),
			IDToken:      h.jwt(map[string]any{"iss": h.server.URL, "sub": "user-1", "aud": h.clientID, "exp": now.Add(time.Hour).Unix(), "iat": now.Unix(), "nonce": nonce}),
			RefreshToken: "refresh-rotated", ExpiresIn: 3600, TokenType: "Bearer",
		})
	default:
		http.NotFound(w, r)
	}
}

func (h *oidcHarness) jwt(claims map[string]any) string {
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "test-key", "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(unsigned))
	signature, _ := rsa.SignPKCS1v15(rand.Reader, h.key, crypto.SHA256, digest[:])
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func newWebFixture(t *testing.T) (*WebService, *oidcHarness, *miniredis.Miniredis) {
	t.Helper()
	h := newOIDCHarness(t)
	mr := miniredis.RunT(t)
	store := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = store.Close() })
	cfg := authnext.Config{Enabled: true, Issuer: h.server.URL, Audience: []string{h.audience}, JWKSURL: h.server.URL + "/jwks", ClockSkew: time.Millisecond, Web: authnext.WebConfig{
		Enabled: true, ClientID: h.clientID, RedirectURL: "http://127.0.0.1/callback", PostLoginURL: "http://127.0.0.1/", PostLogoutURL: "http://127.0.0.1/login",
		AllowedReturnURLs: []string{"http://127.0.0.1/", "http://127.0.0.1/iam/users"}, AllowedOrigins: []string{"http://127.0.0.1"}, CookieName: "cp_session", SessionTTL: time.Hour, FlowTTL: time.Minute,
		EncryptionKey: "0123456789abcdef0123456789abcdef",
	}}
	verifier, err := authnext.NewVerifier(cfg)
	require.NoError(t, err)
	web, err := NewWebService(context.Background(), cfg, verifier, store)
	require.NoError(t, err)
	return web, h, mr
}

func beginFlow(t *testing.T, web *WebService, h *oidcHarness, mode string) (string, string) {
	t.Helper()
	location, state, err := web.Begin(context.Background(), mode, "http://127.0.0.1/iam/users")
	require.NoError(t, err)
	parsed, err := url.Parse(location)
	require.NoError(t, err)
	h.nonce = parsed.Query().Get("nonce")
	h.challenge = parsed.Query().Get("code_challenge")
	assert.Equal(t, "S256", parsed.Query().Get("code_challenge_method"))
	return state, parsed.Query().Get("prompt")
}

func TestWebOIDCFlowSessionAndLogout(t *testing.T) {
	web, h, mr := newWebFixture(t)
	state, prompt := beginFlow(t, web, h, "login")
	assert.Equal(t, "login", prompt)
	id, returnURL, err := web.Callback(context.Background(), "code-1", state, state)
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1/iam/users", returnURL)
	assert.NotContains(t, mustValue(mr.Get(sessionKey(id))), "refresh-rotated")

	claims, err := web.Authenticate(context.Background(), "", "cp_session="+id)
	require.NoError(t, err)
	assert.Equal(t, "user-1", claims.Subject)
	assert.NoError(t, web.ValidateCSRF(http.MethodPost, "http://127.0.0.1", "cp_session="+id, ""))
	assert.ErrorIs(t, web.ValidateCSRF(http.MethodPost, "http://evil.test", "cp_session="+id, ""), ErrCSRF)
	assert.NoError(t, web.ValidateCSRF(http.MethodGet, "", "cp_session="+id, ""))
	assert.NoError(t, web.ValidateCSRF(http.MethodPost, "", "cp_session="+id, "Bearer x"))
	assert.Contains(t, web.SessionCookie(id), "HttpOnly")
	assert.Contains(t, web.FlowCookie(state), "Path=/authn/oidc/callback")
	assert.Equal(t, state, mustValue(web.FlowState(web.FlowCookie(state))))
	assert.Contains(t, web.ClearCookie(), "Max-Age=0")
	assert.Equal(t, "http://127.0.0.1/login", web.PostLogoutURL())

	logoutURL := web.Logout(context.Background(), "cp_session="+id)
	parsed, err := url.Parse(logoutURL)
	require.NoError(t, err)
	assert.Equal(t, "/end_session", parsed.Path)
	assert.NotEmpty(t, parsed.Query().Get("id_token_hint"))
	assert.Equal(t, h.clientID, parsed.Query().Get("client_id"))
	assert.Equal(t, "http://127.0.0.1/login", parsed.Query().Get("post_logout_redirect_uri"))
	assert.Equal(t, int32(1), h.revokeCalls.Load())
	h.mu.Lock()
	assert.Equal(t, "refresh-rotated", h.revokedToken)
	h.mu.Unlock()
	_, err = web.Authenticate(context.Background(), "", "cp_session="+id)
	assert.ErrorIs(t, err, ErrInvalidSession)
	_, _, err = web.Callback(context.Background(), "code-1", state, state)
	assert.ErrorIs(t, err, ErrInvalidFlow)

	assert.Equal(t, "http://127.0.0.1/login", web.Logout(context.Background(), "cp_session="+id), "missing session falls back to post-logout url")
	assert.Equal(t, "http://127.0.0.1/login", web.Logout(context.Background(), ""), "missing cookie falls back to post-logout url")
	assert.Equal(t, int32(1), h.revokeCalls.Load(), "no further revocation without a session")
}

func TestWebDirectLoginCompletesOIDCWithoutBrowserCallback(t *testing.T) {
	web, h, _ := newWebFixture(t)
	web.web.DirectLoginEnabled = true
	web.web.LoginClientToken = "login-client-token"

	id, returnURL, err := web.Login(context.Background(), "alice", "correct-password", "http://127.0.0.1/iam/users")
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1/iam/users", returnURL)
	claims, err := web.Authenticate(context.Background(), "", "cp_session="+id)
	require.NoError(t, err)
	assert.Equal(t, "user-1", claims.Subject)
	assert.False(t, h.directDeleted.Load())

	web.Logout(context.Background(), "cp_session="+id)
	assert.True(t, h.directDeleted.Load())
}

func TestWebDirectLoginRejectsInvalidPasswordAndForcedMFA(t *testing.T) {
	web, h, _ := newWebFixture(t)
	web.web.DirectLoginEnabled = true
	web.web.LoginClientToken = "login-client-token"
	assert.ErrorIs(t, web.ValidateLoginOrigin("http://evil.test"), ErrCSRF)
	assert.NoError(t, web.ValidateLoginOrigin("http://127.0.0.1"))

	_, _, err := web.Login(context.Background(), "alice", "wrong", "http://127.0.0.1/")
	assert.ErrorIs(t, err, authnext.ErrInvalidCredentials)
	h.directMFA = true
	_, _, err = web.Login(context.Background(), "alice", "correct-password", "http://127.0.0.1/")
	assert.ErrorIs(t, err, authnext.ErrAdditionalVerification)
	assert.True(t, h.directDeleted.Load())
}

func TestWebLogoutRevocationFailureStillLogsOut(t *testing.T) {
	web, h, _ := newWebFixture(t)
	state, _ := beginFlow(t, web, h, "login")
	id, _, err := web.Callback(context.Background(), "code", state, state)
	require.NoError(t, err)
	h.revokeStatus = http.StatusBadGateway
	logoutURL := web.Logout(context.Background(), "cp_session="+id)
	assert.Contains(t, logoutURL, "/end_session", "revocation failure must not block RP-initiated logout")
	assert.Equal(t, int32(1), h.revokeCalls.Load())
	_, err = web.Authenticate(context.Background(), "", "cp_session="+id)
	assert.ErrorIs(t, err, ErrInvalidSession, "local session is destroyed even when revocation fails")
}

func TestWebRegistrationValidationAndTampering(t *testing.T) {
	web, h, mr := newWebFixture(t)
	state, prompt := beginFlow(t, web, h, "register")
	assert.Equal(t, "create", prompt)
	_, _, err := web.Callback(context.Background(), "", state, state)
	assert.ErrorIs(t, err, ErrInvalidFlow)
	_, _, err = web.Callback(context.Background(), "code", state, "other")
	assert.ErrorIs(t, err, ErrInvalidFlow)
	_, _, err = web.Begin(context.Background(), "login", "http://evil.test/")
	assert.ErrorIs(t, err, ErrInvalidFlow)

	state, _ = beginFlow(t, web, h, "login")
	h.badNonce = true
	_, _, err = web.Callback(context.Background(), "code", state, state)
	assert.ErrorIs(t, err, ErrInvalidFlow)
	h.badNonce = false
	state, _ = beginFlow(t, web, h, "login")
	id, _, err := web.Callback(context.Background(), "code", state, state)
	require.NoError(t, err)
	sealed := []byte(mustValue(mr.Get(sessionKey(id))))
	sealed[len(sealed)-1] ^= 0xff
	mr.Set(sessionKey(id), string(sealed))
	_, err = web.Authenticate(context.Background(), "", "cp_session="+id)
	assert.ErrorIs(t, err, ErrInvalidSession)
}

func TestWebSessionRefreshIsSingleFlight(t *testing.T) {
	web, h, _ := newWebFixture(t)
	state, _ := beginFlow(t, web, h, "login")
	id, _, err := web.Callback(context.Background(), "code", state, state)
	require.NoError(t, err)
	record, err := web.loadSession(context.Background(), id)
	require.NoError(t, err)
	now := time.Now()
	record.AccessToken = h.jwt(map[string]any{"iss": h.server.URL, "sub": "user-1", "aud": h.audience, "exp": now.Add(-time.Minute).Unix(), "iat": now.Add(-time.Hour).Unix()})
	record.RefreshToken = "refresh-1"
	require.NoError(t, web.storeEncrypted(context.Background(), sessionKey(id), record, time.Hour))
	h.refreshDelay = 120 * time.Millisecond
	start := make(chan struct{})
	errs := make(chan error, 2)
	for range 2 {
		go func() { <-start; _, err := web.Authenticate(context.Background(), "", "cp_session="+id); errs <- err }()
	}
	close(start)
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)
	assert.Equal(t, int32(1), h.refreshCalls.Load())
}

func TestWebServiceConfigurationAndHelpers(t *testing.T) {
	verifier, err := authnext.NewVerifier(authnext.Config{})
	require.NoError(t, err)
	web, err := NewWebService(context.Background(), authnext.Config{}, verifier, nil)
	require.NoError(t, err)
	assert.False(t, web.Enabled())
	_, err = web.Authenticate(context.Background(), "", "")
	assert.Error(t, err)
	_, err = NewWebService(context.Background(), authnext.Config{Web: authnext.WebConfig{Enabled: true}}, verifier, nil)
	assert.Error(t, err)
	_, err = NewWebService(context.Background(), authnext.Config{}, nil, nil)
	assert.Error(t, err)
	_, err = encryptionKey("short")
	assert.Error(t, err)
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	decoded, err := encryptionKey(key)
	require.NoError(t, err)
	assert.Len(t, decoded, 32)
	assert.NotEqual(t, mustValue(randomToken(16)), mustValue(randomToken(16)))
	_, err = cookieValue("", "missing")
	assert.Error(t, err)
	assert.Equal(t, sessionKey("id"), sessionKey("id"))
	assert.NotEqual(t, sessionKey("id"), sessionKey("other"))
}

func TestWebServiceConfigurationFailuresAndDefaults(t *testing.T) {
	h := newOIDCHarness(t)
	mr := miniredis.RunT(t)
	store := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = store.Close() })
	base := authnext.Config{Enabled: true, Issuer: h.server.URL, Audience: []string{h.audience}, JWKSURL: h.server.URL + "/jwks"}
	verifier, err := authnext.NewVerifier(base)
	require.NoError(t, err)
	base.Web = authnext.WebConfig{Enabled: true, ClientID: h.clientID, RedirectURL: "http://app/callback", PostLoginURL: "http://app/", AllowedReturnURLs: []string{"http://app/"}, EncryptionKey: "0123456789abcdef0123456789abcdef"}
	_, err = NewWebService(context.Background(), base, verifier, nil)
	assert.ErrorContains(t, err, "redis")
	missing := base
	missing.Web.ClientID = ""
	_, err = NewWebService(context.Background(), missing, verifier, store)
	assert.ErrorContains(t, err, "client_id")
	badReturn := base
	badReturn.Web.AllowedReturnURLs = []string{"http://other/"}
	_, err = NewWebService(context.Background(), badReturn, verifier, store)
	assert.ErrorContains(t, err, "allowed_return_urls")
	badKey := base
	badKey.Web.EncryptionKey = "short"
	_, err = NewWebService(context.Background(), badKey, verifier, store)
	assert.ErrorContains(t, err, "32 bytes")
	directWithoutToken := base
	directWithoutToken.Web.DirectLoginEnabled = true
	_, err = NewWebService(context.Background(), directWithoutToken, verifier, store)
	assert.ErrorContains(t, err, "login_client_token")
	web, err := NewWebService(context.Background(), base, verifier, store)
	require.NoError(t, err)
	assert.Equal(t, "cp_session", web.web.CookieName)
	assert.Equal(t, 8*time.Hour, web.web.SessionTTL)
	assert.Equal(t, 5*time.Minute, web.web.FlowTTL)
	web.web.Scopes = []string{"openid", "custom"}
	assert.Equal(t, []string{"openid", "custom"}, web.scopes())
}

func TestWebAuthenticateFailureBranches(t *testing.T) {
	web, h, _ := newWebFixture(t)
	now := time.Now()
	valid := h.jwt(map[string]any{"iss": h.server.URL, "sub": "bearer-user", "aud": h.audience, "exp": now.Add(time.Hour).Unix()})
	claims, err := web.Authenticate(context.Background(), "Bearer "+valid, "")
	require.NoError(t, err)
	assert.Equal(t, "bearer-user", claims.Subject)
	_, err = web.Authenticate(context.Background(), "", "")
	assert.ErrorIs(t, err, ErrInvalidSession)

	for name, record := range map[string]sessionRecord{
		"absolute expiry":    {AccessToken: valid, AbsoluteEnd: now.Add(-time.Second)},
		"invalid access":     {AccessToken: "invalid", AbsoluteEnd: now.Add(time.Hour)},
		"expired no refresh": {AccessToken: h.jwt(map[string]any{"iss": h.server.URL, "sub": "u", "aud": h.audience, "exp": now.Add(-time.Minute).Unix()}), AbsoluteEnd: now.Add(time.Hour)},
	} {
		t.Run(name, func(t *testing.T) {
			id := strings.ReplaceAll(name, " ", "-")
			require.NoError(t, web.storeEncrypted(context.Background(), sessionKey(id), record, time.Hour))
			_, err := web.Authenticate(context.Background(), "", "cp_session="+id)
			assert.Error(t, err)
		})
	}
	assert.NoError(t, web.ValidateCSRF(http.MethodPost, "", "", ""))
	_, _, err = web.Begin(context.Background(), "login", "")
	require.NoError(t, err)
	disabled := &WebService{}
	_, _, err = disabled.Begin(context.Background(), "login", "")
	assert.ErrorIs(t, err, authnext.ErrDisabled)
}

func TestWebCallbackAndExchangeFailures(t *testing.T) {
	web, h, mr := newWebFixture(t)
	state, _ := beginFlow(t, web, h, "login")
	sealed := []byte(mustValue(mr.Get(flowKey(state))))
	sealed[len(sealed)-1] ^= 1
	mr.Set(flowKey(state), string(sealed))
	_, _, err := web.Callback(context.Background(), "code", state, state)
	assert.ErrorIs(t, err, ErrInvalidFlow)

	web.discovery.TokenEndpoint = "://bad"
	_, err = web.exchange(context.Background(), url.Values{})
	assert.Error(t, err)
	web.discovery.TokenEndpoint = h.server.URL + "/token"
	h.tokenStatus = http.StatusBadGateway
	_, err = web.exchange(context.Background(), url.Values{"client_id": {h.clientID}})
	assert.ErrorContains(t, err, "status 502")
	h.tokenStatus = 0
	h.badTokenJSON = true
	_, err = web.exchange(context.Background(), url.Values{"client_id": {h.clientID}})
	assert.Error(t, err)
	h.badTokenJSON = false
	h.incompleteToken = true
	_, err = web.exchange(context.Background(), url.Values{"client_id": {h.clientID}})
	assert.ErrorContains(t, err, "incomplete")
}

func TestWebEncryptionAndDiscoveryFailures(t *testing.T) {
	web, _, _ := newWebFixture(t)
	assert.Error(t, web.storeEncrypted(context.Background(), "bad", make(chan int), time.Minute))
	assert.ErrorIs(t, web.open("x", []byte("short"), &sessionRecord{}), ErrInvalidSession)
	nonce := make([]byte, web.aead.NonceSize())
	sealed := web.aead.Seal(nonce, nonce, []byte("not-json"), []byte("x"))
	assert.Error(t, web.open("x", sealed, &sessionRecord{}))

	for name, handler := range map[string]http.HandlerFunc{
		"status":  func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "down", 503) },
		"json":    func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("{")) },
		"missing": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"authorization_endpoint":"x"}`)) },
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(handler)
			defer server.Close()
			_, err := discover(context.Background(), server.Client(), server.URL)
			assert.Error(t, err)
		})
	}
	_, err := discover(context.Background(), &http.Client{Timeout: 20 * time.Millisecond}, "http://127.0.0.1:1")
	assert.Error(t, err)
}

func TestWebRefreshFailureBranches(t *testing.T) {
	web, h, _ := newWebFixture(t)
	now := time.Now()
	expired := h.jwt(map[string]any{"iss": h.server.URL, "sub": "user-1", "aud": h.audience, "exp": now.Add(-time.Minute).Unix()})
	record := sessionRecord{AccessToken: expired, RefreshToken: "refresh", AbsoluteEnd: now.Add(time.Hour)}
	require.NoError(t, web.storeEncrypted(context.Background(), sessionKey("failed-refresh"), record, time.Hour))
	h.tokenStatus = http.StatusBadGateway
	err := web.refresh(context.Background(), "failed-refresh", &record)
	assert.Error(t, err)
	h.tokenStatus = 0
	record = sessionRecord{AccessToken: expired, RefreshToken: "refresh", AbsoluteEnd: time.Now().Add(-time.Second)}
	require.NoError(t, web.storeEncrypted(context.Background(), sessionKey("expired-refresh"), record, time.Hour))
	err = web.refresh(context.Background(), "expired-refresh", &record)
	assert.ErrorIs(t, err, ErrInvalidSession)

	lockID := "locked"
	require.NoError(t, web.redis.Set(context.Background(), sessionKey(lockID)+":refresh", "1", time.Minute).Err())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = web.refresh(ctx, lockID, &sessionRecord{})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestRefreshHolderReloadsSessionBeforeUsingRotatingToken(t *testing.T) {
	web, h, _ := newWebFixture(t)
	now := time.Now()
	valid := h.jwt(map[string]any{"iss": h.server.URL, "sub": "user-1", "aud": h.audience, "exp": now.Add(time.Hour).Unix()})
	current := sessionRecord{AccessToken: valid, RefreshToken: "current-refresh", AbsoluteEnd: now.Add(time.Hour)}
	require.NoError(t, web.storeEncrypted(context.Background(), sessionKey("race"), current, time.Hour))
	stale := sessionRecord{AccessToken: "stale", RefreshToken: "consumed-refresh", AbsoluteEnd: now.Add(time.Hour)}
	require.NoError(t, web.refresh(context.Background(), "race", &stale))
	assert.Equal(t, "current-refresh", stale.RefreshToken)
	assert.Equal(t, int32(0), h.refreshCalls.Load(), "a newly refreshed session must not consume the stale refresh token")
}

func mustValue(value string, err error) string {
	if err != nil {
		panic(err)
	}
	return value
}

func (h *oidcHarness) String() string {
	return fmt.Sprintf("oidc(%s)", strings.TrimPrefix(h.server.URL, "http://"))
}
