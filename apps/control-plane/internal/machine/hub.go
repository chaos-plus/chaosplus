package machine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/gateway"
	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/store"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true }, // local dev single-user
}

// wsCommand is the control→daemon command (mirrors the daemon's RunnerCommand).
type wsCommand struct {
	Type      string         `json:"type"` // spawn|kill|switch-provider|read-file|run-cmd
	Spawn     *gateway.Spawn `json:"spawn,omitempty"`
	SpawnID   string         `json:"spawnId,omitempty"`
	Provider  string         `json:"provider,omitempty"`
	APIKey    string         `json:"apiKey,omitempty"`
	Path      string         `json:"path,omitempty"`
	Cmd       string         `json:"cmd,omitempty"`
	TimeoutMs int            `json:"timeoutMs,omitempty"`
}

// wsReply is the daemon's request/reply answer.
type wsReply struct {
	OK   bool            `json:"ok"`
	Data json.RawMessage `json:"data,omitempty"`
}

// wsEvent is a daemon→control unsolicited event (heartbeat / spawn lifecycle).
type wsEvent struct {
	Type     string `json:"type"`
	SpawnID  string `json:"spawnId,omitempty"`
	OK       *bool  `json:"ok,omitempty"`
	ExitCode int    `json:"exitCode,omitempty"`
	Message  string `json:"message,omitempty"`
}

// daemonConn is one authenticated daemon WebSocket. wmu serializes all writes:
// gorilla/websocket allows only one concurrent writer per connection.
type daemonConn struct {
	runnerID string
	ws       *websocket.Conn
	wmu      sync.Mutex
	mu       sync.Mutex
	reqs     map[int64]chan wsReply
}

func (c *daemonConn) writeJSON(v any) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return c.ws.WriteJSON(v)
}

// Hub accepts daemon WS connections (token-authenticated), maps runnerID↔conn,
// and implements the engine's RunnerLink by dispatching commands over the socket
// and correlating replies + spawn lifecycle events (PRD §5.3.1 / §17.1).
type Hub struct {
	tokens   *TokenStore
	machines *store.Store // nil-safe
	seq      atomic.Int64

	mu      sync.Mutex
	conns   map[string]*daemonConn
	pending map[string]bool // machineID connected with an unconfirmed one-time token
	names   map[string]string
	events  chan gateway.RunnerEvent
}

func NewHub(tokens *TokenStore, machines *store.Store) *Hub {
	return &Hub{
		tokens:   tokens,
		machines: machines,
		conns:    make(map[string]*daemonConn),
		pending:  make(map[string]bool),
		names:    make(map[string]string),
		events:   make(chan gateway.RunnerEvent, 256),
	}
}

// HandleWS authenticates the ?token= and serves the daemon connection.
func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}
	at, err := h.tokens.Validate(token)
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &daemonConn{runnerID: at.MachineID, ws: conn, reqs: make(map[int64]chan wsReply)}
	h.mu.Lock()
	h.conns[at.MachineID] = c
	if !at.LongTerm {
		h.pending[at.MachineID] = true
	}
	h.mu.Unlock()
	_ = c.writeJSON(map[string]any{"type": "ready"})
	h.serve(c)
}

func (h *Hub) serve(c *daemonConn) {
	defer h.unregister(c)
	for {
		var m struct {
			Type  string            `json:"type"`
			ReqID int64             `json:"reqId,omitempty"`
			OK    *bool             `json:"ok,omitempty"`
			Data  json.RawMessage   `json:"data,omitempty"`
			Event *wsEvent          `json:"event,omitempty"`
			Meta  map[string]string `json:"meta,omitempty"`
		}
		if err := c.ws.ReadJSON(&m); err != nil {
			return
		}
		switch m.Type {
		case "reply":
			if m.ReqID != 0 {
				c.mu.Lock()
				ch := c.reqs[m.ReqID]
				c.mu.Unlock()
				if ch != nil {
					ch <- wsReply{OK: m.OK != nil && *m.OK, Data: m.Data}
				}
			}
		case "event":
			if m.Event != nil {
				h.handleEvent(c.runnerID, m.Event)
			}
		case "register":
			h.mu.Lock()
			h.names[c.runnerID] = m.Meta["name"]
			h.mu.Unlock()
		}
	}
}

func (h *Hub) handleEvent(runnerID string, ev *wsEvent) {
	raw, _ := json.Marshal(ev)
	ge := gateway.RunnerEvent{Type: ev.Type, RunnerID: runnerID, Payload: raw}
	if ev.Type == "heartbeat" && h.machines != nil {
		_ = h.machines.TouchMachineHeartbeat(context.Background(), runnerID)
	}
	select {
	case h.events <- ge:
	default: // drop if the engine isn't consuming fast enough
	}
}

func (h *Hub) unregister(c *daemonConn) {
	h.mu.Lock()
	if h.conns[c.runnerID] == c {
		delete(h.conns, c.runnerID)
	}
	wasPending := h.pending[c.runnerID]
	if wasPending {
		delete(h.pending, c.runnerID)
		// §5.3.1: an unconfirmed machine that disconnects loses its token. Call
		// under h.mu so no race window where a client sees the conn gone but the
		// token still valid (tokens.mu is a separate lock; order is always h.mu→tokens.mu).
		h.tokens.Invalidate(c.runnerID)
	}
	h.mu.Unlock()
	_ = c.ws.Close()
}

// ---- RunnerLink implementation ----

func (h *Hub) RegisteredRunners() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, len(h.conns))
	for id := range h.conns {
		out = append(out, id)
	}
	return out
}

// IsConnected reports whether a machine currently holds a live connection.
func (h *Hub) IsConnected(machineID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.conns[machineID]
	return ok
}

// ListMachines returns confirmed machines from the store (empty when none).
func (h *Hub) ListMachines(ctx context.Context) ([]store.Machine, error) {
	if h.machines == nil {
		return nil, nil
	}
	return h.machines.ListMachines(ctx)
}

// Confirm promotes a one-time token to long-term and persists the machine
// (PRD §5.3.1 step 6). Idempotent on repeat confirm of the same token.
func (h *Hub) Confirm(ctx context.Context, machineID, token, address string) error {
	if err := h.tokens.MakeLongTerm(machineID, token); err != nil {
		return err
	}
	if h.machines != nil {
		return h.machines.UpsertMachine(ctx, store.Machine{
			ID: machineID, InstanceID: "desktop", Address: address,
			Status: "confirmed", TokenHash: hashToken(token),
		})
	}
	return nil
}

// Disconnect force-closes a machine's connection (cancel / force-offline).
// unregister handles cleanup + pending-token invalidation.
func (h *Hub) Disconnect(machineID string) {
	h.mu.Lock()
	c := h.conns[machineID]
	h.mu.Unlock()
	if c != nil {
		_ = c.ws.Close()
	}
}

// IssueToken mints a fresh machine id + one-time onboarding token.
func (h *Hub) IssueToken() (machineID, token string) {
	machineID = newMachineID()
	at := h.tokens.Issue(machineID)
	return machineID, at.Token
}

// RefreshToken invalidates the machine's old tokens and issues a new one
// (§5.3.1 "刷新命令").
func (h *Hub) RefreshToken(machineID string) (string, error) {
	h.tokens.Invalidate(machineID)
	return h.tokens.Issue(machineID).Token, nil
}

func newMachineID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return "m-" + hex.EncodeToString(b)
}

// Cancel revokes a machine's tokens and disconnects it (cancel / force-offline).
func (h *Hub) Cancel(machineID string) {
	h.tokens.Invalidate(machineID)
	h.Disconnect(machineID)
	if h.machines != nil {
		_ = h.machines.DeleteMachine(context.Background(), machineID)
	}
}

// MachineName returns the name a connected daemon registered (if any).
func (h *Hub) MachineName(runnerID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.names[runnerID]
}

// Events exposes the daemon event stream (heartbeat + spawn lifecycle).
func (h *Hub) Events() <-chan gateway.RunnerEvent { return h.events }

// doRequest sends a command and waits for the correlated reply.
func (h *Hub) doRequest(ctx context.Context, c *daemonConn, cmd wsCommand) (wsReply, error) {
	reqID := h.seq.Add(1)
	ch := make(chan wsReply, 1)
	c.mu.Lock()
	c.reqs[reqID] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.reqs, reqID)
		c.mu.Unlock()
	}()

	if err := c.writeJSON(map[string]any{"type": "cmd", "reqId": reqID, "cmd": cmd}); err != nil {
		return wsReply{}, fmt.Errorf("send to %s: %w", c.runnerID, err)
	}
	select {
	case r := <-ch:
		if !r.OK {
			var d struct {
				Error string `json:"error"`
			}
			_ = json.Unmarshal(r.Data, &d)
			return r, fmt.Errorf("daemon %s: %s", c.runnerID, d.Error)
		}
		return r, nil
	case <-ctx.Done():
		return wsReply{}, ctx.Err()
	}
}

// SpawnAndWait spawns and blocks until spawn-done/error or timeout (idle resets
// on live activity, max is absolute) — mirrors gateway.SpawnAndWaitOpts over WS.
func (h *Hub) SpawnAndWait(ctx context.Context, runnerID string, sp gateway.Spawn, idle, max time.Duration) (gateway.SpawnResult, error) {
	c := h.connFor(runnerID)
	if c == nil {
		return gateway.SpawnResult{}, fmt.Errorf("runner %s not connected", runnerID)
	}
	if _, err := h.doRequest(ctx, c, wsCommand{Type: "spawn", Spawn: &sp}); err != nil {
		return gateway.SpawnResult{}, err
	}

	maxCtx := ctx
	cancelMax := func() {}
	if max > 0 {
		maxCtx, cancelMax = context.WithTimeout(ctx, max)
	}
	defer cancelMax()

	var idleTimer *time.Timer
	idleC := make(chan time.Time, 1)
	if idle > 0 {
		idleTimer = time.AfterFunc(idle, func() { idleC <- time.Time{} })
	}
	resetIdle := func() {
		if idleTimer != nil {
			idleTimer.Reset(idle)
		}
	}

	for {
		select {
		case ev := <-h.events:
			if ev.RunnerID != runnerID {
				continue
			}
			var p wsEvent
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				continue
			}
			if p.SpawnID != sp.SpawnID {
				continue
			}
			resetIdle()
			switch ev.Type {
			case "spawn-done":
				ok := p.OK == nil || *p.OK
				return gateway.SpawnResult{OK: ok, ExitCode: p.ExitCode}, nil
			case "spawn-error":
				return gateway.SpawnResult{OK: false, Error: p.Message}, nil
			}
		case <-idleC:
			return gateway.SpawnResult{}, fmt.Errorf("spawn %s idle timeout after %s", sp.SpawnID, idle)
		case <-maxCtx.Done():
			return gateway.SpawnResult{}, maxCtx.Err()
		}
	}
}

func (h *Hub) Kill(ctx context.Context, runnerID, spawnID string) error {
	c := h.connFor(runnerID)
	if c == nil {
		return fmt.Errorf("runner %s not connected", runnerID)
	}
	_, err := h.doRequest(ctx, c, wsCommand{Type: "kill", SpawnID: spawnID})
	return err
}

func (h *Hub) ReadArtifact(ctx context.Context, runnerID, spawnID, path string) ([]byte, error) {
	c := h.connFor(runnerID)
	if c == nil {
		return nil, fmt.Errorf("runner %s not connected", runnerID)
	}
	r, err := h.doRequest(ctx, c, wsCommand{Type: "read-file", SpawnID: spawnID, Path: path})
	if err != nil {
		return nil, err
	}
	var d struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(r.Data, &d); err != nil {
		return nil, err
	}
	return []byte(d.Content), nil
}

func (h *Hub) RunCmd(ctx context.Context, runnerID, spawnID, cmdTemplate string, timeoutMs int) (gateway.CmdResult, error) {
	c := h.connFor(runnerID)
	if c == nil {
		return gateway.CmdResult{}, fmt.Errorf("runner %s not connected", runnerID)
	}
	r, err := h.doRequest(ctx, c, wsCommand{Type: "run-cmd", SpawnID: spawnID, Cmd: cmdTemplate, TimeoutMs: timeoutMs})
	if err != nil {
		return gateway.CmdResult{}, err
	}
	var d struct {
		ExitCode int    `json:"exitCode"`
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
	}
	if err := json.Unmarshal(r.Data, &d); err != nil {
		return gateway.CmdResult{}, err
	}
	return gateway.CmdResult{ExitCode: d.ExitCode, Stdout: d.Stdout, Stderr: d.Stderr}, nil
}

func (h *Hub) connFor(runnerID string) *daemonConn {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.conns[runnerID]
}
