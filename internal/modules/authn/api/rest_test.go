package api

import (
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
	RegisterREST(api, verifier)

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
