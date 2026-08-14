package machine

import (
	"context"
	"time"

	runnerprotocol "github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/machine/protocol"
	coreid "github.com/chaos-plus/chaosplus/internal/infra/guid"
)

type Spawn = runnerprotocol.Spawn
type Command = runnerprotocol.Command
type Reply = runnerprotocol.Reply
type Event = runnerprotocol.Event
type EventPreview = runnerprotocol.EventPreview
type RouteLease = runnerprotocol.RouteLease
type CommandEnvelope = runnerprotocol.CommandEnvelope
type ReplyEnvelope = runnerprotocol.ReplyEnvelope
type EventEnvelope = runnerprotocol.EventEnvelope
type ReplyFunc = runnerprotocol.ReplyFunc
type CommandHandler = runnerprotocol.CommandHandler
type EventHandler = runnerprotocol.EventHandler
type ClusterTransport = runnerprotocol.ClusterTransport

type PendingToken struct {
	MachineID coreid.ID
	TenantID  coreid.ID
	EntityID  coreid.ID
	OwnerID   coreid.ID
	TokenHash string
	ExpiresAt int64
	CreatedAt int64
}

type RouteDirectory interface {
	ResolveActiveRoute(context.Context, coreid.ID) (RouteLease, error)
	ListActiveRoutes(context.Context) ([]RouteLease, error)
	RouteIsCurrent(context.Context, RouteLease) (bool, error)
}

type ConnectionDirectory interface {
	RouteDirectory
	StorePendingToken(context.Context, PendingToken) error
	PendingTokenForMachine(context.Context, coreid.ID) (PendingToken, error)
	FindToken(context.Context, string) (AccessToken, error)
	DeletePendingToken(context.Context, coreid.ID, string) error
	AcquireRoute(context.Context, coreid.ID, coreid.ID, time.Duration) (RouteLease, error)
	RenewRoute(context.Context, RouteLease, time.Duration) (RouteLease, error)
	ReleaseRoute(context.Context, RouteLease) error
	RevokeRoute(context.Context, coreid.ID) error
	UpdateRouteInventory(context.Context, RouteLease, string, []string, string, string) error
	RouteInventory(context.Context, coreid.ID) (string, []string, string, string, error)
}
