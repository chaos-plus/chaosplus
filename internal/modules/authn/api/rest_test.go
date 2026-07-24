package api

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
)

type fakeWeb struct {
	enabled   bool
	err       error
	flowErr   error
	csrfErr   error
	loggedOut bool
}

func (f *fakeWeb) Enabled() bool            { return f.enabled }
func (f *fakeWeb) DirectLoginEnabled() bool { return f.enabled }
func (f *fakeWeb) Begin(context.Context, string, string) (string, string, error) {
	return "https://issuer/authorize", "state", f.err
}
func (f *fakeWeb) Callback(context.Context, string, string, string) (string, string, error) {
	return "session", "http://app/", f.err
}
func (f *fakeWeb) Login(context.Context, string, string, string) (string, string, error) {
	return "session", "http://app/", f.err
}
func (f *fakeWeb) Authenticate(context.Context, string, string) (*authnext.Claims, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &authnext.Claims{Issuer: "issuer", Subject: "u1", PreferredUsername: "alice", Email: "alice@example.com"}, nil
}
func (f *fakeWeb) ValidateCSRF(string, string, string, string) error { return f.csrfErr }
func (f *fakeWeb) ValidateLoginOrigin(string) error                  { return f.csrfErr }
func (f *fakeWeb) Logout(context.Context, string) string {
	f.loggedOut = true
	return "https://issuer/end_session?id_token_hint=idt"
}
func (f *fakeWeb) SessionCookie(value string) string { return "cp_session=" + value }
func (f *fakeWeb) FlowCookie(value string) string    { return "cp_session_oidc=" + value }
func (f *fakeWeb) ClearCookie() string               { return "cp_session=; Max-Age=0" }
func (f *fakeWeb) FlowState(string) (string, error)  { return "state", f.flowErr }
func (f *fakeWeb) PostLogoutURL() string             { return "http://app/login" }

func TestRegisterRESTMe(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{jwk(key)}})
	}))
	defer srv.Close()

	verifier, err := authnext.NewVerifier(authnext.Config{Enabled: true, Issuer: "https://issuer.example", JWKSURL: srv.URL})
	require.NoError(t, err)
	_, api := humatest.New(t)
	RegisterREST(api, verifier, nil)

	resp := api.Get("/authn/me", "Authorization: Bearer "+sign(t, key, map[string]any{
		"iss":                "https://issuer.example",
		"sub":                "u1",
		"exp":                time.Now().Add(time.Hour).Unix(),
		"preferred_username": "alice",
	}))
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), `"spicedb_subject":"user:u1"`)

	assert.Equal(t, http.StatusUnauthorized, api.Get("/authn/me").Code)
}

func TestRegisterRESTWebFlow(t *testing.T) {
	web := &fakeWeb{enabled: true}
	_, api := humatest.New(t)
	RegisterREST(api, web, web)
	start := api.Get("/authn/oidc/start?mode=login&return_url=http%3A%2F%2Fapp%2F")
	assert.Equal(t, http.StatusFound, start.Code, start.Body.String())
	assert.Equal(t, "https://issuer/authorize", start.Header().Get("Location"))
	assert.Contains(t, start.Header().Get("Set-Cookie"), "cp_session_oidc")
	callback := api.Get("/authn/oidc/callback?code=code&state=state", "Cookie: cp_session_oidc=state")
	assert.Equal(t, http.StatusFound, callback.Code, callback.Body.String())
	assert.Equal(t, "http://app/", callback.Header().Get("Location"))
	assert.Equal(t, http.StatusOK, api.Get("/authn/session", "Cookie: cp_session=session").Code)
	login := api.Post("/authn/login", map[string]any{"login_name": "alice", "password": "secret"}, "Origin: http://app")
	assert.Equal(t, http.StatusOK, login.Code, login.Body.String())
	assert.Contains(t, login.Header().Get("Set-Cookie"), "cp_session=session")
	logout := api.Post("/authn/logout", "Cookie: cp_session=session", "Origin: http://app")
	assert.Equal(t, http.StatusOK, logout.Code, logout.Body.String())
	assert.Contains(t, logout.Body.String(), `"logout_url":"https://issuer/end_session?id_token_hint=idt"`)
	assert.Contains(t, logout.Header().Get("Set-Cookie"), "Max-Age=0")
	assert.True(t, web.loggedOut)
}

func TestRegisterRESTWebFlowErrors(t *testing.T) {
	t.Run("begin", func(t *testing.T) {
		web := &fakeWeb{enabled: true, err: assert.AnError}
		_, api := humatest.New(t)
		RegisterREST(api, web, web)
		assert.Equal(t, http.StatusUnprocessableEntity, api.Get("/authn/oidc/start").Code)
	})
	t.Run("provider callback error", func(t *testing.T) {
		web := &fakeWeb{enabled: true}
		_, api := humatest.New(t)
		RegisterREST(api, web, web)
		assert.Equal(t, http.StatusUnauthorized, api.Get("/authn/oidc/callback?error=access_denied").Code)
	})
	t.Run("missing flow", func(t *testing.T) {
		web := &fakeWeb{enabled: true, flowErr: assert.AnError}
		_, api := humatest.New(t)
		RegisterREST(api, web, web)
		assert.Equal(t, http.StatusUnauthorized, api.Get("/authn/oidc/callback?code=x&state=x").Code)
	})
	t.Run("callback", func(t *testing.T) {
		web := &fakeWeb{enabled: true, err: assert.AnError}
		_, api := humatest.New(t)
		RegisterREST(api, web, web)
		assert.Equal(t, http.StatusUnauthorized, api.Get("/authn/oidc/callback?code=x&state=x").Code)
		assert.Equal(t, http.StatusUnauthorized, api.Get("/authn/session").Code)
	})
	t.Run("csrf", func(t *testing.T) {
		web := &fakeWeb{enabled: true, csrfErr: assert.AnError}
		_, api := humatest.New(t)
		RegisterREST(api, web, web)
		assert.Equal(t, http.StatusForbidden, api.Post("/authn/logout").Code)
		assert.Equal(t, http.StatusForbidden, api.Post("/authn/login", map[string]any{"login_name": "alice", "password": "secret"}).Code)
	})
	t.Run("additional verification", func(t *testing.T) {
		web := &fakeWeb{enabled: true, err: authnext.ErrAdditionalVerification}
		_, api := humatest.New(t)
		RegisterREST(api, web, web)
		assert.Equal(t, http.StatusConflict, api.Post("/authn/login", map[string]any{"login_name": "alice", "password": "secret"}, "Origin: http://app").Code)
	})
}

func jwk(key *rsa.PrivateKey) map[string]string {
	return map[string]string{
		"kty": "RSA",
		"kid": "kid1",
		"alg": "RS256",
		"n":   base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString([]byte{1, 0, 1}),
	}
}

func sign(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	header := part(t, map[string]any{"alg": "RS256", "kid": "kid1", "typ": "JWT"})
	body := part(t, claims)
	signed := header + "." + body
	hash := sha256.Sum256([]byte(signed))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	require.NoError(t, err)
	return signed + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func part(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return base64.RawURLEncoding.EncodeToString(data)
}
