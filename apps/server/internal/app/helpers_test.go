package app

import (
	"hash/fnv"
	"sync/atomic"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

func testID(value string) guid.ID {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(value))
	return guid.ID(int64(hash.Sum64() >> 1))
}

func newTestIDGenerator() func() (guid.ID, error) {
	var next atomic.Int64
	return func() (guid.ID, error) {
		return guid.ID(next.Add(1)), nil
	}
}
