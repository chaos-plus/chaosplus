// Package gateway implements the control-plane's runner command/event adapter.
// Its NATS subjects are internal cluster transport details; runners connect only
// through the authenticated machine WebSocket protocol.
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/machine"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

// Spawn mirrors the daemon's SpawnCommand (AgentTask + run identity).
type Spawn = machine.Spawn
type runnerCmd = machine.Command
type runnerReply = machine.Reply

// RunnerEvent is a raw event payload published by a runner.
type RunnerEvent struct {
	Seq      int             `json:"seq"`
	Type     string          `json:"type"`
	RunnerID string          `json:"-"`
	Payload  json.RawMessage `json:"-"`
}

// Gateway sends commands to runners and collects their events over NATS.
type Gateway struct {
	transport machine.ClusterTransport
	directory machine.RouteDirectory
	events    chan RunnerEvent
	onEvent   func(RunnerEvent) // optional sink (e.g. event-log persistence)
	mu        sync.Mutex
	onRunner  map[string]struct{} // runners seen via register
	lastSeen  map[string]time.Time
	waiters   map[string]*spawnWaiter // (runnerID,spawnID) -> exclusive delivery
	eventSeq  map[string]int64
}

type spawnWaiter struct {
	terminal chan RunnerEvent
	activity chan RunnerEvent
}

// OnEvent registers a sink invoked for every runner event (in addition to
// Events() channel delivery).
func (g *Gateway) OnEvent(fn func(RunnerEvent)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.onEvent = fn
}

func New(transport machine.ClusterTransport) *Gateway {
	if transport == nil {
		panic("runner gateway requires cluster transport")
	}
	return &Gateway{
		transport: transport,
		events:    make(chan RunnerEvent, 256),
		onRunner:  make(map[string]struct{}),
		lastSeen:  make(map[string]time.Time),
		waiters:   make(map[string]*spawnWaiter),
		eventSeq:  make(map[string]int64),
	}
}

func (g *Gateway) SetDirectory(directory machine.RouteDirectory) {
	if directory == nil {
		panic("runner gateway requires route directory")
	}
	g.mu.Lock()
	g.directory = directory
	g.mu.Unlock()
}

// Start subscribes to the neutral cluster event stream and rejects envelopes
// from expired or superseded connection leases.
func (g *Gateway) Start(ctx context.Context) error {
	g.mu.Lock()
	directory := g.directory
	g.mu.Unlock()
	if directory == nil {
		return errors.New("runner gateway route directory is not configured")
	}
	events, err := g.transport.SubscribeEvents(ctx, func(envelope machine.EventEnvelope) {
		current, err := directory.RouteIsCurrent(context.Background(), envelope.Route)
		if err != nil || !current {
			return
		}
		payload, err := json.Marshal(envelope.Event)
		if err != nil {
			return
		}
		runnerID := envelope.Route.MachineID.String()
		ev := RunnerEvent{Seq: int(envelope.Event.Seq), Type: envelope.Event.Type, RunnerID: runnerID, Payload: payload}
		var identity struct {
			SpawnID string `json:"spawnId"`
		}
		_ = json.Unmarshal(payload, &identity)
		g.mu.Lock()
		sequenceKey := fmt.Sprintf("%s\x00%s\x00%d", runnerID, envelope.Route.HolderID, envelope.Route.FencingToken)
		if envelope.Event.Seq <= g.eventSeq[sequenceKey] {
			g.mu.Unlock()
			return
		}
		g.eventSeq[sequenceKey] = envelope.Event.Seq
		g.lastSeen[runnerID] = time.Now()
		waiter := g.waiters[waiterKey(runnerID, identity.SpawnID)]
		onEvent := g.onEvent
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
		if onEvent != nil {
			onEvent(ev)
		}
		select {
		case g.events <- ev:
		default: // drop if consumer is too slow
		}
	})
	if err != nil {
		return fmt.Errorf("subscribe events: %w", err)
	}
	go func() {
		<-ctx.Done()
		_ = events.Close()
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
	// Deterministic ordering: run launch falls back to reg[0] when no runner id
	// is supplied, and a random map order would pick an arbitrary (possibly
	// stale) daemon. Sort so the choice is stable across launches.
	sort.Strings(out)
	return out
}

func (g *Gateway) RegisteredRunnersContext(ctx context.Context) []string {
	g.mu.Lock()
	directory := g.directory
	g.mu.Unlock()
	if directory == nil {
		return nil
	}
	routes, err := directory.ListActiveRoutes(ctx)
	if err != nil {
		return nil
	}
	result := make([]string, 0, len(routes))
	for _, route := range routes {
		result = append(result, route.MachineID.String())
	}
	sort.Strings(result)
	return result
}

// Events exposes the runner event stream (type + raw payload).
func (g *Gateway) Events() <-chan RunnerEvent { return g.events }

// Spawn sends a spawn command to a runner and waits for its reply.
func (g *Gateway) Spawn(ctx context.Context, runnerID string, sp Spawn) error {
	cmd, err := json.Marshal(runnerCmd{Type: "spawn", Spawn: &sp})
	if err != nil {
		return err
	}
	if err := g.roundTrip(ctx, runnerID, cmd); err != nil {
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
				Error    string `json:"error"`
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
				return SpawnResult{OK: ok, ExitCode: p.ExitCode, Error: p.Error, Preview: p.Preview}, nil
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

// MarkRunner registers a runner id so engine runner discovery sees it. Used by
// the WS machine hub when a daemon connects: the runner registers over the
// machine WS bridge, not the NATS register subject, so without this the gateway
// (and therefore run launch) never discovers the connected runner.
func (g *Gateway) MarkRunner(runnerID string) {
	g.mu.Lock()
	g.onRunner[runnerID] = struct{}{}
	g.lastSeen[runnerID] = time.Now()
	g.mu.Unlock()
}

// UnmarkRunner removes a runner id from discovery. Called by the machine hub
// when a daemon disconnects so stale/zombie runners are never dispatch targets.
func (g *Gateway) UnmarkRunner(runnerID string) {
	g.mu.Lock()
	delete(g.onRunner, runnerID)
	delete(g.lastSeen, runnerID)
	g.mu.Unlock()
}

// Kill sends a kill command for a running spawn.
func (g *Gateway) Kill(ctx context.Context, runnerID, spawnID string) error {
	cmd, err := json.Marshal(runnerCmd{Type: "kill", SpawnID: spawnID})
	if err != nil {
		return err
	}
	return g.roundTrip(ctx, runnerID, cmd)
}

// SwitchProvider asks a runner to change provider for a spawn (cc switch).
func (g *Gateway) SwitchProvider(ctx context.Context, runnerID, spawnID, provider, apiKey string) error {
	cmd, err := json.Marshal(runnerCmd{Type: "switch-provider", SpawnID: spawnID, Provider: provider, APIKey: apiKey})
	if err != nil {
		return err
	}
	return g.roundTrip(ctx, runnerID, cmd)
}

func (g *Gateway) roundTrip(ctx context.Context, runnerID string, payload []byte) error {
	_, err := g.roundTripReply(ctx, runnerID, payload)
	return err
}

func (g *Gateway) roundTripReply(ctx context.Context, runnerID string, payload []byte) (runnerReply, error) {
	id, err := guid.Parse(runnerID)
	if err != nil {
		return runnerReply{}, fmt.Errorf("invalid runner id: %w", err)
	}
	g.mu.Lock()
	directory := g.directory
	g.mu.Unlock()
	if directory == nil {
		return runnerReply{}, errors.New("runner route directory is not configured")
	}
	route, err := directory.ResolveActiveRoute(ctx, id)
	if err != nil {
		return runnerReply{}, fmt.Errorf("resolve runner %s: %w", runnerID, err)
	}
	var command runnerCmd
	if err := json.Unmarshal(payload, &command); err != nil {
		return runnerReply{}, fmt.Errorf("decode runner command: %w", err)
	}
	envelope, err := g.transport.Request(ctx, route, command)
	if err != nil {
		return runnerReply{}, err
	}
	current, err := directory.RouteIsCurrent(ctx, envelope.Route)
	if err != nil {
		return runnerReply{}, fmt.Errorf("verify runner %s reply route: %w", runnerID, err)
	}
	if !current {
		return runnerReply{}, fmt.Errorf("runner %s reply was fenced", runnerID)
	}
	r := envelope.Reply
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
	r, err := g.roundTripReply(ctx, runnerID, cmd)
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
	r, err := g.roundTripReply(ctx, runnerID, cmd)
	if err != nil {
		return CmdResult{}, err
	}
	return CmdResult{ExitCode: r.Data.ExitCode, Stdout: r.Data.Stdout, Stderr: r.Data.Stderr}, nil
}
