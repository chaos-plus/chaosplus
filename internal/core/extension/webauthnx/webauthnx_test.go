package webauthnx

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCeremonyOptionsAndValidationBoundaries(t *testing.T) {
	adapter, err := New(Config{
		RPID: "example.com", DisplayName: "Chaosplus", Origins: []string{"https://example.com"},
	})
	require.NoError(t, err)
	user, err := NewUser([]byte("opaque-user-handle"), "alice", "Alice", nil)
	require.NoError(t, err)
	expires := time.Now().Add(5 * time.Minute).UTC()

	creation, registrationSession, err := adapter.BeginRegistration(user, expires)
	require.NoError(t, err)
	assert.Contains(t, string(creation), `"residentKey":"required"`)
	assert.Contains(t, string(creation), `"userVerification":"required"`)
	assert.NotEmpty(t, registrationSession)
	_, err = adapter.FinishRegistration(user, registrationSession, []byte(`{"invalid":true}`))
	assert.ErrorIs(t, err, ErrInvalidCredential)

	assertion, loginSession, err := adapter.BeginLogin(expires)
	require.NoError(t, err)
	assert.Contains(t, string(assertion), `"userVerification":"required"`)
	assert.NotEmpty(t, loginSession)
	_, _, err = adapter.FinishLogin(loginSession, []byte(`{"invalid":true}`), func(_, _ []byte) (User, error) {
		return user, nil
	})
	assert.ErrorIs(t, err, ErrInvalidCredential)

	_, err = NewUser([]byte("id"), "name", "display", [][]byte{[]byte(`not-json`)})
	assert.ErrorIs(t, err, ErrInvalidCredential)
	var decoded map[string]any
	assert.NoError(t, json.Unmarshal(creation, &decoded))
}

func TestConfigurationRequiresTrustedOrigins(t *testing.T) {
	_, err := New(Config{RPID: "example.com", DisplayName: "Chaosplus"})
	assert.Error(t, err)
}

func TestStoredUserCredentialAndSessionEncoding(t *testing.T) {
	stored := webauthn.Credential{
		ID:        []byte{1, 2, 3, 4},
		PublicKey: []byte{5, 6, 7},
		Flags:     webauthn.NewCredentialFlags(protocol.FlagUserPresent | protocol.FlagUserVerified),
		Authenticator: webauthn.Authenticator{
			AAGUID: []byte{8, 9}, SignCount: 42, CloneWarning: true,
		},
	}
	data, err := json.Marshal(stored)
	require.NoError(t, err)
	handle := []byte("stable-user-handle")
	user, err := NewUser(handle, "alice", "Alice Example", [][]byte{data})
	require.NoError(t, err)

	assert.Equal(t, handle, user.WebAuthnID())
	assert.Equal(t, "alice", user.WebAuthnName())
	assert.Equal(t, "Alice Example", user.WebAuthnDisplayName())
	require.Len(t, user.WebAuthnCredentials(), 1)
	assert.Equal(t, stored.ID, user.WebAuthnCredentials()[0].ID)
	handle[0] = 'X'
	assert.Equal(t, byte('s'), user.WebAuthnID()[0], "the adapter owns an independent user handle")

	encoded, err := encodeCredential(&stored)
	require.NoError(t, err)
	assert.Equal(t, stored.ID, encoded.ID)
	assert.Equal(t, uint32(42), encoded.SignCount)
	assert.True(t, encoded.CloneWarning)
	var roundTrip webauthn.Credential
	require.NoError(t, json.Unmarshal(encoded.Data, &roundTrip))
	assert.Equal(t, stored.PublicKey, roundTrip.PublicKey)

	expires := time.Now().Add(time.Minute).UTC().Truncate(time.Nanosecond)
	sessionData, err := json.Marshal(webauthn.SessionData{
		Challenge: "challenge", RelyingPartyID: "example.com", UserID: user.WebAuthnID(),
		Expires: expires, UserVerification: protocol.VerificationRequired,
	})
	require.NoError(t, err)
	decoded, err := decodeSession(sessionData)
	require.NoError(t, err)
	assert.Equal(t, "challenge", decoded.Challenge)
	assert.Equal(t, expires, decoded.Expires)
	_, err = decodeSession([]byte(`{"expires":`))
	assert.ErrorIs(t, err, ErrInvalidCredential)

	adapter, err := New(Config{RPID: "example.com", DisplayName: "Chaosplus", Origins: []string{"https://example.com"}})
	require.NoError(t, err)
	_, err = adapter.FinishRegistration(user, []byte(`not-json`), nil)
	assert.ErrorIs(t, err, ErrInvalidCredential)
	_, _, err = adapter.FinishLogin([]byte(`not-json`), nil, func(_, _ []byte) (User, error) { return user, nil })
	assert.ErrorIs(t, err, ErrInvalidCredential)
}
