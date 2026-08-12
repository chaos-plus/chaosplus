package plugin

import (
	"sync/atomic"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

func newTestIDGenerator() func() (guid.ID, error) {
	var next atomic.Int64
	return func() (guid.ID, error) {
		return guid.ID(next.Add(1)), nil
	}
}
