package authn

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyAuthorization(t *testing.T) {
	key := newTestKey(t)
	issuer := "https://issuer.example"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]string{"jwks_uri": issuerURL(r) + "/keys"})
		case "/keys":
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{key.jwk()}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	issuer = srv.URL
	verifier, err := NewVerifier(Config{Enabled: true, Issuer: issuer, Audience: []string{"api"}})
	require.NoError(t, err)

	token := key.sign(t, map[string]any{
		"iss":                issuer,
		"sub":                "u123",
		"aud":                []string{"api"},
		"exp":                time.Now().Add(time.Hour).Unix(),
		"preferred_username": "alice",
		"email":              "alice@example.test",
	})

	claims, err := verifier.VerifyAuthorization(context.Background(), "Bearer "+token)
	require.NoError(t, err)
	assert.Equal(t, "u123", claims.Subject)
	assert.Equal(t, "user:u123", claims.SubjectRef().String())
	assert.Equal(t, "alice", claims.PreferredUsername)
	authenticated, err := verifier.Authenticate(context.Background(), "Bearer "+token, "")
	require.NoError(t, err)
	assert.Equal(t, claims.Subject, authenticated.Subject)
}

func TestVerifyAuthorizationRejectsInvalidInputs(t *testing.T) {
	verifier, err := NewVerifier(Config{Enabled: true, Issuer: "https://issuer.example"})
	require.NoError(t, err)

	_, err = verifier.VerifyAuthorization(context.Background(), "")
	assert.ErrorIs(t, err, ErrMissingBearer)
	_, err = verifier.VerifyAuthorization(context.Background(), "Basic abc")
	assert.ErrorIs(t, err, ErrInvalidBearer)
	_, err = verifier.Verify(context.Background(), "not-a-jwt")
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestNewVerifierValidation(t *testing.T) {
	disabled, err := NewVerifier(Config{})
	require.NoError(t, err)
	_, err = disabled.VerifyAuthorization(context.Background(), "Bearer abc")
	assert.ErrorIs(t, err, ErrDisabled)

	_, err = NewVerifier(Config{Enabled: true})
	assert.Error(t, err)
}

func TestVerifyRejectsBadSignatureAndUnknownKey(t *testing.T) {
	key := newTestKey(t)
	other := newTestKey(t)
	other.kid = "kid2"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{key.jwk()}})
	}))
	defer srv.Close()

	verifier, err := NewVerifier(Config{Enabled: true, Issuer: "https://issuer.example", JWKSURL: srv.URL})
	require.NoError(t, err)

	unknownKid := other.sign(t, map[string]any{"iss": "https://issuer.example", "sub": "u1", "exp": time.Now().Add(time.Hour).Unix()})
	_, err = verifier.Verify(context.Background(), unknownKid)
	assert.ErrorIs(t, err, ErrUnknownKey)

	token := key.sign(t, map[string]any{"iss": "https://issuer.example", "sub": "u1", "exp": time.Now().Add(time.Hour).Unix()})
	_, err = verifier.Verify(context.Background(), token[:len(token)-2]+"xx")
	assert.Error(t, err)
}

func TestParseClaims(t *testing.T) {
	claims, err := parseClaims(map[string]any{
		"iss": "issuer",
		"sub": "subject",
		"aud": "api",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"api"}, claims.Audience)

	_, err = parseClaims(map[string]any{"iss": "issuer"})
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestValidateClaims(t *testing.T) {
	verifier, err := NewVerifier(Config{
		Enabled:  true,
		Issuer:   "https://issuer.example",
		Audience: []string{"api"},
	})
	require.NoError(t, err)

	valid := &Claims{Issuer: "https://issuer.example", Subject: "u1", Audience: []string{"api"}, ExpiresAt: time.Now().Add(time.Hour)}
	assert.NoError(t, verifier.validate(valid, time.Now()))

	assert.ErrorIs(t, verifier.validate(&Claims{Issuer: "bad", Subject: "u1", Audience: []string{"api"}, ExpiresAt: time.Now().Add(time.Hour)}, time.Now()), ErrInvalidIssuer)
	assert.ErrorIs(t, verifier.validate(&Claims{Issuer: "https://issuer.example", Subject: "u1", Audience: []string{"other"}, ExpiresAt: time.Now().Add(time.Hour)}, time.Now()), ErrInvalidAud)
	assert.ErrorIs(t, verifier.validate(&Claims{Issuer: "https://issuer.example", Subject: "u1", Audience: []string{"api"}, ExpiresAt: time.Now().Add(-time.Hour)}, time.Now()), ErrExpiredToken)
	assert.ErrorIs(t, verifier.validate(&Claims{Issuer: "https://issuer.example", Audience: []string{"api"}, ExpiresAt: time.Now().Add(time.Hour)}, time.Now()), ErrInvalidToken)
	assert.ErrorIs(t, verifier.validate(&Claims{Issuer: "https://issuer.example", Subject: "u1", Audience: []string{"api"}, ExpiresAt: time.Now().Add(time.Hour), NotBefore: time.Now().Add(time.Hour)}, time.Now()), ErrInvalidToken)
}

func TestContextHelpers(t *testing.T) {
	_, ok := SubjectFromContext(context.Background())
	assert.False(t, ok)

	ctx := WithClaims(context.Background(), &Claims{Subject: "u1"})
	claims, ok := FromContext(ctx)
	require.True(t, ok)
	assert.Equal(t, "u1", claims.Subject)
	subject, ok := SubjectFromContext(ctx)
	require.True(t, ok)
	assert.Equal(t, "user:u1", subject.String())
}

func TestVerifierErrorBranches(t *testing.T) {
	_, err := (&jwk{Kid: "bad", Kty: "RSA", N: "!", E: "AQAB"}).publicKey()
	assert.Error(t, err)
	_, err = (&jwk{Kid: "bad", Kty: "RSA", N: "AQAB", E: "!"}).publicKey()
	assert.Error(t, err)
	_, err = (&jwk{Kid: "bad", Kty: "oct"}).publicKey()
	assert.Error(t, err)
	_, err = (&jwk{Kty: "RSA"}).publicKey()
	assert.Error(t, err)
	assert.Error(t, verifySignature("RS256", "not-rsa", []byte("a.b"), []byte("sig")))
	assert.Error(t, verifySignature("HS256", &rsa.PublicKey{}, []byte("a.b"), []byte("sig")))

	var n json.Number = "123"
	assert.False(t, unixClaim(n).IsZero())
	assert.True(t, unixClaim("bad").IsZero())

	var dst map[string]any
	assert.Error(t, decodeJSON("!", &dst))
}

func TestRefreshAndDiscoveryErrors(t *testing.T) {
	statusSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer statusSrv.Close()
	verifier, err := NewVerifier(Config{Enabled: true, Issuer: statusSrv.URL})
	require.NoError(t, err)
	assert.Error(t, verifier.refreshKeys(context.Background()))

	badJWKS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"keys":[{"kid":"x","kty":"oct"}]}`))
	}))
	defer badJWKS.Close()
	verifier, err = NewVerifier(Config{Enabled: true, Issuer: "https://issuer.example", JWKSURL: badJWKS.URL})
	require.NoError(t, err)
	assert.Error(t, verifier.refreshKeys(context.Background()))

	noKey := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer noKey.Close()
	verifier, err = NewVerifier(Config{Enabled: true, Issuer: noKey.URL})
	require.NoError(t, err)
	_, err = verifier.jwksURLValue(context.Background())
	assert.Error(t, err)

	verifier.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network")
	})
	_, err = verifier.jwksURLValue(context.Background())
	assert.Error(t, err)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type testKey struct {
	private *rsa.PrivateKey
	kid     string
}

func newTestKey(t *testing.T) testKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return testKey{private: key, kid: "kid1"}
}

func (k testKey) jwk() map[string]string {
	e := bigEndian(k.private.PublicKey.E)
	return map[string]string{
		"kty": "RSA",
		"kid": k.kid,
		"alg": "RS256",
		"n":   base64.RawURLEncoding.EncodeToString(k.private.PublicKey.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(e),
	}
}

func (k testKey) sign(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := map[string]any{"alg": "RS256", "kid": k.kid, "typ": "JWT"}
	head := mustJSONPart(t, header)
	body := mustJSONPart(t, claims)
	signed := head + "." + body
	hash := sha256.Sum256([]byte(signed))
	sig, err := rsa.SignPKCS1v15(rand.Reader, k.private, crypto.SHA256, hash[:])
	require.NoError(t, err)
	return signed + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func mustJSONPart(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return base64.RawURLEncoding.EncodeToString(data)
}

func bigEndian(n int) []byte {
	if n == 0 {
		return []byte{0}
	}
	var out []byte
	for n > 0 {
		out = append([]byte{byte(n)}, out...)
		n >>= 8
	}
	return out
}

func issuerURL(r *http.Request) string {
	return "http://" + r.Host
}
