package authn

import (
	"context"
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

func TestModuleRecoveryWorkerLifecycle(t *testing.T) {
	service, _ := newLocalService(t)
	module := NewModule(nil, service)
	require.NoError(t, module.Start(t.Context()))
	require.NoError(t, module.Stop(t.Context()))

	module = NewModule(nil, nil)
	require.NoError(t, module.Start(context.Background()))
	require.NoError(t, module.Stop(context.Background()))
}
