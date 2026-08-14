package protocol

import (
	"context"
	"encoding/json"
	"io"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

type Spawn struct {
	RunID        string            `json:"runId"`
	NodeID       string            `json:"nodeId"`
	Attempt      int               `json:"attempt"`
	SpawnID      string            `json:"spawnId"`
	ExecutorType string            `json:"executorType"`
	Prompt       string            `json:"prompt"`
	Cwd          string            `json:"cwd"`
	SystemPrompt string            `json:"systemPrompt,omitempty"`
	Model        string            `json:"model,omitempty"`
	Provider     string            `json:"provider,omitempty"`
	APIKey       string            `json:"apiKey,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	AllowedTools []string          `json:"allowedTools,omitempty"`
	MaxTurns     int               `json:"maxTurns,omitempty"`
}

type Command struct {
	Type      string `json:"type"`
	Spawn     *Spawn `json:"spawn,omitempty"`
	SpawnID   string `json:"spawnId,omitempty"`
	Provider  string `json:"provider,omitempty"`
	APIKey    string `json:"apiKey,omitempty"`
	Path      string `json:"path,omitempty"`
	Cmd       string `json:"cmd,omitempty"`
	TimeoutMs int    `json:"timeoutMs,omitempty"`
}

type Reply struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Data  struct {
		Content  string `json:"content"`
		Error    string `json:"error,omitempty"`
		ExitCode int    `json:"exitCode,omitempty"`
		Stdout   string `json:"stdout,omitempty"`
		Stderr   string `json:"stderr,omitempty"`
	} `json:"data,omitempty"`
}

type Event struct {
	Seq      int64           `json:"seq"`
	Type     string          `json:"type"`
	SpawnID  string          `json:"spawnId,omitempty"`
	OK       *bool           `json:"ok,omitempty"`
	ExitCode int             `json:"exitCode,omitempty"`
	CostUSD  *float64        `json:"costUsd,omitempty"`
	Message  string          `json:"message,omitempty"`
	Error    string          `json:"error,omitempty"`
	Preview  *EventPreview   `json:"preview,omitempty"`
	Event    json.RawMessage `json:"event,omitempty"`
}

type EventPreview struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

type RouteLease struct {
	MachineID    guid.ID `json:"machineId"`
	TenantID     guid.ID `json:"tenantId"`
	EntityID     guid.ID `json:"entityId"`
	HolderID     guid.ID `json:"holderId"`
	FencingToken int64   `json:"fencingToken"`
	ExpiresAt    int64   `json:"expiresAt"`
}

func (r RouteLease) SameFence(other RouteLease) bool {
	return r.MachineID == other.MachineID && r.TenantID == other.TenantID && r.EntityID == other.EntityID &&
		r.HolderID == other.HolderID && r.FencingToken == other.FencingToken
}

type CommandEnvelope struct {
	Route   RouteLease `json:"route"`
	Command Command    `json:"command"`
}

type ReplyEnvelope struct {
	Route RouteLease `json:"route"`
	Reply Reply      `json:"reply"`
}

type EventEnvelope struct {
	Route RouteLease `json:"route"`
	Event Event      `json:"event"`
}

type ReplyFunc func(ReplyEnvelope) error
type CommandHandler func(CommandEnvelope, ReplyFunc)
type EventHandler func(EventEnvelope)

type ClusterTransport interface {
	SubscribeCommands(context.Context, RouteLease, CommandHandler) (io.Closer, error)
	Request(context.Context, RouteLease, Command) (ReplyEnvelope, error)
	PublishEvent(context.Context, EventEnvelope) error
	SubscribeEvents(context.Context, EventHandler) (io.Closer, error)
}
