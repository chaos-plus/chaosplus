// Package auditx defines the transaction-local audit port shared by security modules.
package auditx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/uptrace/bun"
)

const SystemActor = "_system"

type Event struct {
	TenantID    string
	PrincipalID string
	EventType   string
	TargetType  string
	TargetID    string
	Detail      map[string]any
}

type Appender func(context.Context, bun.IDB, Event) error

func NewEvent(ctx context.Context, tenantID, eventType, targetType, targetID string) Event {
	principalID := SystemActor
	if claims, ok := authnext.FromContext(ctx); ok && claims.Subject != "" {
		principalID = BoundedID(claims.Subject, 64)
	}
	return Event{
		TenantID: tenantID, PrincipalID: principalID, EventType: eventType,
		TargetType: targetType, TargetID: BoundedID(targetID, 128), Detail: map[string]any{},
	}
}

func BoundedID(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	sum := sha256.Sum256([]byte(value))
	encoded := hex.EncodeToString(sum[:])
	if limit < len(encoded) {
		return encoded[:limit]
	}
	return encoded
}
