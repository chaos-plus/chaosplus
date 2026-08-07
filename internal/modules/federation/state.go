package federation

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const stateCookieName = "cp_federation_state"

var (
	errUnsupportedState = errors.New("unsupported federation state cookie")
	errInvalidState     = errors.New("invalid federation state cookie")
)

// loginState is the server-side browser state sealed into the federation
// cookie. The random nonce doubles as the OIDC `state` parameter, which makes
// the callback replay-proof and origin-bound without a database table.
type loginState struct {
	ProviderID   string `json:"provider_id"`
	Nonce        string `json:"nonce"`
	CodeVerifier string `json:"code_verifier"`
	ReturnURL    string `json:"return_url"`
	ExpiresAt    int64  `json:"expires_at"`
}

func newLoginState(providerID, returnURL string, ttl time.Duration, now time.Time) (loginState, error) {
	nonce, err := randomToken(24)
	if err != nil {
		return loginState{}, err
	}
	verifier, err := randomToken(32)
	if err != nil {
		return loginState{}, err
	}
	return loginState{
		ProviderID: providerID, Nonce: nonce, CodeVerifier: verifier,
		ReturnURL: returnURL, ExpiresAt: now.UTC().Add(ttl).Unix(),
	}, nil
}

// sealState encrypts and authenticates the login state with the federation
// key. The sealed value is safe to store in a browser cookie.
func sealState(key []byte, state loginState) (string, error) {
	plain, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("encode federation state: %w", err)
	}
	block, err := aes.NewCipher(purposeKey(key, "state"))
	if err != nil {
		return "", fmt.Errorf("create federation state cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create federation state AEAD: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate federation state nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, plain, []byte("chaosplus:federation:state"))
	return "v1." + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func openState(key []byte, encoded string) (loginState, error) {
	version, payload, ok := strings.Cut(encoded, ".")
	if !ok || version != "v1" {
		return loginState{}, errUnsupportedState
	}
	sealed, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return loginState{}, errInvalidState
	}
	block, err := aes.NewCipher(purposeKey(key, "state"))
	if err != nil {
		return loginState{}, errInvalidState
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return loginState{}, errInvalidState
	}
	if len(sealed) < gcm.NonceSize() {
		return loginState{}, errInvalidState
	}
	nonce, ciphertext := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, []byte("chaosplus:federation:state"))
	if err != nil {
		return loginState{}, errInvalidState
	}
	var state loginState
	if err := json.Unmarshal(plain, &state); err != nil || state.Nonce == "" || state.CodeVerifier == "" {
		return loginState{}, errInvalidState
	}
	return state, nil
}

// codeChallenge derives the PKCE S256 challenge for a code verifier.
func codeChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func stateCookie(encoded string, secure bool, maxAge int) string {
	return (&http.Cookie{Name: stateCookieName, Value: encoded, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: maxAge}).String()
}

func stateClearCookie(secure bool) string {
	return (&http.Cookie{Name: stateCookieName, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: -1}).String()
}
