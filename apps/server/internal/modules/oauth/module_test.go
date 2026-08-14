package oauth

import (
	"hash/fnv"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
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

func TestModuleRegistersOAuthRoutes(t *testing.T) {
	service, authentication, _ := newOAuthTestService(t)
	module := NewModule(service.db, authentication, nil, newTestIDGenerator())
	_, api := humatest.New(t)
	module.RegisterREST(api)

	assert.Equal(t, http.StatusOK, api.Get("/.well-known/openid-configuration").Code)
	assert.NotNil(t, api.OpenAPI().Paths["/oauth/token"].Post)
}
