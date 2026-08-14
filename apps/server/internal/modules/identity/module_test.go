package identity

import (
	"hash/fnv"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func guidString(value guid.ID) string { return value.String() }

func newTestIDGenerator() func() (guid.ID, error) {
	var next atomic.Int64
	return func() (guid.ID, error) { return guid.ID(next.Add(1)), nil }
}

func TestModuleRegistersIdentityOperations(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	registrar := authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry())
	module := NewModule(db, registrar, newIdentityAuditAppender(db), iam.NewAdministratorGuard(), newTestIDGenerator())
	_, api := humatest.New(t)

	module.RegisterREST(api)
	assert.Contains(t, api.OpenAPI().Paths, "/iam/principals")
	assert.Contains(t, api.OpenAPI().Paths, "/iam/principals/{id}")
	assert.Panics(t, func() {
		NewModule(db, nil, newIdentityAuditAppender(db), iam.NewAdministratorGuard(), newTestIDGenerator())
	})
	assert.Panics(t, func() { NewModule(db, registrar, nil, iam.NewAdministratorGuard(), newTestIDGenerator()) })
	assert.Panics(t, func() { NewModule(db, registrar, newIdentityAuditAppender(db), nil, newTestIDGenerator()) })
	assert.Panics(t, func() { NewModuleWithService(nil, nil) })
}
