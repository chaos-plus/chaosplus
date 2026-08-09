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

	"github.com/gorilla/websocket"
	"github.com/nats-io/nats.go"

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

// wsEvent is a daemon→control unsolicited event (heartbeat / spawn lifecycle).
type wsEvent struct {
	// Seq 由桥接方按连接递增。daemon 不带序号,而下游用 (runner,type,seq)
	// 做事件落库的幂等键 —— 恒为 0 会让同类事件只存下第一条(§15.1 被破坏)。
	Seq      int64           `json:"seq,omitempty"`
	Type     string          `json:"type"`
	SpawnID  string          `json:"spawnId,omitempty"`
	OK       *bool           `json:"ok,omitempty"`
	ExitCode int             `json:"exitCode,omitempty"`
	CostUSD  *float64        `json:"costUsd,omitempty"` // agent 上报的本次花费(仪表盘汇总)
	Message  string          `json:"message,omitempty"`
	Event    json.RawMessage `json:"event,omitempty"` // AgentEvent (message/tool) content, kept for progress
}

// pendingReq pairs a forwarded command's reqId with the NATS request to answer.
type pendingReq struct {
	respond func(data []byte) error
}

// daemonConn is one authenticated daemon WebSocket. wmu serializes all writes:
// gorilla/websocket allows only one concurrent writer per connection.
type daemonConn struct {
	machineID string
	ws        *websocket.Conn
	wmu       sync.Mutex
	mu        sync.Mutex
	reqs      map[int64]*pendingReq
	evtSeq    atomic.Int64 // 事件序号,保证下游幂等键唯一
}

func (c *daemonConn) writeJSON(v any) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return c.ws.WriteJSON(v)
}

// Hub is a NATS↔WebSocket bridge for machine runners (PRD §5.3.1). A daemon
// holds one authenticated WS to /api/machines/ws; the hub bridges it onto the
// NATS runner subjects, so ANY control-plane instance (a NATS client) can reach
// the daemon — the daemon itself never touches NATS.
//
//	instance(gateway) --NATS request chaos.runner.{id}.cmd-->  hub --WS cmd--> daemon
//	daemon --WS reply/event-->  hub --NATS reply/chaos.runner.{id}.evt-->  gateway
type Hub struct {
	nc       *nats.Conn
	tokens   *TokenStore
	machines *store.Store // nil-safe
	seq      atomic.Int64

	mu      sync.Mutex
	conns   map[string]*daemonConn
	subs    map[string]*nats.Subscription // machineID -> its chaos.runner.{id}.cmd sub
	pending map[string]bool               // machineID connected with an unconfirmed one-time token
	names   map[string]string
}

func NewHub(nc *nats.Conn, tokens *TokenStore, machines *store.Store) *Hub {
	return &Hub{
		nc:       nc,
		tokens:   tokens,
		machines: machines,
		conns:    make(map[string]*daemonConn),
		subs:     make(map[string]*nats.Subscription),
		pending:  make(map[string]bool),
		names:    make(map[string]string),
	}
}

const cmdSubjectFmt = "chaos.runner.%s.cmd"
const evtSubjectFmt = "chaos.runner.%s.evt"
const registerSubject = "chaos.runner.register"

// HandleWS authenticates the ?token= and bridges the daemon onto NATS.
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
	c := &daemonConn{machineID: at.MachineID, ws: conn, reqs: make(map[int64]*pendingReq)}

	// Bridge: subscribe this machine's command subject, forward each request over WS.
	sub, err := h.nc.Subscribe(fmt.Sprintf(cmdSubjectFmt, at.MachineID), func(m *nats.Msg) {
		var cmd wsCommand
		if err := json.Unmarshal(m.Data, &cmd); err != nil {
			_ = m.Respond([]byte(`{"ok":false,"data":{"error":"bad command"}}`))
			return
		}
		reqID := h.seq.Add(1)
		c.mu.Lock()
		c.reqs[reqID] = &pendingReq{respond: m.Respond}
		c.mu.Unlock()
		if err := c.writeJSON(map[string]any{"type": "cmd", "reqId": reqID, "cmd": cmd}); err != nil {
			c.mu.Lock()
			delete(c.reqs, reqID)
			c.mu.Unlock()
			_ = m.Respond([]byte(`{"ok":false,"data":{"error":"daemon disconnected"}}`))
		}
	})
	if err != nil {
		_ = conn.Close()
		return
	}

	h.mu.Lock()
	h.conns[at.MachineID] = c
	h.subs[at.MachineID] = sub
	if !at.LongTerm {
		h.pending[at.MachineID] = true
	}
	h.mu.Unlock()
	_ = c.writeJSON(map[string]any{"type": "ready"})
	h.serve(c, sub)
}

func (h *Hub) serve(c *daemonConn, sub *nats.Subscription) {
	defer h.unregister(c, sub)
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
			if m.ReqID == 0 {
				continue
			}
			c.mu.Lock()
			pr := c.reqs[m.ReqID]
			delete(c.reqs, m.ReqID)
			c.mu.Unlock()
			if pr != nil {
				ok := m.OK != nil && *m.OK
				body, _ := json.Marshal(map[string]any{"ok": ok, "data": json.RawMessage(m.Data)})
				pr.respond(body)
			}
		case "event":
			if m.Event != nil {
				m.Event.Seq = c.evtSeq.Add(1)
				h.bridgeEvent(c.machineID, m.Event)
			}
		case "register":
			h.mu.Lock()
			h.names[c.machineID] = m.Meta["name"]
			h.mu.Unlock()
			// Let the gateway's register subscription see this runner too.
			reg, _ := json.Marshal(map[string]any{"runnerId": c.machineID, "meta": m.Meta})
			_ = h.nc.Publish(registerSubject, reg)
		}
	}
}

func (h *Hub) bridgeEvent(machineID string, ev *wsEvent) {
	raw, _ := json.Marshal(ev)
	if ev.Type == "heartbeat" && h.machines != nil {
		_ = h.machines.TouchMachineHeartbeat(context.Background(), machineID)
	}
	_ = h.nc.Publish(fmt.Sprintf(evtSubjectFmt, machineID), raw)
}

func (h *Hub) unregister(c *daemonConn, sub *nats.Subscription) {
	_ = sub.Unsubscribe()
	h.mu.Lock()
	if h.conns[c.machineID] == c {
		delete(h.conns, c.machineID)
	}
	delete(h.subs, c.machineID)
	wasPending := h.pending[c.machineID]
	if wasPending {
		delete(h.pending, c.machineID)
		// §5.3.1: an unconfirmed machine that disconnects loses its token. Call
		// under h.mu so no race window (tokens.mu is separate; order is h.mu→tokens.mu).
		h.tokens.Invalidate(c.machineID)
	}
	h.mu.Unlock()
	_ = c.ws.Close()
}

// ---- machine onboarding surface (unchanged API) ----

func (h *Hub) RegisteredRunners() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, len(h.conns))
	for id := range h.conns {
		out = append(out, id)
	}
	return out
}

func (h *Hub) IsConnected(machineID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.conns[machineID]
	return ok
}

func (h *Hub) MachineName(runnerID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.names[runnerID]
}

func (h *Hub) ListMachines(ctx context.Context) ([]store.Machine, error) {
	if h.machines == nil {
		return nil, nil
	}
	return h.machines.ListMachines(ctx)
}

func (h *Hub) Confirm(ctx context.Context, machineID, token, address string) error {
	if err := h.tokens.MakeLongTerm(machineID, token); err != nil {
		return err
	}
	// A confirmed machine is no longer pending — otherwise its disconnect would
	// be misread as an unconfirmed-abandon and invalidate the long-term token.
	h.mu.Lock()
	delete(h.pending, machineID)
	h.mu.Unlock()
	if h.machines != nil {
		return h.machines.UpsertMachine(ctx, store.Machine{
			ID: machineID, InstanceID: "desktop", Address: address,
			Status: "confirmed", TokenHash: hashToken(token),
		})
	}
	return nil
}

func (h *Hub) Disconnect(machineID string) {
	h.mu.Lock()
	c := h.conns[machineID]
	h.mu.Unlock()
	if c != nil {
		_ = c.ws.Close()
	}
}

func (h *Hub) IssueToken() (machineID, token string) {
	machineID = newMachineID()
	at := h.tokens.Issue(machineID)
	return machineID, at.Token
}

func (h *Hub) RefreshToken(machineID string) (string, error) {
	// 长期 token 仅手动轮换:失效旧的,签发新的长期 token 并落库(§5.3.1)。
	// 必须先确认机器存在 —— 否则任意 id 都能凭空换到一个可用的长期令牌。
	if h.machines != nil {
		known, err := h.machines.ListMachines(context.Background())
		if err != nil {
			return "", err
		}
		found := false
		for _, m := range known {
			if m.ID == machineID {
				found = true
				break
			}
		}
		if !found {
			return "", fmt.Errorf("machine %s not found", machineID)
		}
	}
	h.tokens.Invalidate(machineID)
	at := h.tokens.IssueLongTerm(machineID)
	if h.machines != nil {
		if err := h.machines.UpdateMachineToken(context.Background(), machineID, hashToken(at.Token)); err != nil {
			return "", err
		}
	}
	return at.Token, nil
}

func (h *Hub) Cancel(machineID string) {
	h.tokens.Invalidate(machineID)
	h.Disconnect(machineID)
	if h.machines != nil {
		_ = h.machines.DeleteMachine(context.Background(), machineID)
	}
}

func newMachineID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return "m-" + hex.EncodeToString(b)
}
