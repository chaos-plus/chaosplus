// Package auditx defines the transaction-local audit port shared by security modules.
package auditx

import (
	"context"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

type Event struct {
	TenantID    guid.ID
	PrincipalID guid.ID
	EventType   string
	TargetType  string
	TargetID    guid.ID
	Detail      map[string]any
}

type Appender func(context.Context, bun.IDB, Event) error

func NewEvent(ctx context.Context, tenantID guid.ID, eventType, targetType string, targetID guid.ID) Event {
	var principalID guid.ID
	if claims, ok := authnext.FromContext(ctx); ok {
		principalID = claims.PrincipalID
	}
	return Event{
		TenantID: tenantID, PrincipalID: principalID, EventType: eventType,
		TargetType: targetType, TargetID: targetID, Detail: map[string]any{},
	}
}
