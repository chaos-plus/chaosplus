package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/machine"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/store"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/websec"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/workflow"
)

var upgrader = websocket.Upgrader{
	CheckOrigin:     websec.OriginAllowed,
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// AuthToken is the pre-shared API token, read from env at startup (NOT argv —
// argv is world-readable via /proc). Empty = auth disabled (desktop/localhost).
var AuthToken string

// RESTRegistrar is implemented by bounded-context modules. The server package
// owns cross-cutting middleware only; feature routes register themselves.
type RESTRegistrar interface {
	RegisterREST(*http.ServeMux)
}

// authMiddleware wraps h with Bearer token validation. Skipped when AuthToken
// is empty (desktop profile). Machine WebSocket onboarding routes are exempt —
// they validate their own machine tokens.
// /api/machines/ws (machine authenticates via its own token) and
// /api/email/notification (external mailbridge webhook) are exempt; minting a
// machine credential (/api/machines/tokens) is NOT — it must require the
// server-ai Bearer when auth is enabled (review: auth bypass).
var authExemptPrefixes = []string{"/api/machines/ws", "/api/email/notification"}

func authMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Machine onboarding carries its own token; server-ai token is not required.
		for _, prefix := range authExemptPrefixes {
			if strings.HasPrefix(r.URL.Path, prefix) {
				h.ServeHTTP(w, r)
				return
			}
		}
		// PRD F.7 explicitly permits Get/List/Subscribe without a session token.
		if readOnlyRequest(r) || AuthToken == "" {
			h.ServeHTTP(w, r)
			return
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
	// M3 (round-3 review): accept ?token only on WebSocket upgrade routes
	// (browsers can't set Authorization on an upgrade request); elsewhere a
	// query-string credential would leak in logs/history/Referer.
	if isWSPath(r.URL.Path) {
		if t := r.URL.Query().Get("token"); t != "" {
			return subtle.ConstantTimeCompare([]byte(t), []byte(AuthToken)) == 1
		}
	}
	return false
}

// isWSPath reports whether a path is a WebSocket upgrade route.
func isWSPath(p string) bool {
	return strings.HasSuffix(p, "/events") || p == "/api/machines/ws"
}

// NewHandler wires all server-ai HTTP routes.
func NewHandler(m *RunManager, hub *machine.Hub, chat *ChatService, modules ...RESTRegistrar) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if m == nil || m.nc == nil || !m.nc.IsConnected() {
			writeErr(w, http.StatusServiceUnavailable, "NATS is not connected")
			return
		}
		if m.st != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if err := m.st.Ping(ctx); err != nil {
				writeErr(w, http.StatusServiceUnavailable, "state store is not ready")
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	if chat != nil {
		chat.register(mux)
	}
	for _, module := range modules {
		if module != nil {
			module.RegisterREST(mux)
		}
	}

	// machines — runner onboarding (PRD §5.3.1).
	mux.HandleFunc("POST /api/machines/tokens", func(w http.ResponseWriter, r *http.Request) {
		machineID, token, expiresAt := hub.IssueTokenFor(r.Context())
		writeJSON(w, 201, map[string]any{
			"token": token, "machineId": machineID, "longTerm": false,
			"expiresAt": expiresAt.UnixMilli(),
		})
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
		if e := store.EntityOf(r.Context()); e != "" && chat != nil {
			_ = chat.st.UpdateMachineEntity(r.Context(), id, e)
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /api/machines/{id}/onboarding-status", func(w http.ResponseWriter, r *http.Request) {
		if !hub.CanAccess(r.Context(), r.PathValue("id")) {
			writeErr(w, http.StatusNotFound, "machine not found")
			return
		}
		var body struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil || body.Token == "" {
			writeErr(w, http.StatusBadRequest, "invalid onboarding status request")
			return
		}
		state, expiresAt := hub.OnboardingStatus(r.PathValue("id"), body.Token)
		response := map[string]any{"state": state}
		if !expiresAt.IsZero() {
			response["expiresAt"] = expiresAt.UnixMilli()
		}
		writeJSON(w, http.StatusOK, response)
	})
	mux.HandleFunc("DELETE /api/machines/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !hub.CanAccess(r.Context(), r.PathValue("id")) {
			writeErr(w, http.StatusNotFound, "machine not found")
			return
		}
		hub.Cancel(r.PathValue("id"))
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("GET /api/machines/{id}/token", func(w http.ResponseWriter, r *http.Request) {
		if !hub.CanAccess(r.Context(), r.PathValue("id")) {
			writeErr(w, http.StatusNotFound, "machine not found")
			return
		}
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
		if !hub.CanAccess(r.Context(), r.PathValue("id")) {
			writeErr(w, http.StatusNotFound, "machine not found")
			return
		}
		token, err := hub.RefreshToken(r.PathValue("id"))
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		// 长期 token:手动轮换后长期有效,无过期时间。
		writeJSON(w, 200, map[string]any{"token": token, "longTerm": true})
	})
	mux.HandleFunc("POST /api/runs", func(w http.ResponseWriter, r *http.Request) {
		var req LaunchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, 400, "bad request: "+err.Error())
			return
		}
		req.InstanceID = store.EntityOf(r.Context())
		req.OwnerID = store.OwnerOf(r.Context())
		// The run outlives the HTTP request: tie it to the process lifetime, not
		// r.Context() (which cancels when this handler returns and would kill a
		// run parked on a human-approval gate).
		run, err := m.Launch(m.Context(), req)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		writeJSON(w, 201, map[string]any{"runId": run.ID})
	})
	mux.HandleFunc("GET /api/artifacts", func(w http.ResponseWriter, r *http.Request) {
		if m.st == nil {
			writeJSON(w, http.StatusOK, []store.Artifact{})
			return
		}
		artifacts, err := m.st.ListArtifacts(r.Context(), store.EntityOf(r.Context()), r.URL.Query().Get("projectId"), r.URL.Query().Get("status"))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to list artifacts")
			return
		}
		writeJSON(w, http.StatusOK, artifacts)
	})
	mux.HandleFunc("POST /api/artifacts/{id}/force-valid", func(w http.ResponseWriter, r *http.Request) {
		if m.st == nil {
			writeErr(w, http.StatusServiceUnavailable, "store not available")
			return
		}
		reviewer := store.OwnerOf(r.Context())
		if reviewer == "" {
			reviewer = "human"
		}
		artifact, err := m.st.GetArtifact(r.Context(), r.PathValue("id"), store.EntityOf(r.Context()))
		if err != nil || artifact.Status == store.ArtifactOrphaned {
			writeErr(w, http.StatusNotFound, "artifact not found or orphaned")
			return
		}
		artifact.Status, artifact.ForceValid = store.ArtifactValid, true
		payload, err := json.Marshal(map[string]any{"artifact": artifact, "reviewer": reviewer})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to encode force-valid audit event")
			return
		}
		committed, err := m.st.CommitArtifactEventIfChecksum(r.Context(), store.Event{
			ID: "force-" + randHex(8), InstanceID: artifact.InstanceID, RunID: artifact.ProducerRunID,
			Type: "ARTIFACT_FORCE_VALIDATED", IdempotencyKey: "force:" + artifact.ID + ":" + randHex(8), PayloadJSON: string(payload),
		}, artifact.Checksum)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to persist force-valid audit event")
			return
		}
		if !committed {
			writeErr(w, http.StatusConflict, "artifact changed while it was being reviewed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/artifacts/reconcile", func(w http.ResponseWriter, r *http.Request) {
		report, err := m.ReconcileArtifacts(r.Context(), store.EntityOf(r.Context()))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "artifact reconciliation failed")
			return
		}
		writeJSON(w, http.StatusOK, report)
	})
	mux.HandleFunc("GET /api/runs", func(w http.ResponseWriter, r *http.Request) {
		type sum struct {
			ID        string    `json:"id"`
			Status    RunStatus `json:"status"`
			Nodes     int       `json:"nodes"`
			CreatedAt string    `json:"createdAt"`
		}
		out := []sum{}
		seen := map[string]bool{}
		instanceID := store.EntityOf(r.Context())
		for _, run := range m.ListFor(instanceID) {
			seen[run.ID] = true
			out = append(out, sum{ID: run.ID, Status: run.Status(), Nodes: len(run.Def.Nodes), CreatedAt: run.created.Format("2006-01-02 15:04:05")})
		}
		// Persisted runs (PRD §15.1) via the merged RunDef persistence.
		if m.st != nil {
			if recs, err := m.st.ListRunDefinitions(r.Context(), instanceID, 500); err != nil {
				slog.Warn("list persisted runs", "err", err)
			} else {
				for _, rec := range recs {
					if seen[rec.ID] {
						continue
					}
					out = append(out, sum{ID: rec.ID, Status: RunStatus(rec.Status),
						Nodes:     nodeCountFromSnapshot(rec.DefJSON),
						CreatedAt: time.UnixMilli(rec.CreatedAt).Format("2006-01-02 15:04:05")})
				}
			}
		}
		writeJSON(w, 200, out)
	})
	mux.HandleFunc("GET /api/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		instanceID := store.EntityOf(r.Context())
		run, ok := m.GetFor(id, instanceID)
		if ok {
			// 返回 run 的静态 DAG(供前端 React Flow 渲染节点/边 + 实时状态)。
			writeJSON(w, 200, map[string]any{"id": run.ID, "status": run.Status(), "def": run.Def})
			return
		}
		// 历史 run(重启后保留):从 workflow_runs 重建 DAG 快照(PRD §15.1)。
		if m.st != nil {
			if rec, err := m.st.GetRunDef(r.Context(), id); err == nil && (instanceID == "" || rec.InstanceID == instanceID) {
				var def workflow.WorkflowDef
				if json.Unmarshal([]byte(rec.DefJSON), &def) == nil {
					writeJSON(w, 200, map[string]any{"id": rec.ID, "status": RunStatus(rec.Status), "def": &def})
					return
				}
			}
		}
		writeErr(w, 404, "run not found")
	})
	mux.HandleFunc("POST /api/runs/{id}/pause", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := m.GetFor(r.PathValue("id"), store.EntityOf(r.Context())); !ok {
			writeErr(w, http.StatusNotFound, "run not found")
			return
		}
		if err := m.Pause(r.PathValue("id")); err != nil {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/runs/{id}/resume", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := m.GetFor(r.PathValue("id"), store.EntityOf(r.Context())); !ok {
			writeErr(w, http.StatusNotFound, "run not found")
			return
		}
		if err := m.Resume(r.PathValue("id")); err != nil {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/runs/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := m.GetFor(r.PathValue("id"), store.EntityOf(r.Context())); !ok {
			writeErr(w, http.StatusNotFound, "run not found")
			return
		}
		if err := m.Cancel(r.PathValue("id")); err != nil {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	// PRD D.1 仪表盘:活跃 run 状态分布 / runner 健康 / 待审批队列 / 今日成本。
	// ── workflow definitions CRUD (PRD §16 workflows table) ──
	mux.HandleFunc("GET /api/workflows", func(w http.ResponseWriter, r *http.Request) {
		if chat == nil || chat.st == nil {
			writeJSON(w, 200, []any{})
			return
		}
		instanceID := store.EntityOf(r.Context())
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
			DefJSON: defJSON, InstanceID: store.EntityOf(r.Context()), OwnerID: store.OwnerOf(r.Context()),
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
		for _, run := range m.ListFor(store.EntityOf(r.Context())) {
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
		if _, ok := m.GetFor(id, store.EntityOf(r.Context())); !ok {
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
		// M7 (round-3 review): reason is persisted; cap it like the feedback fields.
		if len(body.Reason) > workflow.MaxFeedbackFieldLen {
			writeErr(w, 400, "reason too long")
			return
		}
		if err := m.Approve(id, node, body.Approve, body.Reason, body.Feedback); err != nil {
			writeErr(w, 409, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	// Auth gate first, then entity/actor context injection.
	return securityMiddleware(authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hardened := os.Getenv("CONTROL_AUTH_HARDENED") == "1"
		entityID, ownerID := r.Header.Get("X-Entity"), r.Header.Get("X-Actor")
		if hardened && (entityID != "" || ownerID != "") && !trustedIdentityProxy(r) {
			writeErr(w, 401, "unauthorized: identity headers require a trusted proxy")
			return
		}
		if hardened && entityID == "" {
			entityID = "desktop"
		}
		if hardened && ownerID == "" {
			ownerID = "api-token"
		}
		ctx := r.Context()
		if entityID != "" {
			ctx = store.WithEntity(ctx, entityID)
		}
		if ownerID != "" {
			ctx = store.WithOwner(ctx, ownerID)
		}
		mux.ServeHTTP(w, r.WithContext(ctx))
	})))
}

func trustedIdentityProxy(r *http.Request) bool {
	expected := os.Getenv("CONTROL_TRUSTED_PROXY_TOKEN")
	provided := r.Header.Get("X-Control-Proxy-Token")
	return expected != "" && provided != "" && subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) == 1
}

// readOnlyRequest reports whether a request is read-only (F.7: Get*/List*/Subscribe).
func readOnlyRequest(r *http.Request) bool {
	if strings.HasSuffix(r.URL.Path, "/token") {
		return false
	}
	return r.Method == http.MethodGet || r.Method == http.MethodHead
}

func securityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
		}
		next.ServeHTTP(w, r)
	})
}

// handleWS upgrades to WebSocket, replays buffered events, then streams live.
func (m *RunManager) handleWS(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, ok := m.GetFor(id, store.EntityOf(r.Context()))
	if !ok {
		writeErr(w, 404, "run not found")
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()

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
