package secure

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOriginPolicy(t *testing.T) {
	policy, err := NewOriginPolicy([]string{"https://admin.example.com"})
	require.NoError(t, err)
	for _, test := range []struct {
		origin string
		want   bool
	}{
		{"", true},
		{"https://control.example.com", true},
		{"https://admin.example.com", true},
		{"https://evil.example.com", false},
		{"://", false},
	} {
		request := httptest.NewRequest("GET", "https://control.example.com/ws", nil)
		request.Header.Set("Origin", test.origin)
		assert.Equal(t, test.want, policy.Allows(request), test.origin)
	}
	_, err = NewOriginPolicy([]string{"*"})
	assert.ErrorIs(t, err, ErrInvalidOrigin)
}
