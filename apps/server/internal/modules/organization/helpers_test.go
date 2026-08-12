package organization

import (
	"hash/fnv"
	"strconv"
	"sync/atomic"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

// testID derives a stable positive snowflake-shaped identifier from a test
// label so fixtures keep readable names while every internal ID is a guid.ID.
func testID(value string) guid.ID {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(value))
	return guid.ID(int64(hash.Sum64() >> 1))
}

// wireID returns the decimal wire form of testID.
func wireID(value string) string {
	return strconv.FormatInt(int64(testID(value)), 10)
}

// wireGUID parses the decimal wire form of testID back into a guid.ID.
func wireGUID(value string) guid.ID {
	id, _ := guid.Parse(wireID(value))
	return id
}


// newTestIDGenerator returns a monotonically increasing guid generator for
// repository and service fixtures.
func newTestIDGenerator() func() (guid.ID, error) {
	var next atomic.Int64
	return func() (guid.ID, error) {
		return guid.ID(next.Add(1)), nil
	}
}
