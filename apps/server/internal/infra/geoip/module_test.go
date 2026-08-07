package geoip

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestModuleStartsWithCancelledLifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	module := NewModule(Config{})
	assert.NoError(t, module.Start(ctx))
	assert.NoError(t, module.Stop(t.Context()))
}
