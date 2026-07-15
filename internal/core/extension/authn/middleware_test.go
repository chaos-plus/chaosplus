package authn

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMiddlewareRejectsWhenDisabled(t *testing.T) {
	verifier, err := NewVerifier(Config{})
	require.NoError(t, err)
	called := false
	handler := Middleware(verifier)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.False(t, called)
}

func TestMiddlewareSuccess(t *testing.T) {
	key := newTestKey(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{key.jwk()}})
	}))
	defer srv.Close()

	verifier, err := NewVerifier(Config{Enabled: true, Issuer: "https://issuer.example", JWKSURL: srv.URL})
	require.NoError(t, err)
	token := key.sign(t, map[string]any{
		"iss": "https://issuer.example",
		"sub": "u1",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	handler := Middleware(verifier)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := FromContext(r.Context())
		require.True(t, ok)
		assert.Equal(t, "u1", claims.Subject)
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(context.Background())
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}
