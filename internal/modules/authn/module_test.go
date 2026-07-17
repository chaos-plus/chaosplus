package authn

import (
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
)

func TestRegisterREST(t *testing.T) {
	verifier, err := authnext.NewVerifier(authnext.Config{})
	require.NoError(t, err)
	m := NewModule(verifier, nil)
	_, api := humatest.New(t)
	m.RegisterREST(api)
	assert.NotNil(t, m)
}

func TestRegisterRESTNilVerifier(t *testing.T) {
	m := NewModule(nil, nil)
	_, api := humatest.New(t)
	m.RegisterREST(api)
	assert.NotNil(t, m)
}
