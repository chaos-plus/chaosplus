package machine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/secure"
	coreid "github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/gorilla/websocket"
)

// pendingReq pairs a forwarded command's reqId with the cluster request to answer.
type pendingReq struct {
	respond ReplyFunc
}

// daemonConn is one authenticated daemon WebSocket. wmu serializes all writes:
// gorilla/websocket allows only one concurrent writer per connection.
type daemonConn struct {
	machineID coreid.ID
	addr      string // daemon WS 握手来源地址(机器真实地址)
	claims    authn.Claims
	route     RouteLease
	ws        *websocket.Conn
	wmu       sync.Mutex
	mu        sync.Mutex
	reqs      map[int64]*pendingReq
	evtSeq    atomic.Int64 // 事件序号,保证下游幂等键唯一
}

func (c *daemonConn) routeLease() RouteLease {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.route
}

func (c *daemonConn) setRouteLease(route RouteLease) {
	c.mu.Lock()
	c.route = route
	c.mu.Unlock()
}

func (c *daemonConn) writeJSON(v any) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return c.ws.WriteJSON(v)
}

// Hub terminates the authenticated machine WebSocket protocol (PRD §5.3.1).
// A provider-neutral ClusterTransport bridges commands and events between
// control-plane instances; runners only see this authenticated WebSocket.
type Hub struct {
	transport ClusterTransport
	tokens    *TokenStore
	machines  Repository // nil-safe
	nextID    IDGenerator
	holderID  coreid.ID
	origin    secure.OriginPolicy
	seq       atomic.Int64

	mu            sync.Mutex
	conns         map[coreid.ID]*daemonConn
	subs          map[coreid.ID]io.Closer
	pending       map[coreid.ID]bool         // machineID connected with an unconfirmed one-time token
	pendingScopes map[coreid.ID]authn.Claims // machineID -> trusted issuing scope during onboarding
	names         map[coreid.ID]string
	// runtimes 是每台机上报的可用执行器(claude/codex/...),数字人表单据此给下拉。
	runtimes map[coreid.ID][]string
	oses     map[coreid.ID]string // 注册时上报的 OS/架构

	// markRunner/unmarkRunner are optional hooks (set by the composition root)
	// that tell the run gateway when a daemon connects/disconnects, so run
	// launch can discover live WS-only runners and never dispatch to zombies.
	// Nil means no gateway integration.
	markRunner   func(string)
	unmarkRunner func(string)
}

// SetRunnerMarker wires the run gateway so connected daemons become visible to
// runner discovery and disconnected daemons are removed. Called once by the
// composition root.
func (h *Hub) SetRunnerMarker(mark, unmark func(string)) {
	h.mu.Lock()
	h.markRunner = mark
	h.unmarkRunner = unmark
	h.mu.Unlock()
}

type IDGenerator func() (coreid.ID, error)

const ConnectionLeaseTTL = 15 * time.Second

func NewHub(transport ClusterTransport, tokens *TokenStore, machines Repository, nextID IDGenerator, holderID coreid.ID, origin secure.OriginPolicy) *Hub {
	if transport == nil || tokens == nil || nextID == nil {
		panic("machine hub requires cluster transport, token store, and id generator")
	}
	return &Hub{
		transport:     transport,
		tokens:        tokens,
		machines:      machines,
		nextID:        nextID,
		holderID:      holderID,
		origin:        origin,
		conns:         make(map[coreid.ID]*daemonConn),
		subs:          make(map[coreid.ID]io.Closer),
		pending:       make(map[coreid.ID]bool),
		pendingScopes: make(map[coreid.ID]authn.Claims),
		names:         make(map[coreid.ID]string),
		runtimes:      make(map[coreid.ID][]string),
		oses:          make(map[coreid.ID]string),
	}
}

func (h *Hub) SetRepository(repository Repository) { h.machines = repository }

func (h *Hub) Start(ctx context.Context) error {
	h.mu.Lock()
	if h.holderID.Zero() {
		holderID, err := h.nextID()
		if err != nil {
			h.mu.Unlock()
			return fmt.Errorf("generate machine route holder id: %w", err)
		}
		h.holderID = holderID
	}
	h.mu.Unlock()
	return h.LoadTokens(ctx)
}

func (h *Hub) validateToken(ctx context.Context, token string) (*AccessToken, error) {
	if h.machines == nil {
		return h.tokens.Validate(token)
	}
	value, err := h.machines.FindToken(ctx, hashToken(token))
	if err != nil {
		return nil, err
	}
	value.Token = token
	return &value, nil
}

var ErrMachineNotConnected = errors.New("machine has not connected with the onboarding token")
var ErrMachineNotFound = errors.New("machine not found")

// HandleWS authenticates the ?token= and bridges the daemon onto the cluster transport.
func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}
	at, err := h.validateToken(r.Context(), token)
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	claims := authn.Claims{TenantID: at.TenantID, EntityID: at.EntityID, PrincipalID: at.OwnerID}
	leaseContext := authn.WithClaims(r.Context(), &claims)
	route := RouteLease{MachineID: at.MachineID, TenantID: at.TenantID, EntityID: at.EntityID, HolderID: h.holderID, FencingToken: 1, ExpiresAt: time.Now().UTC().Add(ConnectionLeaseTTL).UnixMilli()}
	if h.machines != nil {
		route, err = h.machines.AcquireRoute(leaseContext, at.MachineID, h.holderID, ConnectionLeaseTTL)
		if err != nil {
			http.Error(w, "machine already connected", http.StatusConflict)
			return
		}
	}
	upgrader := websocket.Upgrader{CheckOrigin: h.origin.Allows}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		if h.machines != nil {
			_ = h.machines.ReleaseRoute(leaseContext, route)
		}
		return
	}
	c := &daemonConn{
		machineID: at.MachineID, addr: r.RemoteAddr, ws: conn,
		claims: claims, route: route, reqs: make(map[int64]*pendingReq),
	}

	sub, err := h.transport.SubscribeCommands(r.Context(), route, func(envelope CommandEnvelope, respond ReplyFunc) {
		reqID := h.seq.Add(1)
		c.mu.Lock()
		c.reqs[reqID] = &pendingReq{respond: respond}
		c.mu.Unlock()
		if err := c.writeJSON(map[string]any{"type": "cmd", "reqId": reqID, "cmd": envelope.Command}); err != nil {
			c.mu.Lock()
			delete(c.reqs, reqID)
			c.mu.Unlock()
			_ = respond(ReplyEnvelope{Route: route, Reply: Reply{OK: false, Error: "daemon disconnected"}})
		}
	})
	if err != nil {
		slog.Warn("subscribe machine commands", "machine", c.machineID, "err", err)
		_ = conn.Close()
		if h.machines != nil {
			_ = h.machines.ReleaseRoute(leaseContext, route)
		}
		return
	}

	var markRunner func(string)
	h.mu.Lock()
	h.conns[at.MachineID] = c
	h.subs[at.MachineID] = sub
	if !at.LongTerm {
		h.pending[at.MachineID] = true
	}
	markRunner = h.markRunner
	h.mu.Unlock()
	if markRunner != nil {
		markRunner(at.MachineID.String())
	}
	if h.machines != nil {
		// 记录 daemon 实际连接地址(来自 WS 握手,不是浏览器 confirm 的地址)。
		// 未确认的机器还没落库,更新是 no-op,confirm 时再从连接里取。
		ctx := authn.WithClaims(r.Context(), &c.claims)
		_ = h.machines.UpdateAddress(ctx, at.MachineID, r.RemoteAddr)
	}
	_ = c.writeJSON(map[string]any{"type": "ready"})
	h.serve(c, sub)
}

func (h *Hub) serve(c *daemonConn, sub io.Closer) {
	leaseDone := make(chan struct{})
	if h.machines != nil {
		go h.renewLease(c, leaseDone)
	}
	defer close(leaseDone)
	defer h.unregister(c, sub)
	for {
		var m struct {
			Type  string            `json:"type"`
			ReqID int64             `json:"reqId,omitempty"`
			OK    *bool             `json:"ok,omitempty"`
			Data  json.RawMessage   `json:"data,omitempty"`
			Event *Event            `json:"event,omitempty"`
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
				var reply Reply
				reply.OK = ok
				if len(m.Data) != 0 {
					_ = json.Unmarshal(m.Data, &reply.Data)
				}
				if err := pr.respond(ReplyEnvelope{Route: c.routeLease(), Reply: reply}); err != nil {
					slog.Debug("respond to runner request", "machine", c.machineID, "err", err)
				}
			}
		case "event":
			if m.Event != nil {
				m.Event.Seq = c.evtSeq.Add(1)
				h.bridgeEvent(c, m.Event)
			}
		case "register":
			var runtimes []string
			if raw := m.Meta["runtimes"]; raw != "" {
				runtimes = strings.Split(raw, ",")
			}
			h.mu.Lock()
			h.names[c.machineID] = m.Meta["name"]
			h.runtimes[c.machineID] = append([]string(nil), runtimes...)
			if osv := m.Meta["os"]; osv != "" {
				h.oses[c.machineID] = osv
			}
			h.mu.Unlock()
			if h.machines != nil {
				_ = h.machines.UpdateRouteInventory(authn.WithClaims(context.Background(), &c.claims), c.routeLease(), m.Meta["name"], runtimes, m.Meta["os"], c.addr)
			}
		}
	}
}

func (h *Hub) renewLease(conn *daemonConn, done <-chan struct{}) {
	ticker := time.NewTicker(ConnectionLeaseTTL / 3)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			route, err := h.machines.RenewRoute(authn.WithClaims(context.Background(), &conn.claims), conn.routeLease(), ConnectionLeaseTTL)
			if err != nil {
				_ = conn.ws.Close()
				return
			}
			conn.setRouteLease(route)
		}
	}
}

func (h *Hub) bridgeEvent(conn *daemonConn, ev *Event) {
	if ev.Type == "heartbeat" && h.machines != nil {
		ctx := authn.WithClaims(context.Background(), &conn.claims)
		_ = h.machines.TouchHeartbeat(ctx, conn.machineID)
	}
	_ = h.transport.PublishEvent(context.Background(), EventEnvelope{Route: conn.routeLease(), Event: *ev})
}

func (h *Hub) unregister(c *daemonConn, sub io.Closer) {
	_ = sub.Close()
	removed := false
	h.mu.Lock()
	if h.conns[c.machineID] == c {
		delete(h.conns, c.machineID)
		removed = true
	}
	// Guard by identity like conns: a reconnecting daemon installs a new sub for
	// the same machine id before the stale conn's loop exits; the stale conn
	// must not delete the live sub (Close would then leak the subscription).
	if h.subs[c.machineID] == sub {
		delete(h.subs, c.machineID)
	}
	wasPending := h.pending[c.machineID]
	if wasPending {
		delete(h.pending, c.machineID)
		// §5.3.1: an unconfirmed machine that disconnects loses its token. Call
		// under h.mu so no race window (tokens.mu is separate; order is h.mu→tokens.mu).
		h.tokens.Invalidate(c.machineID)
		if h.machines != nil {
			_ = h.machines.DeletePendingToken(authn.WithClaims(context.Background(), &c.claims), c.machineID, "")
		}
	}
	var unmarkRunner func(string)
	if removed {
		unmarkRunner = h.unmarkRunner
	}
	h.mu.Unlock()
	// Only unmark when this connection was still the live one: a reconnect can
	// install a new conn for the same machine id before the old conn's read loop
	// exits, and the stale conn must not remove a live runner from discovery.
	if unmarkRunner != nil {
		unmarkRunner(c.machineID.String())
	}
	if h.machines != nil {
		_ = h.machines.ReleaseRoute(authn.WithClaims(context.Background(), &c.claims), c.routeLease())
	}
	_ = c.ws.Close()
}

// ---- machine onboarding surface (unchanged API) ----

// RunnerScope returns the tenant/entity a connected daemon was onboarded under.
// Used to reject cross-tenant runner selection before dispatch.
func (h *Hub) RunnerScope(ctx context.Context, runnerID string) (tenantID, entityID coreid.ID, ok bool) {
	id, err := coreid.Parse(runnerID)
	if err != nil {
		return 0, 0, false
	}
	if h.machines != nil {
		route, err := h.machines.ResolveActiveRoute(ctx, id)
		if err != nil {
			return 0, 0, false
		}
		return route.TenantID, route.EntityID, true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	c := h.conns[id]
	if c == nil {
		return 0, 0, false
	}
	return c.claims.TenantID, c.claims.EntityID, true
}

func (h *Hub) RegisteredRunnersContext(ctx context.Context) []string {
	if h.machines == nil {
		return h.RegisteredRunners()
	}
	routes, err := h.machines.ListActiveRoutes(ctx)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(routes))
	for _, route := range routes {
		out = append(out, route.MachineID.String())
	}
	return out
}

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

func (h *Hub) IsConnectedContext(ctx context.Context, machineID coreid.ID) bool {
	if h.machines == nil {
		return h.IsConnected(machineID)
	}
	_, err := h.machines.ResolveActiveRoute(ctx, machineID)
	return err == nil
}

// MachineRuntimes 返回该机注册时上报的可用执行器列表。
func (h *Hub) MachineRuntimes(runnerID coreid.ID) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string{}, h.runtimes[runnerID]...)
}

func (h *Hub) MachineRuntimesContext(ctx context.Context, runnerID coreid.ID) []string {
	if h.machines == nil {
		return h.MachineRuntimes(runnerID)
	}
	_, runtimes, _, _, err := h.machines.RouteInventory(ctx, runnerID)
	if err != nil {
		return nil
	}
	return runtimes
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

func (h *Hub) MachineNameContext(ctx context.Context, runnerID coreid.ID) string {
	if h.machines == nil {
		return h.MachineName(runnerID)
	}
	name, _, _, _, err := h.machines.RouteInventory(ctx, runnerID)
	if err != nil {
		return ""
	}
	return name
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
	validated, err := h.validateToken(ctx, token)
	if err != nil || validated.MachineID != machineID || validated.LongTerm {
		return ErrTokenInvalid
	}
	var address, osName string
	if h.machines != nil {
		if _, err := h.machines.ResolveActiveRoute(ctx, machineID); err != nil {
			return ErrMachineNotConnected
		}
		_, _, osName, address, err = h.machines.RouteInventory(ctx, machineID)
		if err != nil {
			return err
		}
	} else {
		h.mu.Lock()
		connection := h.conns[machineID]
		if connection == nil || !h.pending[machineID] {
			h.mu.Unlock()
			return ErrMachineNotConnected
		}
		address, osName = connection.addr, h.oses[machineID]
		h.mu.Unlock()
	}
	if h.machines != nil {
		claims, err := requireClaims(ctx)
		if err != nil {
			return err
		}
		if err := h.machines.Upsert(ctx, Machine{
			ID: machineID, TenantID: claims.TenantID, EntityID: claims.EntityID, OwnerID: claims.PrincipalID, Address: address,
			Status: StatusConfirmed, TokenHash: hashToken(token),
			OS: osName,
		}); err != nil {
			return err
		}
		if err := h.machines.DeletePendingToken(ctx, machineID, hashToken(token)); err != nil {
			return err
		}
	}
	h.mu.Lock()
	delete(h.pending, machineID)
	delete(h.pendingScopes, machineID)
	h.mu.Unlock()
	h.tokens.Invalidate(machineID)
	h.tokens.Rehydrate(machineID, validated.TenantID, validated.EntityID, validated.OwnerID, hashToken(token))
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
	subscriptions := make([]io.Closer, 0, len(h.subs))
	for _, connection := range h.conns {
		connections = append(connections, connection.ws)
	}
	for _, subscription := range h.subs {
		subscriptions = append(subscriptions, subscription)
	}
	h.conns = make(map[coreid.ID]*daemonConn)
	h.subs = make(map[coreid.ID]io.Closer)
	h.mu.Unlock()
	var failures []error
	for _, subscription := range subscriptions {
		if err := subscription.Close(); err != nil {
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
	if h.machines != nil {
		if err := h.machines.StorePendingToken(ctx, PendingToken{MachineID: machineID, TenantID: claims.TenantID, EntityID: claims.EntityID, OwnerID: claims.PrincipalID, TokenHash: hashToken(at.Token), ExpiresAt: at.ExpiresAt.UTC().UnixMilli(), CreatedAt: time.Now().UTC().UnixMilli()}); err != nil {
			h.tokens.Invalidate(machineID)
			return 0, "", time.Time{}, err
		}
	}
	h.mu.Lock()
	h.pendingScopes[machineID] = *claims
	h.mu.Unlock()
	return machineID, at.Token, at.ExpiresAt, nil
}

// CanAccess applies the same tenant visibility rule to pending in-memory
// onboarding records and confirmed machines persisted in StateStore.
func (h *Hub) CanAccess(ctx context.Context, machineID coreid.ID) bool {
	if h.machines != nil {
		if _, err := h.machines.PendingTokenForMachine(ctx, machineID); err == nil {
			return true
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
	h.mu.Lock()
	scope, pending := h.pendingScopes[machineID]
	h.mu.Unlock()
	if pending {
		claims, err := requireClaims(ctx)
		return err == nil && scope.TenantID == claims.TenantID && scope.EntityID == claims.EntityID && scope.PrincipalID == claims.PrincipalID
	}
	return authn.EntityIDFromContext(ctx).Zero()
}

// OnboardingStatus returns the authoritative state for the 300-second
// onboarding window. The raw token is accepted in the JSON request body by the
// HTTP layer so it does not leak into access logs or browser history.
func (h *Hub) OnboardingStatus(machineID coreid.ID, token string) (state string, expiresAt time.Time) {
	at, err := h.validateToken(context.Background(), token)
	if errors.Is(err, ErrTokenExpired) {
		return "expired", time.Time{}
	}
	if err != nil || at.MachineID != machineID {
		return "invalid", time.Time{}
	}
	if at.LongTerm {
		return "confirmed", time.Time{}
	}
	connected := false
	if h.machines != nil {
		claims := authn.Claims{TenantID: at.TenantID, EntityID: at.EntityID, PrincipalID: at.OwnerID}
		_, routeErr := h.machines.ResolveActiveRoute(authn.WithClaims(context.Background(), &claims), machineID)
		connected = routeErr == nil
	} else {
		h.mu.Lock()
		connected = h.conns[machineID] != nil && h.pending[machineID]
		h.mu.Unlock()
	}
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
	if h.machines != nil {
		if err := h.machines.RevokeRoute(ctx, machineID); err != nil {
			return "", err
		}
	}
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
		if err := h.machines.RevokeRoute(ctx, machineID); err != nil {
			return err
		}
		_ = h.machines.DeletePendingToken(ctx, machineID, "")
		if err := h.machines.Delete(ctx, machineID); err != nil {
			return err
		}
	}
	return nil
}
