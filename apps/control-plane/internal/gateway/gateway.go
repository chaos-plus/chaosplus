// Package gateway implements the control-plane side of the daemon ↔
// control-plane NATS contract (PRD §17.1/C3, F.1 RunnerGateway).
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/nats-io/nats.go"
)

// Subject scheme — mirrors apps/daemon/src/nats/transport.ts.
const (
	registerSubject = "chaos.runner.register"
	cmdSubjectFmt   = "chaos.runner.%s.cmd"
	evtSubjectFmt   = "chaos.runner.%s.evt"
)

// Spawn mirrors the daemon's SpawnCommand (AgentTask + run identity).
type Spawn struct {
	RunID        string `json:"runId"`
	NodeID       string `json:"nodeId"`
	Attempt      int    `json:"attempt"`
	SpawnID      string `json:"spawnId"`
	ExecutorType string `json:"executorType"`
	Prompt       string `json:"prompt"`
	Cwd          string `json:"cwd"`
	SystemPrompt string `json:"systemPrompt,omitempty"`
	Model        string `json:"model,omitempty"`
	Provider     string `json:"provider,omitempty"`
	APIKey       string `json:"apiKey,omitempty"`
}

type runnerCmd struct {
	Type     string `json:"type"`
	Spawn    *Spawn `json:"spawn,omitempty"`
	SpawnID  string `json:"spawnId,omitempty"`
	Provider string `json:"provider,omitempty"`
	APIKey   string `json:"apiKey,omitempty"`
}

type runnerReply struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// RunnerEvent is a raw event payload published by a runner.
type RunnerEvent struct {
	Seq      int             `json:"seq"`
	Type     string          `json:"type"`
	RunnerID string          `json:"-"`
	Payload  json.RawMessage `json:"-"`
}

// Gateway sends commands to runners and collects their events over NATS.
type Gateway struct {
	nc       *nats.Conn
	events   chan RunnerEvent
	onEvent  func(RunnerEvent) // optional sink (e.g. event-log persistence)
	mu       sync.Mutex
	onRunner map[string]struct{} // runners seen via register
}

// OnEvent registers a sink invoked for every runner event (in addition to
// Events() channel delivery).
func (g *Gateway) OnEvent(fn func(RunnerEvent)) {
	g.onEvent = fn
}

func New(nc *nats.Conn) *Gateway {
	return &Gateway{
		nc:       nc,
		events:   make(chan RunnerEvent, 256),
		onRunner: make(map[string]struct{}),
	}
}

// Start subscribes to runner registrations and every runner's event subject.
func (g *Gateway) Start(ctx context.Context) error {
	reg, err := g.nc.Subscribe(registerSubject, func(m *nats.Msg) {
		var req struct {
			RunnerID string            `json:"runnerId"`
			Meta     map[string]string `json:"meta"`
		}
		if err := json.Unmarshal(m.Data, &req); err != nil {
			_ = m.Respond([]byte(`{"ok":false,"error":"bad register payload"}`))
			return
		}
		g.mu.Lock()
		g.onRunner[req.RunnerID] = struct{}{}
		g.mu.Unlock()
		_ = m.Respond([]byte(`{"ok":true}`))
	})
	if err != nil {
		return fmt.Errorf("subscribe register: %w", err)
	}

	evt, err := g.nc.Subscribe("chaos.runner.*.evt", func(m *nats.Msg) {
		parts := splitSubject(m.Subject)
		if len(parts) < 3 {
			return
		}
		runnerID := parts[2]
		var ev RunnerEvent
		if err := json.Unmarshal(m.Data, &ev); err != nil {
			return
		}
		ev.RunnerID = runnerID
		ev.Payload = m.Data
		if g.onEvent != nil {
			g.onEvent(ev)
		}
		select {
		case g.events <- ev:
		default: // drop if consumer is too slow
		}
	})
	if err != nil {
		_ = reg.Unsubscribe()
		return fmt.Errorf("subscribe events: %w", err)
	}

	<-ctx.Done()
	_ = reg.Unsubscribe()
	_ = evt.Unsubscribe()
	return nil
}

// RegisteredRunners returns the runner IDs seen via register.
func (g *Gateway) RegisteredRunners() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]string, 0, len(g.onRunner))
	for id := range g.onRunner {
		out = append(out, id)
	}
	return out
}

// Events exposes the runner event stream (type + raw payload).
func (g *Gateway) Events() <-chan RunnerEvent { return g.events }

// Spawn sends a spawn command to a runner and waits for its reply.
func (g *Gateway) Spawn(ctx context.Context, runnerID string, sp Spawn) error {
	cmd, err := json.Marshal(runnerCmd{Type: "spawn", Spawn: &sp})
	if err != nil {
		return err
	}
	return g.roundTrip(ctx, fmt.Sprintf(cmdSubjectFmt, runnerID), cmd)
}

// Kill sends a kill command for a running spawn.
func (g *Gateway) Kill(ctx context.Context, runnerID, spawnID string) error {
	cmd, err := json.Marshal(runnerCmd{Type: "kill", SpawnID: spawnID})
	if err != nil {
		return err
	}
	return g.roundTrip(ctx, fmt.Sprintf(cmdSubjectFmt, runnerID), cmd)
}

// SwitchProvider asks a runner to change provider for a spawn (cc switch).
func (g *Gateway) SwitchProvider(ctx context.Context, runnerID, spawnID, provider, apiKey string) error {
	cmd, err := json.Marshal(runnerCmd{Type: "switch-provider", SpawnID: spawnID, Provider: provider, APIKey: apiKey})
	if err != nil {
		return err
	}
	return g.roundTrip(ctx, fmt.Sprintf(cmdSubjectFmt, runnerID), cmd)
}

func (g *Gateway) roundTrip(ctx context.Context, subject string, payload []byte) error {
	reply, err := g.nc.RequestWithContext(ctx, subject, payload)
	if err != nil {
		return fmt.Errorf("request %s: %w", subject, err)
	}
	var r runnerReply
	if err := json.Unmarshal(reply.Data, &r); err != nil {
		return fmt.Errorf("bad reply on %s: %w", subject, err)
	}
	if !r.OK {
		return errors.New(r.Error)
	}
	return nil
}

func splitSubject(subj string) []string {
	out := []string{}
	start := 0
	for i, c := range subj {
		if c == '.' {
			out = append(out, subj[start:i])
			start = i + 1
		}
	}
	return append(out, subj[start:])
}
