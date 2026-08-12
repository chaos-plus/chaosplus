// Package gateway implements the server-ai side of the daemon ↔
// server-ai NATS contract (PRD §17.1/C3, F.1 RunnerGateway).
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

// Subject scheme — mirrors apps/runner/src/nats/transport.ts.
const (
	registerSubject   = "chaos.runner.register"
	cmdSubjectFmt     = "chaos.runner.%s.cmd"
	evtSubjectFmt     = "chaos.runner.%s.evt"
	activationTimeout = 5 * time.Second
)

// Spawn mirrors the daemon's SpawnCommand (AgentTask + run identity).
type Spawn struct {
	RunID        string   `json:"runId"`
	NodeID       string   `json:"nodeId"`
	Attempt      int      `json:"attempt"`
	SpawnID      string   `json:"spawnId"`
	ExecutorType string   `json:"executorType"`
	Prompt       string   `json:"prompt"`
	Cwd          string   `json:"cwd"`
	SystemPrompt string   `json:"systemPrompt,omitempty"`
	Model        string   `json:"model,omitempty"`
	Provider     string   `json:"provider,omitempty"`
	APIKey       string   `json:"apiKey,omitempty"`
	AllowedTools []string `json:"allowedTools,omitempty"`
	MaxTurns     int      `json:"maxTurns,omitempty"`
}

type runnerCmd struct {
	Type      string `json:"type"`
	Spawn     *Spawn `json:"spawn,omitempty"`
	SpawnID   string `json:"spawnId,omitempty"`
	Provider  string `json:"provider,omitempty"`
	APIKey    string `json:"apiKey,omitempty"`
	Path      string `json:"path,omitempty"`
	Cmd       string `json:"cmd,omitempty"`
	TimeoutMs int    `json:"timeoutMs,omitempty"`
}

type runnerReply struct {
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
	lastSeen map[string]time.Time
	waiters  map[string]*spawnWaiter // (runnerID,spawnID) -> exclusive delivery
}

type spawnWaiter struct {
	terminal chan RunnerEvent
	activity chan RunnerEvent
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
		lastSeen: make(map[string]time.Time),
		waiters:  make(map[string]*spawnWaiter),
	}
}

// Start subscribes to runner registrations and every runner's event subject.
// It returns after the server acknowledges the subscriptions; cancellation
// removes them during application shutdown.
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
		g.lastSeen[req.RunnerID] = time.Now()
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
		var envelope struct {
			SpawnID string `json:"spawnId"`
		}
		_ = json.Unmarshal(m.Data, &envelope)
		g.mu.Lock()
		g.lastSeen[runnerID] = time.Now()
		waiter := g.waiters[waiterKey(runnerID, envelope.SpawnID)]
		g.mu.Unlock()
		if waiter != nil {
			switch ev.Type {
			case "spawn-done", "spawn-error":
				select {
				case waiter.terminal <- ev:
				default:
				}
			default:
				select {
				case waiter.activity <- ev:
				default:
					select {
					case <-waiter.activity:
					default:
					}
					select {
					case waiter.activity <- ev:
					default:
					}
				}
			}
		}
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
	activationCtx, cancelActivation := context.WithTimeout(ctx, activationTimeout)
	defer cancelActivation()
	if err := g.nc.FlushWithContext(activationCtx); err != nil {
		_ = reg.Unsubscribe()
		_ = evt.Unsubscribe()
		return fmt.Errorf("activate runner subscriptions: %w", err)
	}
	go func() {
		<-ctx.Done()
		_ = reg.Unsubscribe()
		_ = evt.Unsubscribe()
	}()
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
	if err := g.roundTrip(ctx, fmt.Sprintf(cmdSubjectFmt, runnerID), cmd); err != nil {
		return err
	}
	g.touchRunner(runnerID)
	return nil
}

// SpawnResult is the outcome of a completed spawn.
type SpawnResult struct {
	OK       bool
	ExitCode int
	Error    string
	Preview  *struct {
		Type    string `json:"type"`
		Content string `json:"content"`
	}
}

// SpawnWaitOption configures activity-based timeout on SpawnAndWait.
type SpawnWaitOption func(*spawnWaitConfig)

type spawnWaitConfig struct {
	idle      time.Duration // no matching spawn event for this long → timeout
	max       time.Duration // absolute cap regardless of activity
	heartbeat time.Duration // no runner activity for this long → runner lost
}

// WithIdleTimeout fails the wait after `d` with no matching spawn event.
func WithIdleTimeout(d time.Duration) SpawnWaitOption {
	return func(c *spawnWaitConfig) { c.idle = d }
}

// WithMaxTimeout caps the total wait even if activity keeps arriving.
func WithMaxTimeout(d time.Duration) SpawnWaitOption {
	return func(c *spawnWaitConfig) { c.max = d }
}

// WithHeartbeatTimeout fails an in-flight spawn when the selected runner has
// produced no register, heartbeat, or spawn activity for d. This closes the
// Phantom Running gap independently of the (usually longer) agent idle timeout.
func WithHeartbeatTimeout(d time.Duration) SpawnWaitOption {
	return func(c *spawnWaitConfig) { c.heartbeat = d }
}

var ErrRunnerHeartbeatLost = errors.New("runner heartbeat lost")

// SpawnAndWait sends a spawn command and blocks until the matching
// spawn-done / spawn-error event arrives for that spawnId. Events are consumed
// from the gateway's stream, so callers must not concurrently drain Events().
// Callers wanting activity-based timeout use SpawnAndWaitOpts.
func (g *Gateway) SpawnAndWait(ctx context.Context, runnerID string, sp Spawn) (SpawnResult, error) {
	return g.SpawnAndWaitOpts(ctx, runnerID, sp)
}

// SpawnAndWaitOpts is SpawnAndWait with timeout options. Every matching event
// (started/event/done/error) resets the idle timer; max is an absolute bound.
func (g *Gateway) SpawnAndWaitOpts(ctx context.Context, runnerID string, sp Spawn, opts ...SpawnWaitOption) (SpawnResult, error) {
	cfg := &spawnWaitConfig{}
	for _, o := range opts {
		o(cfg)
	}
	waiter, unregister, err := g.registerWaiter(runnerID, sp.SpawnID)
	if err != nil {
		return SpawnResult{}, err
	}
	defer unregister()
	if err := g.Spawn(ctx, runnerID, sp); err != nil {
		return SpawnResult{}, err
	}

	maxCtx := ctx
	cancelMax := func() {}
	if cfg.max > 0 {
		maxCtx, cancelMax = context.WithTimeout(ctx, cfg.max)
	}
	defer cancelMax()
	g.mu.Lock()
	if _, ok := g.lastSeen[runnerID]; !ok {
		g.lastSeen[runnerID] = time.Now()
	}
	g.mu.Unlock()

	var idle *time.Timer
	idleC := make(chan time.Time, 1)
	if cfg.idle > 0 {
		idle = time.AfterFunc(cfg.idle, func() { idleC <- time.Time{} })
	}
	resetIdle := func() {
		if idle != nil {
			idle.Reset(cfg.idle)
		}
	}
	var heartbeat *time.Ticker
	var heartbeatC <-chan time.Time
	if cfg.heartbeat > 0 {
		interval := cfg.heartbeat / 3
		if interval < 50*time.Millisecond {
			interval = 50 * time.Millisecond
		}
		heartbeat = time.NewTicker(interval)
		heartbeatC = heartbeat.C
		defer heartbeat.Stop()
	}

	for {
		select {
		case ev := <-waiter.terminal:
			var p struct {
				SpawnID  string `json:"spawnId"`
				OK       *bool  `json:"ok"`
				ExitCode int    `json:"exitCode"`
				Message  string `json:"message"`
				Preview  *struct {
					Type    string `json:"type"`
					Content string `json:"content"`
				} `json:"preview,omitempty"`
			}
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				continue
			}
			if p.SpawnID != sp.SpawnID {
				continue
			}
			switch ev.Type {
			case "spawn-done":
				ok := p.OK == nil || *p.OK
				return SpawnResult{OK: ok, ExitCode: p.ExitCode, Preview: p.Preview}, nil
			case "spawn-error":
				return SpawnResult{OK: false, Error: p.Message}, nil
			}
		case <-waiter.activity:
			resetIdle()
		case <-idleC:
			return SpawnResult{}, fmt.Errorf("spawn %s idle timeout after %s", sp.SpawnID, cfg.idle)
		case <-heartbeatC:
			g.mu.Lock()
			lastSeen := g.lastSeen[runnerID]
			g.mu.Unlock()
			if time.Since(lastSeen) >= cfg.heartbeat {
				return SpawnResult{}, fmt.Errorf("%w: runner %s silent for %s", ErrRunnerHeartbeatLost, runnerID, cfg.heartbeat)
			}
		case <-maxCtx.Done():
			return SpawnResult{}, maxCtx.Err()
		}
	}
}

func waiterKey(runnerID, spawnID string) string { return runnerID + "\x00" + spawnID }

func (g *Gateway) registerWaiter(runnerID, spawnID string) (*spawnWaiter, func(), error) {
	if runnerID == "" || spawnID == "" {
		return nil, nil, fmt.Errorf("runnerID and spawnID are required")
	}
	key := waiterKey(runnerID, spawnID)
	waiter := &spawnWaiter{terminal: make(chan RunnerEvent, 1), activity: make(chan RunnerEvent, 1)}
	g.mu.Lock()
	if _, exists := g.waiters[key]; exists {
		g.mu.Unlock()
		return nil, nil, fmt.Errorf("spawn %s already has a waiter", spawnID)
	}
	g.waiters[key] = waiter
	g.mu.Unlock()
	return waiter, func() {
		g.mu.Lock()
		if g.waiters[key] == waiter {
			delete(g.waiters, key)
		}
		g.mu.Unlock()
	}, nil
}

func (g *Gateway) touchRunner(runnerID string) {
	g.mu.Lock()
	g.lastSeen[runnerID] = time.Now()
	g.mu.Unlock()
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
	_, err := g.roundTripReply(ctx, subject, payload)
	return err
}

func (g *Gateway) roundTripReply(ctx context.Context, subject string, payload []byte) (runnerReply, error) {
	reply, err := g.nc.RequestWithContext(ctx, subject, payload)
	if err != nil {
		return runnerReply{}, fmt.Errorf("request %s: %w", subject, err)
	}
	var r runnerReply
	if err := json.Unmarshal(reply.Data, &r); err != nil {
		return runnerReply{}, fmt.Errorf("bad reply on %s: %w", subject, err)
	}
	if !r.OK {
		// daemon's reply contract is {ok, data}; errors ride inside data.error.
		msg := r.Error
		if msg == "" {
			msg = r.Data.Error
		}
		return r, errors.New(msg)
	}
	return r, nil
}

// ReadArtifact asks the runner to read a workspace file (relative to the spawn's
// cwd, sandboxed) and returns its contents.
func (g *Gateway) ReadArtifact(ctx context.Context, runnerID, spawnID, path string) ([]byte, error) {
	cmd, err := json.Marshal(runnerCmd{Type: "read-file", SpawnID: spawnID, Path: path})
	if err != nil {
		return nil, err
	}
	r, err := g.roundTripReply(ctx, fmt.Sprintf(cmdSubjectFmt, runnerID), cmd)
	if err != nil {
		return nil, err
	}
	return []byte(r.Data.Content), nil
}

// CmdResult is the outcome of running a validator command on a runner.
type CmdResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// RunCmd executes a PRD F.5 'cmd:' validator template in the spawn's workspace
// on the runner and returns its output. Non-zero exit = validation failed.
func (g *Gateway) RunCmd(ctx context.Context, runnerID, spawnID, cmdTemplate string, timeoutMs int) (CmdResult, error) {
	cmd, err := json.Marshal(runnerCmd{Type: "run-cmd", SpawnID: spawnID, Cmd: cmdTemplate, TimeoutMs: timeoutMs})
	if err != nil {
		return CmdResult{}, err
	}
	r, err := g.roundTripReply(ctx, fmt.Sprintf(cmdSubjectFmt, runnerID), cmd)
	if err != nil {
		return CmdResult{}, err
	}
	return CmdResult{ExitCode: r.Data.ExitCode, Stdout: r.Data.Stdout, Stderr: r.Data.Stderr}, nil
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
