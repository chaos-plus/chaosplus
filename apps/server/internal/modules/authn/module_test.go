package authn

import (
	"context"
	"hash/fnv"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

func testID(value string) guid.ID {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(value))
	id, err := guid.Parse(strconv.FormatUint(hash.Sum64()>>1, 10))
	if err != nil {
		panic(err)
	}
	return id
}

func wireID(value string) string { return strconv.FormatInt(int64(testID(value)), 10) }

func parseGUID(value string) guid.ID {
	id, _ := guid.Parse(value)
	return id
}

func guidString(value guid.ID) string { return value.String() }

func newTestIDGenerator() func() (guid.ID, error) {
	var next atomic.Int64
	return func() (guid.ID, error) { return guid.ID(next.Add(1)), nil }
}

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
