package federation

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testIDP is a real OIDC identity provider for tests: discovery, authorize,
// token, and JWKS endpoints backed by an Ed25519 signing key.
type testIDPCode struct {
	issuer    string
	challenge string
}

type testIDP struct {
	server        *httptest.Server
	mu            sync.Mutex
	key           ed25519.PrivateKey
	kid           string
	codes         map[string]testIDPCode
	issuer        string
	clientID      string
	secret        string
	extra         map[string]any
	badSig        bool
	ghostKid      bool
	discoveryHits int
}

func newTestIDP(t *testing.T) *testIDP {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	idp := &testIDP{key: key, kid: "test-key", codes: map[string]testIDPCode{}, clientID: "chaosplus-app"}
	idp.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, wellKnownPath):
			issuer := idp.server.URL + strings.TrimSuffix(r.URL.Path, wellKnownPath)
			idp.mu.Lock()
			idp.discoveryHits++
			idp.mu.Unlock()
			writeJSON(w, map[string]any{
				"issuer":                 issuer,
				"authorization_endpoint": issuer + "/authorize",
				"token_endpoint":         issuer + "/token",
				"jwks_uri":               issuer + "/jwks",
			})
		case strings.HasSuffix(r.URL.Path, "/authorize"):
			idp.handleAuthorize(w, r)
		case strings.HasSuffix(r.URL.Path, "/token"):
			idp.handleToken(t, w, r)
		case strings.HasSuffix(r.URL.Path, "/jwks"):
			idp.mu.Lock()
			key, kid, ghost := idp.key, idp.kid, idp.ghostKid
			idp.mu.Unlock()
			if ghost {
				_, ghostKey, _ := ed25519.GenerateKey(rand.Reader)
				key = ghostKey
				kid = "ghost-key"
			}
			writeJSON(w, map[string]any{"keys": []any{ed25519JWK(key.Public().(ed25519.PublicKey), kid)}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(idp.server.Close)
	idp.issuer = idp.server.URL
	return idp
}

func ed25519JWK(pub ed25519.PublicKey, kid string) map[string]any {
	return map[string]any{"kty": "OKP", "crv": "Ed25519", "kid": kid, "x": base64.RawURLEncoding.EncodeToString(pub), "alg": "EdDSA"}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func (p *testIDP) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	query := r.URL.Query()
	if query.Get("response_type") != "code" || query.Get("client_id") != p.clientID ||
		query.Get("redirect_uri") == "" || query.Get("state") == "" ||
		query.Get("code_challenge") == "" || query.Get("code_challenge_method") != "S256" {
		http.Error(w, "invalid authorize request", http.StatusBadRequest)
		return
	}
	code, err := randomToken(18)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	issuer := p.server.URL + strings.TrimSuffix(r.URL.Path, "/authorize")
	p.mu.Lock()
	p.codes[code] = testIDPCode{issuer: issuer, challenge: query.Get("code_challenge")}
	p.mu.Unlock()
	writeJSON(w, map[string]any{"code": code, "state": query.Get("state")})
}

func (p *testIDP) handleToken(t *testing.T, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	require.NoError(t, r.ParseForm())
	code, verifier := r.FormValue("code"), r.FormValue("code_verifier")
	p.mu.Lock()
	record, ok := p.codes[code]
	if ok {
		delete(p.codes, code)
	}
	secret := p.secret
	p.mu.Unlock()
	if !ok || codeChallenge(verifier) != record.challenge {
		writeJSON(w, map[string]any{"error": "invalid_grant"})
		return
	}
	if secret != "" {
		user, pass, ok := r.BasicAuth()
		if !ok || user != p.clientID || pass != secret {
			writeJSON(w, map[string]any{"error": "invalid_client"})
			return
		}
	}
	p.mu.Lock()
	badSig, ghost := p.badSig, p.ghostKid
	extra := make(map[string]any, len(p.extra))
	for key, value := range p.extra {
		extra[key] = value
	}
	key, kid := p.key, p.kid
	if ghost {
		_, ghostKey, _ := ed25519.GenerateKey(rand.Reader)
		key = ghostKey
	}
	p.mu.Unlock()
	if badSig {
		_, other, _ := ed25519.GenerateKey(rand.Reader)
		key = other
	}
	token := signTestJWT(t, key, kid, testIDTokenClaims(record.issuer, p.clientID, extra))
	writeJSON(w, map[string]any{"id_token": token})
}

func testIDTokenClaims(issuer, audience string, extra map[string]any) map[string]any {
	now := time.Now().UTC()
	claims := map[string]any{
		"iss":                issuer,
		"sub":                "external-user-1",
		"aud":                audience,
		"exp":                now.Add(time.Minute).Unix(),
		"iat":                now.Unix(),
		"nbf":                now.Add(-10 * time.Second).Unix(),
		"auth_time":          now.Add(-30 * time.Second).Unix(),
		"email":              "user@example.com",
		"email_verified":     true,
		"name":               "External User",
		"preferred_username": "external-user",
	}
	for key, value := range extra {
		claims[key] = value
	}
	return claims
}

func signTestJWT(t *testing.T, key ed25519.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]any{"alg": "EdDSA", "kid": kid, "typ": "JWT"})
	require.NoError(t, err)
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	enc := base64.RawURLEncoding
	signing := enc.EncodeToString(header) + "." + enc.EncodeToString(payload)
	return signing + "." + enc.EncodeToString(ed25519.Sign(key, []byte(signing)))
}

func (p *testIDP) setClaim(key string, value any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.extra == nil {
		p.extra = map[string]any{}
	}
	p.extra[key] = value
}

func (p *testIDP) rotateKey(t *testing.T) {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.key = key
	p.kid = "rotated-key"
}

func (p *testIDP) authorize(t *testing.T, client *http.Client, issuer, verifier, state string) (code, returnedState string) {
	t.Helper()
	u := issuer + "/authorize?response_type=code&client_id=" + p.clientID +
		"&redirect_uri=" + issuer + "/callback&state=" + url.QueryEscape(state) + "&scope=openid&code_challenge=" +
		codeChallenge(verifier) + "&code_challenge_method=S256"
	response, err := client.Get(u)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	var body struct {
		Code  string `json:"code"`
		State string `json:"state"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&body))
	assert.Equal(t, state, body.State)
	return body.Code, body.State
}

func newTestOIDCClient(idp *testIDP) *oidcClient {
	return newOIDCClient(idp.server.Client(), 30*time.Second, 5*time.Minute)
}

func TestOIDCDiscoveryCachesAndValidates(t *testing.T) {
	idp := newTestIDP(t)
	client := newTestOIDCClient(idp)
	ctx := context.Background()

	first, err := client.discovery(ctx, idp.issuer)
	require.NoError(t, err)
	assert.Equal(t, idp.issuer+"/authorize", first.AuthorizationEndpoint)
	second, err := client.discovery(ctx, idp.issuer+"/")
	require.NoError(t, err)
	assert.Equal(t, first, second)
	idp.mu.Lock()
	hits := idp.discoveryHits
	idp.mu.Unlock()
	assert.Equal(t, 1, hits)

	missing := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(missing.Close)
	_, err = client.discovery(ctx, missing.URL)
	assert.ErrorIs(t, err, ErrOIDCDiscovery)

	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"authorization_endpoint": "relative/path"})
	}))
	t.Cleanup(broken.Close)
	_, err = client.discovery(ctx, broken.URL)
	assert.ErrorIs(t, err, ErrOIDCDiscovery)
}

func TestOIDCCodeExchangeFlow(t *testing.T) {
	idp := newTestIDP(t)
	idp.secret = "client-secret"
	client := newTestOIDCClient(idp)
	ctx := context.Background()
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"

	code, returned := idp.authorize(t, idp.server.Client(), idp.issuer, verifier, "flow-state")
	require.Equal(t, "flow-state", returned)
	raw, err := client.exchangeCode(ctx, idp.issuer, idp.clientID, idp.secret, code, idp.issuer+"/callback", verifier)
	require.NoError(t, err)
	assert.Contains(t, raw, ".")

	// Codes are one-time: a second exchange must be rejected by the IdP.
	_, err = client.exchangeCode(ctx, idp.issuer, idp.clientID, idp.secret, code, idp.issuer+"/callback", verifier)
	assert.ErrorIs(t, err, ErrOIDCToken)

	// Wrong PKCE verifier fails at the IdP.
	code, _ = idp.authorize(t, idp.server.Client(), idp.issuer, verifier, "flow-state")
	_, err = client.exchangeCode(ctx, idp.issuer, idp.clientID, idp.secret, code, idp.issuer+"/callback", verifier+"x")
	assert.ErrorIs(t, err, ErrOIDCToken)

	// Wrong client secret fails at the IdP.
	code, _ = idp.authorize(t, idp.server.Client(), idp.issuer, verifier, "flow-state")
	_, err = client.exchangeCode(ctx, idp.issuer, idp.clientID, "wrong", code, idp.issuer+"/callback", verifier)
	assert.ErrorIs(t, err, ErrOIDCToken)
}

func verifyIDTokenViaFlow(t *testing.T, idp *testIDP, client *oidcClient) (idTokenClaims, error) {
	t.Helper()
	ctx := context.Background()
	verifier := "verifier-12345678901234567890123456789012"
	code, returned := idp.authorize(t, idp.server.Client(), idp.issuer, verifier, "flow-state")
	require.Equal(t, "flow-state", returned)
	raw, err := client.exchangeCode(ctx, idp.issuer, idp.clientID, "", code, idp.issuer+"/callback", verifier)
	require.NoError(t, err)
	return client.verifyIDToken(ctx, idp.issuer, idp.clientID, raw, time.Now().UTC())
}

func TestOIDCIDTokenVerification(t *testing.T) {
	idp := newTestIDP(t)
	client := newTestOIDCClient(idp)

	claims, err := verifyIDTokenViaFlow(t, idp, client)
	require.NoError(t, err)
	assert.Equal(t, idp.issuer, claims.Issuer)
	assert.Equal(t, "external-user-1", claims.Subject)
	assert.Equal(t, idp.clientID, claims.Audience[0])
	assert.Equal(t, "user@example.com", claims.Email)
	assert.NotNil(t, claims.EmailVerified)
	assert.True(t, *claims.EmailVerified)

	t.Run("bad signature", func(t *testing.T) {
		sub := newTestIDP(t)
		sub.badSig = true
		_, err := verifyIDTokenViaFlow(t, sub, newTestOIDCClient(sub))
		assert.ErrorIs(t, err, ErrOIDCTokenInvalid)
	})
	t.Run("wrong issuer", func(t *testing.T) {
		sub := newTestIDP(t)
		sub.setClaim("iss", "https://other.example")
		_, err := verifyIDTokenViaFlow(t, sub, newTestOIDCClient(sub))
		assert.ErrorIs(t, err, ErrOIDCTokenInvalid)
	})
	t.Run("wrong audience", func(t *testing.T) {
		sub := newTestIDP(t)
		sub.setClaim("aud", "some-other-app")
		_, err := verifyIDTokenViaFlow(t, sub, newTestOIDCClient(sub))
		assert.ErrorIs(t, err, ErrOIDCTokenInvalid)
	})
	t.Run("expired", func(t *testing.T) {
		sub := newTestIDP(t)
		sub.setClaim("exp", time.Now().Add(-time.Minute).Unix())
		_, err := verifyIDTokenViaFlow(t, sub, newTestOIDCClient(sub))
		assert.ErrorIs(t, err, ErrOIDCTokenInvalid)
	})
	t.Run("not yet valid", func(t *testing.T) {
		sub := newTestIDP(t)
		sub.setClaim("nbf", time.Now().Add(2*time.Minute).Unix())
		_, err := verifyIDTokenViaFlow(t, sub, newTestOIDCClient(sub))
		assert.ErrorIs(t, err, ErrOIDCTokenInvalid)
	})
	t.Run("malformed token", func(t *testing.T) {
		_, err := client.verifyIDToken(context.Background(), idp.issuer, idp.clientID, "not.a.jwt", time.Now().UTC())
		assert.ErrorIs(t, err, ErrOIDCTokenInvalid)
	})
	t.Run("audience as array", func(t *testing.T) {
		sub := newTestIDP(t)
		sub.setClaim("aud", []string{"first", sub.clientID})
		claims, err := verifyIDTokenViaFlow(t, sub, newTestOIDCClient(sub))
		require.NoError(t, err)
		assert.Contains(t, claims.Audience, sub.clientID)
	})
}

func TestOIDCKeyRotationRetries(t *testing.T) {
	idp := newTestIDP(t)
	client := newTestOIDCClient(idp)
	ctx := context.Background()

	first, err := client.verifyIDToken(ctx, idp.issuer, idp.clientID, signedTokenFor(t, idp, "external-user-1", nil), time.Now().UTC())
	require.NoError(t, err)
	assert.Equal(t, "external-user-1", first.Subject)

	// The client cached the first JWKS. After the IdP rotates keys, the next
	// verification must discover the unknown kid, refresh JWKS, and succeed.
	idp.rotateKey(t)
	second, err := client.verifyIDToken(ctx, idp.issuer, idp.clientID, signedTokenFor(t, idp, "external-user-2", nil), time.Now().UTC())
	require.NoError(t, err)
	assert.Equal(t, "external-user-2", second.Subject)

	// A token signed by an unknown kid that the refreshed JWKS still cannot
	// verify must fail.
	_, ghostKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	idp.ghostKid = true
	ghost := signTestJWT(t, ghostKey, "ghost-key", testIDTokenClaims(idp.issuer, idp.clientID, nil))
	_, err = client.verifyIDToken(ctx, idp.issuer, idp.clientID, ghost, time.Now().UTC())
	assert.ErrorIs(t, err, ErrOIDCTokenInvalid)
}

func signedTokenFor(t *testing.T, idp *testIDP, subject string, extra map[string]any) string {
	t.Helper()
	idp.mu.Lock()
	key, kid := idp.key, idp.kid
	claims := testIDTokenClaims(idp.issuer, idp.clientID, extra)
	idp.mu.Unlock()
	claims["sub"] = subject
	return signTestJWT(t, key, kid, claims)
}

func TestJWKPublicKeyParsing(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	rsaJWK := jwk{Kty: "RSA", Kid: "rsa-1", Alg: "RS256",
		N: base64.RawURLEncoding.EncodeToString(rsaKey.N.Bytes()),
		E: base64.RawURLEncoding.EncodeToString(big.NewInt(int64(rsaKey.E)).Bytes())}
	parsed, err := rsaJWK.publicKey()
	require.NoError(t, err)
	assert.Equal(t, &rsaKey.PublicKey, parsed)
	rsaJWK.N = "!!not-base64!!"
	_, err = rsaJWK.publicKey()
	assert.Error(t, err)

	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	ecJWK := jwk{Kty: "EC", Kid: "ec-1", Crv: "P-256",
		X: base64.RawURLEncoding.EncodeToString(ecKey.X.Bytes()),
		Y: base64.RawURLEncoding.EncodeToString(ecKey.Y.Bytes())}
	parsed, err = ecJWK.publicKey()
	require.NoError(t, err)
	assert.Equal(t, &ecKey.PublicKey, parsed)
	_, err = (jwk{Kty: "EC", Kid: "ec-2", Crv: "P-999"}).publicKey()
	assert.Error(t, err)

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	edJWK := jwk{Kty: "OKP", Kid: "ed-1", Crv: "Ed25519", X: base64.RawURLEncoding.EncodeToString(pub)}
	parsed, err = edJWK.publicKey()
	require.NoError(t, err)
	assert.Equal(t, ed25519.PublicKey(pub), parsed)
	_, err = (jwk{Kty: "OKP", Kid: "ed-2", Crv: "X25519", X: base64.RawURLEncoding.EncodeToString(pub)}).publicKey()
	assert.Error(t, err)
	_, err = (jwk{Kty: "OKP", Kid: "ed-3", Crv: "Ed25519", X: base64.RawURLEncoding.EncodeToString([]byte("short"))}).publicKey()
	assert.Error(t, err)

	_, err = (jwk{Kty: "RSA"}).publicKey()
	assert.Error(t, err)
	_, err = (jwk{Kty: "unknown", Kid: "x"}).publicKey()
	assert.Error(t, err)
}

func TestVerifySignatureAlgorithms(t *testing.T) {
	message := []byte("header.payload")
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	sha := sha256.Sum256(message)
	rsaSig, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, sha[:])
	require.NoError(t, err)
	require.NoError(t, verifySignature("RS256", &rsaKey.PublicKey, message, rsaSig))
	assert.ErrorIs(t, verifySignature("RS256", &rsaKey.PublicKey, message, append([]byte{}, rsaSig...)[:len(rsaSig)-1]), ErrOIDCTokenInvalid)
	assert.ErrorIs(t, verifySignature("RS512", &rsaKey.PublicKey, message, rsaSig), ErrOIDCTokenInvalid)

	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	ecSig := ecdsaSignature(t, ecKey, sha[:])
	require.NoError(t, verifySignature("ES256", &ecKey.PublicKey, message, ecSig))
	assert.ErrorIs(t, verifySignature("ES256", &ecKey.PublicKey, message, ecSig[:10]), ErrOIDCTokenInvalid)
	assert.ErrorIs(t, verifySignature("ES256", &rsaKey.PublicKey, message, ecSig), ErrOIDCTokenInvalid)

	ec521, err := ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	require.NoError(t, err)
	sha512 := sha512.Sum512(message)
	ec512Sig := ecdsaSignature(t, ec521, sha512[:])
	require.NoError(t, verifySignature("ES512", &ec521.PublicKey, message, ec512Sig))

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	edSig := ed25519.Sign(priv, message)
	require.NoError(t, verifySignature("EdDSA", pub, message, edSig))
	bad := append([]byte{}, edSig...)
	bad[0] ^= 0xff
	assert.ErrorIs(t, verifySignature("EdDSA", pub, message, bad), ErrOIDCTokenInvalid)
	assert.ErrorIs(t, verifySignature("EdDSA", &rsaKey.PublicKey, message, edSig), ErrOIDCTokenInvalid)
	assert.ErrorIs(t, verifySignature("HS256", pub, message, edSig), ErrOIDCTokenInvalid)
}

func ecdsaSignature(t *testing.T, key *ecdsa.PrivateKey, digest []byte) []byte {
	t.Helper()
	r, s, err := ecdsa.Sign(rand.Reader, key, digest)
	require.NoError(t, err)
	size := (key.Curve.Params().BitSize + 7) / 8
	sig := make([]byte, 0, 2*size)
	sig = append(sig, r.FillBytes(make([]byte, size))...)
	sig = append(sig, s.FillBytes(make([]byte, size))...)
	return sig
}

func TestSplitJWTAndDecode(t *testing.T) {
	_, _, _, err := splitJWT("only.two")
	assert.Error(t, err)
	_, _, _, err = splitJWT("a.b.!!not-base64!!")
	assert.Error(t, err)
	header, payload, signature, err := splitJWT("eyJhbGciOiJFZERTQSJ9.eyJzdWIiOiJ4In0.c2ln")
	require.NoError(t, err)
	assert.Equal(t, "eyJhbGciOiJFZERTQSJ9", header)
	assert.Equal(t, "eyJzdWIiOiJ4In0", payload)
	assert.Equal(t, []byte("sig"), signature)

	var claims map[string]any
	require.NoError(t, decodeJSONPart(payload, &claims))
	assert.Equal(t, "x", claims["sub"])
	assert.Error(t, decodeJSONPart("!!bad!!", &claims))
}

func TestCurveForAndDecodeCoordinate(t *testing.T) {
	for name, want := range map[string]string{"P-256": "P-256", "P-384": "P-384", "P-521": "P-521"} {
		curve, err := curveFor(name)
		require.NoError(t, err)
		assert.Equal(t, want, curve.Params().Name)
	}
	_, err := curveFor("P-999")
	assert.Error(t, err)

	value, err := decodeCoordinate(base64.RawURLEncoding.EncodeToString([]byte{1, 2, 3}))
	require.NoError(t, err)
	assert.Equal(t, big.NewInt(66051), value)
	_, err = decodeCoordinate("!!bad!!")
	assert.Error(t, err)
}

func TestJWKPublicKeyRejectsMalformedKeys(t *testing.T) {
	goodX := base64.RawURLEncoding.EncodeToString([]byte{1, 2, 3})
	_, err := (jwk{Kty: "RSA", Kid: "k", N: "!!bad!!"}).publicKey()
	assert.Error(t, err)
	_, err = (jwk{Kty: "RSA", Kid: "k", N: base64.RawURLEncoding.EncodeToString([]byte{1}), E: "!!bad!!"}).publicKey()
	assert.Error(t, err)
	_, err = (jwk{Kty: "EC", Kid: "k", Crv: "P-999", X: goodX, Y: goodX}).publicKey()
	assert.Error(t, err)
	_, err = (jwk{Kty: "EC", Kid: "k", Crv: "P-256", X: "!!bad!!", Y: goodX}).publicKey()
	assert.Error(t, err)
	_, err = (jwk{Kty: "OKP", Kid: "k", Crv: "X25519", X: goodX}).publicKey()
	assert.Error(t, err)
	_, err = (jwk{Kty: "OKP", Kid: "k", Crv: "Ed25519", X: base64.RawURLEncoding.EncodeToString([]byte{1, 2})}).publicKey()
	assert.Error(t, err)
	_, err = (jwk{Kty: "oct", Kid: "k"}).publicKey()
	assert.Error(t, err)
	_, err = (jwk{Kty: "RSA", N: base64.RawURLEncoding.EncodeToString([]byte{1}), E: "AQAB"}).publicKey()
	assert.Error(t, err)
}

func TestAudClaimUnmarshalRejectsInvalidJSON(t *testing.T) {
	var audience audClaim
	assert.Error(t, json.Unmarshal([]byte(`123`), &audience))
}

func TestOIDCDiscoveryServerErrors(t *testing.T) {
	internalError := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(internalError.Close)
	badJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{"))
	}))
	t.Cleanup(badJSON.Close)

	client := newOIDCClient(http.DefaultClient, 30*time.Second, 5*time.Minute)
	_, err := client.discovery(context.Background(), internalError.URL)
	assert.ErrorIs(t, err, ErrOIDCDiscovery)
	_, err = client.discovery(context.Background(), badJSON.URL)
	assert.ErrorIs(t, err, ErrOIDCDiscovery)
}

func newJwksServer(t *testing.T, jwksHandler func(http.ResponseWriter)) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, wellKnownPath) {
			writeJSON(w, map[string]any{
				"issuer":                 server.URL,
				"authorization_endpoint": server.URL + "/authorize",
				"token_endpoint":         server.URL + "/token",
				"jwks_uri":               server.URL + "/jwks",
			})
			return
		}
		jwksHandler(w)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestOIDCKeysServerErrors(t *testing.T) {
	client := newOIDCClient(http.DefaultClient, 30*time.Second, 5*time.Minute)

	status500 := newJwksServer(t, func(w http.ResponseWriter) { http.Error(w, "boom", http.StatusInternalServerError) })
	_, err := client.keys(context.Background(), status500.URL, false)
	assert.ErrorIs(t, err, ErrOIDCTokenInvalid)

	badJSON := newJwksServer(t, func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{"))
	})
	_, err = client.keys(context.Background(), badJSON.URL, false)
	assert.ErrorIs(t, err, ErrOIDCTokenInvalid)

	missingKid := newJwksServer(t, func(w http.ResponseWriter) {
		writeJSON(w, map[string]any{"keys": []any{map[string]any{"kty": "OKP", "crv": "Ed25519", "x": "x"}}})
	})
	_, err = client.keys(context.Background(), missingKid.URL, false)
	assert.ErrorIs(t, err, ErrOIDCTokenInvalid)
}

func TestOIDCExchangeCodeServerError(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, wellKnownPath) {
			writeJSON(w, map[string]any{
				"issuer":                 server.URL,
				"authorization_endpoint": server.URL + "/authorize",
				"token_endpoint":         server.URL + "/token",
				"jwks_uri":               server.URL + "/jwks",
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{"))
	}))
	t.Cleanup(server.Close)
	client := newOIDCClient(http.DefaultClient, 30*time.Second, 5*time.Minute)
	_, err := client.exchangeCode(context.Background(), server.URL, "client", "", "code", server.URL+"/callback", "verifier")
	assert.ErrorIs(t, err, ErrOIDCToken)
}
