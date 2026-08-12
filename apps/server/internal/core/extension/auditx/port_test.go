package auditx

import (
	"testing"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/stretchr/testify/assert"
)

func TestNewEventUsesVerifiedActorAndBoundsIdentifiers(t *testing.T) {
	event := NewEvent(t.Context(), guid.ID(1), "changed", "principal", guid.ID(2))
	assert.Zero(t, event.PrincipalID)
	assert.Equal(t, guid.ID(1), event.TenantID)
	assert.Equal(t, guid.ID(2), event.TargetID)

	ctx := authnext.WithClaims(t.Context(), &authnext.Claims{PrincipalID: guid.ID(9)})
	event = NewEvent(ctx, guid.ID(3), "changed", "principal", guid.ID(4))
	assert.Equal(t, guid.ID(9), event.PrincipalID)
	assert.Equal(t, guid.ID(3), event.TenantID)
	assert.Equal(t, guid.ID(4), event.TargetID)
	assert.NotNil(t, event.Detail)
}
