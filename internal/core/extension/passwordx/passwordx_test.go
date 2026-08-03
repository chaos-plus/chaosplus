package passwordx

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashAndVerify(t *testing.T) {
	hash, err := Hash("correct horse battery staple")
	require.NoError(t, err)
	assert.NotContains(t, hash, "correct horse")
	ok, err := Verify(hash, "correct horse battery staple")
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = Verify(hash, "wrong")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestRejectsInvalidInput(t *testing.T) {
	_, err := Hash("")
	assert.Error(t, err)
	_, err = Verify("bad", "password")
	assert.ErrorIs(t, err, ErrInvalidHash)
}

func TestVerifyRejectsMalformedArgon2Parameters(t *testing.T) {
	validSalt := "MTIzNDU2Nzg5MGFiY2RlZg"
	validHash := "MTIzNDU2Nzg5MGFiY2RlZjEyMzQ1Njc4OTBhYmNkZWY"
	for _, encoded := range []string{
		"$argon2id$v=19$broken$" + validSalt + "$" + validHash,
		"$argon2id$v=19$m=abc,t=2,p=1$" + validSalt + "$" + validHash,
		"$argon2id$v=19$m=19456,t=2,x=1$" + validSalt + "$" + validHash,
		"$argon2id$v=19$m=1,t=2,p=1$" + validSalt + "$" + validHash,
		"$argon2id$v=19$m=19456,t=2,p=1$invalid$" + validHash,
		"$argon2id$v=19$m=19456,t=2,p=1$MTIz$" + validHash,
		"$argon2id$v=19$m=19456,t=2,p=1$" + validSalt + "$invalid",
		"$argon2id$v=19$m=19456,t=2,p=1$" + validSalt + "$MTIz",
	} {
		_, err := Verify(encoded, "password")
		assert.ErrorIs(t, err, ErrInvalidHash, encoded)
	}
}
