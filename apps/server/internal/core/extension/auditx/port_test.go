package auditx

import (
	"strings"
	"testing"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/stretchr/testify/assert"
)

func TestNewEventUsesVerifiedActorAndBoundsIdentifiers(t *testing.T) {
	event := NewEvent(t.Context(), "tenant", "changed", "principal", "target")
	assert.Equal(t, SystemActor, event.PrincipalID)

	ctx := authnext.WithClaims(t.Context(), &authnext.Claims{Subject: strings.Repeat("a", 65)})
	event = NewEvent(ctx, "tenant", "changed", "principal", strings.Repeat("b", 129))
	assert.Len(t, event.PrincipalID, 64)
	assert.Len(t, event.TargetID, 64)
	assert.NotNil(t, event.Detail)
	assert.Equal(t, "short", BoundedID("short", 8))
	assert.Len(t, BoundedID(strings.Repeat("x", 9), 8), 8)
}
