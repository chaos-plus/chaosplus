package federation

import (
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return key
}

func TestLoginStateSealOpenRoundTrip(t *testing.T) {
	key := testKey()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	state, err := newLoginState("provider-1", "https://app.example/", 10*time.Minute, now)
	require.NoError(t, err)
	assert.Equal(t, "provider-1", state.ProviderID)
	assert.NotEmpty(t, state.Nonce)
	assert.NotEmpty(t, state.CodeVerifier)
	assert.Equal(t, now.Add(10*time.Minute).Unix(), state.ExpiresAt)

	sealed, err := sealState(key, state)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(sealed, "v1."))

	opened, err := openState(key, sealed)
	require.NoError(t, err)
	assert.Equal(t, state, opened)
}

func TestLoginStateTamperAndWrongKeyRejected(t *testing.T) {
	key := testKey()
	state, err := newLoginState("provider-1", "https://app.example/", time.Minute, time.Now())
	require.NoError(t, err)
	sealed, err := sealState(key, state)
	require.NoError(t, err)

	tampered := sealed[:len(sealed)-3] + "AAA"
	_, err = openState(key, tampered)
	assert.ErrorIs(t, err, errInvalidState)

	wrong := append([]byte(nil), key...)
	wrong[0] ^= 0xff
	_, err = openState(wrong, sealed)
	assert.ErrorIs(t, err, errInvalidState)

	_, err = openState(key, "v2."+strings.TrimPrefix(sealed, "v1."))
	assert.ErrorIs(t, err, errUnsupportedState)

	_, err = openState(key, "v1.not-base64!!!")
	assert.ErrorIs(t, err, errInvalidState)
}

func TestCodeChallengeS256(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	challenge := codeChallenge(verifier)
	assert.Len(t, challenge, 43)
	assert.Equal(t, challenge, codeChallenge(verifier))
	assert.NotEqual(t, challenge, codeChallenge(verifier+"x"))
	raw, err := base64.RawURLEncoding.DecodeString(challenge)
	require.NoError(t, err)
	assert.Len(t, raw, 32)
}

func TestStateCookies(t *testing.T) {
	cookie := stateCookie("value", true, 600)
	parsed := (&http.Response{Header: http.Header{"Set-Cookie": []string{cookie}}}).Cookies()
	require.Len(t, parsed, 1)
	assert.Equal(t, "value", parsed[0].Value)
	assert.True(t, parsed[0].HttpOnly)
	assert.True(t, parsed[0].Secure)
	assert.Equal(t, 600, parsed[0].MaxAge)

	cleared := stateClearCookie(false)
	parsedClear := (&http.Response{Header: http.Header{"Set-Cookie": []string{cleared}}}).Cookies()
	require.Len(t, parsedClear, 1)
	assert.Equal(t, -1, parsedClear[0].MaxAge)
	assert.False(t, parsedClear[0].Secure)
}

func TestLoginStateOpenRejectsInvalidPayloads(t *testing.T) {
	key := testKey()
	_, err := openState(key, "v1.AA")
	assert.ErrorIs(t, err, errInvalidState)

	state, err := sealState(key, loginState{ProviderID: "p", ReturnURL: "https://app.example/", ExpiresAt: time.Now().Add(time.Minute).Unix()})
	require.NoError(t, err)
	_, err = openState(key, state)
	assert.ErrorIs(t, err, errInvalidState)
}
