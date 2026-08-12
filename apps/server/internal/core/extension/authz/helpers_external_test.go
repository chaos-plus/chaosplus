package authz_test

import (
	"hash/fnv"
	"strconv"
	"sync/atomic"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

func testID(value string) guid.ID {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(value))
	return guid.ID(int64(hash.Sum64() >> 1))
}

func wireID(value string) string {
	return strconv.FormatInt(int64(testID(value)), 10)
}

func parseGUID(value string) guid.ID {
	id, _ := guid.Parse(value)
	return id
}

func guidString(value guid.ID) string { return value.String() }

func newTestIDGenerator() func() (guid.ID, error) {
	var next atomic.Int64
	return func() (guid.ID, error) {
		return guid.ID(next.Add(1)), nil
	}
}
