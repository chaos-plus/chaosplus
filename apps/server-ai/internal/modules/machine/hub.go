package machine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nats-io/nats.go"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/infra/runnergateway"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/secure"
	coreid "github.com/chaos-plus/chaosplus/internal/infra/guid"
)

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
	machineID coreid.ID
	addr      string // daemon WS 握手来源地址(机器真实地址)
	claims    authn.Claims
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
// NATS runner subjects, so ANY server-ai instance (a NATS client) can reach
// the daemon — the daemon itself never touches NATS.
//
//	instance(gateway) --NATS request chaos.runner.{id}.cmd-->  hub --WS cmd--> daemon
//	daemon --WS reply/event-->  hub --NATS reply/chaos.runner.{id}.evt-->  gateway
type Hub struct {
	nc       *nats.Conn
	tokens   *TokenStore
	machines Repository // nil-safe
	nextID   IDGenerator
	origin   secure.OriginPolicy
	seq      atomic.Int64

	mu            sync.Mutex
	conns         map[coreid.ID]*daemonConn
	subs          map[coreid.ID]*nats.Subscription // machineID -> its chaos.runner.{id}.cmd sub
	pending       map[coreid.ID]bool               // machineID connected with an unconfirmed one-time token
	pendingScopes map[coreid.ID]authn.Claims       // machineID -> trusted issuing scope during onboarding
	names         map[coreid.ID]string
	// runtimes 是每台机上报的可用执行器(claude/codex/...),数字人表单据此给下拉。
	runtimes map[coreid.ID][]string
	oses     map[coreid.ID]string // 注册时上报的 OS/架构
}

type IDGenerator func() (coreid.ID, error)

func NewHub(nc *nats.Conn, tokens *TokenStore, machines Repository, nextID IDGenerator, origin secure.OriginPolicy) *Hub {
	if nc == nil || tokens == nil || nextID == nil {
		panic("machine hub requires NATS, token store, and id generator")
	}
	return &Hub{
		nc:            nc,
		tokens:        tokens,
		machines:      machines,
		nextID:        nextID,
		origin:        origin,
		conns:         make(map[coreid.ID]*daemonConn),
		subs:          make(map[coreid.ID]*nats.Subscription),
		pending:       make(map[coreid.ID]bool),
		pendingScopes: make(map[coreid.ID]authn.Claims),
		names:         make(map[coreid.ID]string),
		runtimes:      make(map[coreid.ID][]string),
		oses:          make(map[coreid.ID]string),
	}
}

func (h *Hub) SetRepository(repository Repository) { h.machines = repository }

const cmdSubjectFmt = "chaos.runner.%s.cmd"
const evtSubjectFmt = "chaos.runner.%s.evt"
const registerSubject = "chaos.runner.register"

var ErrMachineNotConnected = errors.New("machine has not connected with the onboarding token")
var ErrMachineNotFound = errors.New("machine not found")

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
	upgrader := websocket.Upgrader{CheckOrigin: h.origin.Allows}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &daemonConn{
		machineID: at.MachineID, addr: r.RemoteAddr, ws: conn,
		claims: authn.Claims{TenantID: at.TenantID, EntityID: at.EntityID, PrincipalID: at.OwnerID},
		reqs:   make(map[int64]*pendingReq),
	}

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
	if h.machines != nil {
		// 记录 daemon 实际连接地址(来自 WS 握手,不是浏览器 confirm 的地址)。
		// 未确认的机器还没落库,更新是 no-op,confirm 时再从连接里取。
		ctx := authn.WithClaims(r.Context(), &c.claims)
		_ = h.machines.UpdateAddress(ctx, at.MachineID, r.RemoteAddr)
	}
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
				if err := pr.respond(body); err != nil {
					slog.Debug("respond to runner request", "machine", c.machineID, "err", err)
				}
			}
		case "event":
			if m.Event != nil {
				m.Event.Seq = c.evtSeq.Add(1)
				h.bridgeEvent(c, m.Event)
			}
		case "register":
			h.mu.Lock()
			h.names[c.machineID] = m.Meta["name"]
			if rt := m.Meta["runtimes"]; rt != "" {
				h.runtimes[c.machineID] = strings.Split(rt, ",")
			}
			if osv := m.Meta["os"]; osv != "" {
				h.oses[c.machineID] = osv
			}
			h.mu.Unlock()
			// Let the gateway's register subscription see this runner too.
			reg, _ := json.Marshal(map[string]any{"runnerId": c.machineID, "meta": m.Meta})
			_ = h.nc.Publish(registerSubject, reg)
		}
	}
}

func (h *Hub) bridgeEvent(conn *daemonConn, ev *wsEvent) {
	raw, _ := json.Marshal(ev)
	if ev.Type == "heartbeat" && h.machines != nil {
		ctx := authn.WithClaims(context.Background(), &conn.claims)
		_ = h.machines.TouchHeartbeat(ctx, conn.machineID)
	}
	_ = h.nc.Publish(fmt.Sprintf(evtSubjectFmt, conn.machineID.String()), raw)
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
		out = append(out, id.String())
	}
	return out
}

func (h *Hub) IsConnected(machineID coreid.ID) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.conns[machineID]
	return ok
}

// MachineRuntimes 返回该机注册时上报的可用执行器列表。
func (h *Hub) MachineRuntimes(runnerID coreid.ID) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string{}, h.runtimes[runnerID]...)
}

// MachineOS 返回该机注册时上报的操作系统/架构。
func (h *Hub) MachineOS(runnerID coreid.ID) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.oses[runnerID]
}

func (h *Hub) MachineName(runnerID coreid.ID) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.names[runnerID]
}

func (h *Hub) ListMachines(ctx context.Context) ([]Machine, error) {
	if h.machines == nil {
		return nil, nil
	}
	return h.machines.List(ctx)
}

func (h *Hub) Confirm(ctx context.Context, machineID coreid.ID, token string) error {
	if !h.CanAccess(ctx, machineID) {
		return ErrMachineNotFound
	}
	// A confirmed machine is no longer pending — otherwise its disconnect would
	// be misread as an unconfirmed-abandon and invalidate the long-term token.
	// 地址取 daemon 实际连接来源(HandleWS 握手时记录),daemon 未连接则为空。
	h.mu.Lock()
	if h.conns[machineID] == nil || !h.pending[machineID] {
		h.mu.Unlock()
		return ErrMachineNotConnected
	}
	if err := h.tokens.MakeLongTerm(machineID, token); err != nil {
		h.mu.Unlock()
		return err
	}
	delete(h.pending, machineID)
	address := h.conns[machineID].addr
	h.mu.Unlock()
	if h.machines != nil {
		claims, err := requireClaims(ctx)
		if err != nil {
			return err
		}
		if err := h.machines.Upsert(ctx, Machine{
			ID: machineID, TenantID: claims.TenantID, EntityID: claims.EntityID, OwnerID: claims.PrincipalID, Address: address,
			Status: StatusConfirmed, TokenHash: hashToken(token),
			OS: h.MachineOS(machineID),
		}); err != nil {
			return err
		}
	}
	h.mu.Lock()
	delete(h.pendingScopes, machineID)
	h.mu.Unlock()
	return nil
}

func (h *Hub) Disconnect(machineID coreid.ID) {
	h.mu.Lock()
	c := h.conns[machineID]
	h.mu.Unlock()
	if c != nil {
		_ = c.ws.Close()
	}
}

func (h *Hub) Close() error {
	h.mu.Lock()
	connections := make([]*websocket.Conn, 0, len(h.conns))
	subscriptions := make([]*nats.Subscription, 0, len(h.subs))
	for _, connection := range h.conns {
		connections = append(connections, connection.ws)
	}
	for _, subscription := range h.subs {
		subscriptions = append(subscriptions, subscription)
	}
	h.conns = make(map[coreid.ID]*daemonConn)
	h.subs = make(map[coreid.ID]*nats.Subscription)
	h.mu.Unlock()
	var failures []error
	for _, subscription := range subscriptions {
		if err := subscription.Unsubscribe(); err != nil {
			failures = append(failures, err)
		}
	}
	for _, connection := range connections {
		if err := connection.Close(); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// LoadTokens 启动时回灌 DB 里已确认机器的长期 token hash,否则控制面重启后
// daemon 用旧 token 重连会因内存 store 为空而失败。
func (h *Hub) LoadTokens(ctx context.Context) error {
	if h.machines == nil {
		return nil
	}
	machines, err := h.machines.ListAll(ctx)
	if err != nil {
		return err
	}
	for _, m := range machines {
		if m.TokenHash != "" {
			h.tokens.Rehydrate(m.ID, m.TenantID, m.EntityID, m.OwnerID, m.TokenHash)
		}
	}
	return nil
}

// GetToken 只读返回当前 token(构建接入命令),不轮换不踢守护进程。
func (h *Hub) GetToken(machineID coreid.ID) (string, error) {
	return h.tokens.GetToken(machineID)
}

// IssueTokenFor binds an onboarding flow to the caller's instance before the
// machine is persisted. This prevents another tenant from cancelling or
// claiming a pending machine by ID.
func (h *Hub) IssueTokenFor(ctx context.Context) (machineID coreid.ID, token string, expiresAt time.Time, err error) {
	claims, err := requireClaims(ctx)
	if err != nil {
		return 0, "", time.Time{}, err
	}
	machineID, err = h.nextID()
	if err != nil {
		return 0, "", time.Time{}, fmt.Errorf("generate machine id: %w", err)
	}
	at := h.tokens.Issue(machineID, claims.TenantID, claims.EntityID, claims.PrincipalID)
	h.mu.Lock()
	h.pendingScopes[machineID] = *claims
	h.mu.Unlock()
	return machineID, at.Token, at.ExpiresAt, nil
}

// CanAccess applies the same tenant visibility rule to pending in-memory
// onboarding records and confirmed machines persisted in StateStore.
func (h *Hub) CanAccess(ctx context.Context, machineID coreid.ID) bool {
	h.mu.Lock()
	scope, pending := h.pendingScopes[machineID]
	h.mu.Unlock()
	if pending {
		claims, err := requireClaims(ctx)
		return err == nil && scope.TenantID == claims.TenantID && scope.EntityID == claims.EntityID && scope.PrincipalID == claims.PrincipalID
	}
	if h.machines == nil {
		return authn.EntityIDFromContext(ctx).Zero()
	}
	machines, err := h.machines.List(ctx)
	if err != nil {
		return false
	}
	for _, item := range machines {
		if item.ID == machineID {
			return true
		}
	}
	return false
}

// OnboardingStatus returns the authoritative state for the 300-second
// onboarding window. The raw token is accepted in the JSON request body by the
// HTTP layer so it does not leak into access logs or browser history.
func (h *Hub) OnboardingStatus(machineID coreid.ID, token string) (state string, expiresAt time.Time) {
	at, err := h.tokens.Validate(token)
	if errors.Is(err, ErrTokenExpired) {
		return "expired", time.Time{}
	}
	if err != nil || at.MachineID != machineID {
		return "invalid", time.Time{}
	}
	if at.LongTerm {
		return "confirmed", time.Time{}
	}
	h.mu.Lock()
	connected := h.conns[machineID] != nil && h.pending[machineID]
	h.mu.Unlock()
	if connected {
		return "connected", at.ExpiresAt
	}
	return "waiting", at.ExpiresAt
}

func (h *Hub) RefreshToken(ctx context.Context, machineID coreid.ID) (string, error) {
	// 长期 token 仅手动轮换:失效旧的,签发新的长期 token 并落库(§5.3.1)。
	// 必须先确认机器存在 —— 否则任意 id 都能凭空换到一个可用的长期令牌。
	if h.machines != nil {
		known, err := h.machines.List(ctx)
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
	h.Disconnect(machineID) // 轮换后旧 daemon 的 token 立即失效,踢下线等新命令重连
	claims, err := requireClaims(ctx)
	if err != nil {
		return "", err
	}
	at := h.tokens.IssueLongTerm(machineID, claims.TenantID, claims.EntityID, claims.PrincipalID)
	if h.machines != nil {
		if err := h.machines.UpdateToken(ctx, machineID, hashToken(at.Token)); err != nil {
			return "", err
		}
	}
	return at.Token, nil
}

func (h *Hub) Cancel(ctx context.Context, machineID coreid.ID) error {
	if !h.CanAccess(ctx, machineID) {
		return ErrMachineNotFound
	}
	h.tokens.Invalidate(machineID)
	h.Disconnect(machineID)
	h.mu.Lock()
	delete(h.pendingScopes, machineID)
	h.mu.Unlock()
	if h.machines != nil {
		if err := h.machines.Delete(ctx, machineID); err != nil {
			return err
		}
	}
	return nil
}
