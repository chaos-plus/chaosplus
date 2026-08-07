package providers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIP2RegionCancelledStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	provider := &IP2Region{}
	assert.NoError(t, provider.Start(ctx))
	assert.NoError(t, provider.Stop(t.Context()))
}
