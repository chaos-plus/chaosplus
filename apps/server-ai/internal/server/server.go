package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/machine"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/store"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/workflow"
)

var upgrader = websocket.Upgrader{
	CheckOrigin:     func(r *http.Request) bool { return true }, // local dev single-user
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// AuthToken is the pre-shared API token, read from env at startup (NOT argv —
// argv is world-readable via /proc). Empty = auth disabled (desktop/localhost).
var AuthToken string

// authMiddleware wraps h with Bearer token validation. Skipped when AuthToken
// is empty (desktop profile). Machine WebSocket onboarding routes are exempt —
// they validate their own machine tokens.
var authExemptPrefixes = []string{"/api/machines/ws", "/api/machines/tokens"}

func authMiddleware(h http.Handler) http.Handler {
	if AuthToken == "" {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Machine onboarding carries its own token; server-ai token is not required.
		for _, prefix := range authExemptPrefixes {
			if strings.HasPrefix(r.URL.Path, prefix) {
				h.ServeHTTP(w, r)
				return
			}
		}
		if !validAuth(r) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="server-ai"`)
			writeErr(w, 401, "unauthorized")
			return
		}
		h.ServeHTTP(w, r)
	})
}

// validAuth checks the Authorization header (Bearer) or ?token query param.
func validAuth(r *http.Request) bool {
	if b := r.Header.Get("Authorization"); b != "" {
		const prefix = "Bearer "
		if len(b) > len(prefix) && subtle.ConstantTimeCompare([]byte(b[len(prefix):]), []byte(AuthToken)) == 1 {
			return true
		}
	}
	// Fallback for WebSocket (browser can't set Authorization header on
	// upgrade requests — token passes via query string).
	if t := r.URL.Query().Get("token"); t != "" {
		return subtle.ConstantTimeCompare([]byte(t), []byte(AuthToken)) == 1
	}
	return false
}

// NewHandler wires all server-ai HTTP routes.
func NewHandler(m *RunManager, hub *machine.Hub, chat *ChatService) http.Handler {
	mux := http.NewServeMux()
	if chat != nil {
		chat.register(mux)
	}

	// machines — runner onboarding (PRD §5.3.1).
	mux.HandleFunc("POST /api/machines/tokens", func(w http.ResponseWriter, r *http.Request) {
		machineID, token := hub.IssueToken()
		writeJSON(w, 201, map[string]any{"token": token, "machineId": machineID, "longTerm": true})
	})
	mux.HandleFunc("GET /api/machines/ws", hub.HandleWS)
	mux.HandleFunc("GET /api/machines", func(w http.ResponseWriter, r *http.Request) {
		ms, _ := hub.ListMachines(r.Context())
		type msum struct {
			ID              string `json:"id"`
			Name            string `json:"name"`
			Address         string `json:"address"`
			Status          string `json:"status"`
			Online          bool   `json:"online"`
			LastHeartbeatAt int64  `json:"lastHeartbeatAt"`
			// AgentCount 是该机托管的数字人数(PRD D.3 列表列)。
			AgentCount int `json:"agentCount"`
			// Runtimes 是该机可用的执行器(数字人表单的 runtime 下拉来源)。
			Runtimes []string `json:"runtimes"`
		}
		counts := map[string]int{}
		if chat != nil && chat.st != nil {
			if c, err := chat.st.CountAgentsByMachine(r.Context()); err == nil {
				counts = c
			}
		}
		out := []msum{}
		for _, m := range ms {
			name := hub.MachineName(m.ID)
			if name == "" {
				name = m.ID
			}
			online := hub.IsConnected(m.ID)
			runtimes := []string{}
			if online {
				runtimes = hub.MachineRuntimes(m.ID)
			}
			out = append(out, msum{
				ID: m.ID, Name: name, Address: m.Address, Status: m.Status,
				Online: online, LastHeartbeatAt: m.LastHeartbeatAt,
				AgentCount: counts[m.ID], Runtimes: runtimes,
			})
		}
		writeJSON(w, 200, out)
	})
	// PRD D.3 machine 详情:关键信息 / 运行时 / 托管 agent 列表。
	mux.HandleFunc("GET /api/machines/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		ms, _ := hub.ListMachines(r.Context())
		var found *store.Machine
		for i := range ms {
			if ms[i].ID == id {
				found = &ms[i]
				break
			}
		}
		if found == nil {
			writeErr(w, 404, "machine not found")
			return
		}
		name := hub.MachineName(id)
		if name == "" {
			name = id
		}
		agents := []store.AgentSpec{}
		if chat != nil && chat.st != nil {
			if all, err := chat.st.ListAgents(r.Context()); err == nil {
				for _, a := range all {
					if a.MachineID == id {
						agents = append(agents, a)
					}
				}
			}
		}
		writeJSON(w, 200, map[string]any{
			"id":              found.ID,
			"name":            name,
			"address":         found.Address,
			"status":          found.Status,
			"online":          hub.IsConnected(id),
			"os":              found.OS,
			"registeredAt":    found.RegisteredAt,
			"lastHeartbeatAt": found.LastHeartbeatAt,
			"runtimes":        hub.MachineRuntimes(id),
			"agents":          agents,
		})
	})
	mux.HandleFunc("POST /api/machines/{id}/confirm", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var body struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, 400, "bad request: "+err.Error())
			return
		}
		if err := hub.Confirm(r.Context(), id, body.Token); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		if e := r.Header.Get("X-Entity"); e != "" && chat != nil {
			_ = chat.st.UpdateMachineEntity(r.Context(), id, e)
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("DELETE /api/machines/{id}", func(w http.ResponseWriter, r *http.Request) {
		hub.Cancel(r.PathValue("id"))
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("GET /api/machines/{id}/token", func(w http.ResponseWriter, r *http.Request) {
		// 只读返回当前 token(展示接入命令),不轮换、不踢守护进程。
		token, err := hub.GetToken(r.PathValue("id"))
		if err != nil {
			// 控制面重启后内存只有 hash,原始 token 取不到 → 提示轮换生成。
			writeErr(w, 404, "no token in memory; rotate to generate a new command")
			return
		}
		writeJSON(w, 200, map[string]any{"token": token})
	})
	mux.HandleFunc("POST /api/machines/{id}/refresh-token", func(w http.ResponseWriter, r *http.Request) {
		token, err := hub.RefreshToken(r.PathValue("id"))
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		// 长期 token:手动轮换后长期有效,无过期时间。
		writeJSON(w, 200, map[string]any{"token": token, "longTerm": true})
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(uiHTML))
	})
	mux.HandleFunc("POST /api/runs", func(w http.ResponseWriter, r *http.Request) {
		var req LaunchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, 400, "bad request: "+err.Error())
			return
		}
		// The run outlives the HTTP request: tie it to the process lifetime, not
		// r.Context() (which cancels when this handler returns and would kill a
		// run parked on a human-approval gate).
		run, err := m.Launch(context.Background(), req)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		writeJSON(w, 201, map[string]any{"runId": run.ID})
	})
	mux.HandleFunc("GET /api/runs", func(w http.ResponseWriter, r *http.Request) {
		type sum struct {
			ID        string    `json:"id"`
			Status    RunStatus `json:"status"`
			Nodes     int       `json:"nodes"`
			CreatedAt string    `json:"createdAt"`
		}
		out := []sum{}
		for _, run := range m.List() {
			out = append(out, sum{ID: run.ID, Status: run.Status(), Nodes: len(run.Def.Nodes), CreatedAt: run.created.Format("2006-01-02 15:04:05")})
		}
		writeJSON(w, 200, out)
	})
	mux.HandleFunc("GET /api/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		run, ok := m.Get(r.PathValue("id"))
		if !ok {
			writeErr(w, 404, "run not found")
			return
		}
		// 返回 run 的静态 DAG(供前端 React Flow 渲染节点/边 + 实时状态)。
		writeJSON(w, 200, map[string]any{
			"id":     run.ID,
			"status": run.Status(),
			"def":    run.Def,
		})
	})
	// PRD D.1 仪表盘:活跃 run 状态分布 / runner 健康 / 待审批队列 / 今日成本。
	// ── workflow definitions CRUD (PRD §16 workflows table) ──
	mux.HandleFunc("GET /api/workflows", func(w http.ResponseWriter, r *http.Request) {
		if chat == nil || chat.st == nil {
			writeJSON(w, 200, []any{})
			return
		}
		instanceID := r.Header.Get("X-Entity")
		list, err := chat.st.ListWorkflows(r.Context(), instanceID)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		// Populate def from def_json so the client sees the workflow JSON.
		for i := range list {
			if list[i].DefJSON != "" {
				var def any
				if json.Unmarshal([]byte(list[i].DefJSON), &def) == nil {
					list[i].DefRaw = def
				}
			}
		}
		writeJSON(w, 200, list)
	})
	mux.HandleFunc("POST /api/workflows", func(w http.ResponseWriter, r *http.Request) {
		if chat == nil || chat.st == nil {
			writeErr(w, 503, "store not available")
			return
		}
		var body struct {
			ID      string          `json:"id"`
			Version string          `json:"version"`
			Name    string          `json:"name"`
			Def     json.RawMessage `json:"def"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, 400, "bad request: "+err.Error())
			return
		}
		defJSON := string(body.Def)
		if defJSON == "" || defJSON == "null" {
			defJSON = "{}"
		}
		if err := chat.st.SaveWorkflow(r.Context(), store.WorkflowDefModel{
			ID: body.ID, Version: body.Version, Name: body.Name,
			DefJSON: defJSON, InstanceID: r.Header.Get("X-Entity"), OwnerID: r.Header.Get("X-Actor"),
		}); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 201, map[string]any{"ok": true})
	})
	mux.HandleFunc("DELETE /api/workflows/{id}", func(w http.ResponseWriter, r *http.Request) {
		if chat == nil || chat.st == nil {
			writeErr(w, 503, "store not available")
			return
		}
		if err := chat.st.DeleteWorkflow(r.Context(), r.PathValue("id")); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})

	mux.HandleFunc("GET /api/stats/dashboard", func(w http.ResponseWriter, r *http.Request) {
		type pending struct {
			RunID  string `json:"runId"`
			NodeID string `json:"nodeId"`
			// ChannelID 让前端能跳到对应频道的审批卡片(D.1 要求点击跳转)。
			ChannelID string `json:"channelId"`
			Title     string `json:"title"`
		}
		byStatus := map[string]int{}
		approvals := []pending{}
		for _, run := range m.List() {
			byStatus[string(run.Status())]++
			if run.Status() != RunWaitingApproval {
				continue
			}
			for _, ev := range run.Events() {
				if ev.Status == workflow.StatusWaitingApproval && ev.NodeID != "" {
					approvals = append(approvals, pending{RunID: run.ID, NodeID: ev.NodeID})
				}
			}
		}
		// 关联工作项 → 频道,供点击跳转。
		if chat != nil && chat.st != nil {
			items, err := chat.st.ListWorkItems(r.Context(), "", "", "")
			if err == nil {
				for i := range approvals {
					for _, it := range items {
						if it.WorkflowRunID == approvals[i].RunID {
							approvals[i].ChannelID, approvals[i].Title = it.ChannelID, it.Title
						}
					}
				}
			}
		}

		machines := []store.Machine{}
		var online int
		var lastHeartbeat int64
		if hub != nil {
			if list, err := hub.ListMachines(r.Context()); err == nil {
				machines = list
				connected := map[string]bool{}
				for _, id := range hub.RegisteredRunners() {
					connected[id] = true
				}
				for _, mm := range machines {
					if connected[mm.ID] {
						online++
					}
					if mm.LastHeartbeatAt > lastHeartbeat {
						lastHeartbeat = mm.LastHeartbeatAt
					}
				}
			}
		}

		var costToday float64
		if chat != nil && chat.st != nil {
			now := time.Now()
			midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UnixMilli()
			if c, err := chat.st.SumCostSince(r.Context(), midnight); err == nil {
				costToday = c
			}
		}

		writeJSON(w, 200, map[string]any{
			"runsByStatus":     byStatus,
			"pendingApprovals": approvals,
			"machinesTotal":    len(machines),
			"machinesOnline":   online,
			"lastHeartbeatAt":  lastHeartbeat,
			"costTodayUsd":     costToday,
		})
	})
	mux.HandleFunc("GET /api/runs/{id}/events", m.handleWS)
	mux.HandleFunc("POST /api/runs/{id}/approvals/{node}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		node := r.PathValue("node")
		var body struct {
			Approve  bool               `json:"approve"`
			Reason   string             `json:"reason"`
			Feedback *workflow.Feedback `json:"feedback"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, 400, "bad request: "+err.Error())
			return
		}
		if _, ok := m.Get(id); !ok {
			writeErr(w, 404, "run not found")
			return
		}
		// PRD §13:拒绝必须带结构化反馈,缺字段是客户端错误(400)而非冲突。
		if !body.Approve {
			if body.Feedback == nil {
				writeErr(w, 400, "rejection requires structured feedback: category(功能缺陷|样式|需求偏差|其他) + detail")
				return
			}
			if err := body.Feedback.Validate(); err != nil {
				writeErr(w, 400, err.Error())
				return
			}
		}
		if err := m.Approve(id, node, body.Approve, body.Reason, body.Feedback); err != nil {
			writeErr(w, 409, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	// Auth gate first, then entity/actor context injection.
	return authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if e := r.Header.Get("X-Entity"); e != "" {
			ctx = store.WithEntity(ctx, e)
		}
		if o := r.Header.Get("X-Actor"); o != "" {
			ctx = store.WithOwner(ctx, o)
		}
		mux.ServeHTTP(w, r.WithContext(ctx))
	}))
}

// handleWS upgrades to WebSocket, replays buffered events, then streams live.
func (m *RunManager) handleWS(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, ok := m.Get(id)
	if !ok {
		writeErr(w, 404, "run not found")
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	sub, hist, unsub := run.subscribe()
	defer unsub()
	for _, ev := range hist { // replay buffered
		if err := conn.WriteJSON(ev); err != nil {
			return
		}
	}
	for ev := range sub {
		if err := conn.WriteJSON(ev); err != nil {
			return
		}
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": msg})
}
