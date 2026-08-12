package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/gorilla/websocket"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/gateway"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/store"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/workflow"
)

// ChatService owns the 会话区 domain: team agents, channels, members, and the
// message log. A user message in a channel that has an agent member is routed to
// that agent, which executes on a connected machine (claude/codex) and writes
// its deliverable to output.json in the channel's workspace; the result is
// appended back into the channel as the agent's reply (PRD §9, §23).
type ChatService struct {
	st     *store.Store
	link   workflow.RunnerLink
	g      *gateway.Gateway
	wsRoot string
	seq    atomic.Int64
	hub    *channelHub // live message fan-out (PRD §9.1 realtime)

	execMu  sync.Mutex
	execLog map[string][]ProgressEntry // channelID → 本次 agent 执行的实时活动

	busyMu    sync.Mutex
	agentBusy map[string]int // agentID → 并发执行数(路由时选更闲的)
}

// ProgressEntry is one agent-activity line surfaced to the frontend (chat
// execution popup), e.g. a tool call or an agent message.
type ProgressEntry struct {
	TS      int64  `json:"ts"`
	Kind    string `json:"kind"` // message | tool | spawn | done | error
	Content string `json:"content"`
}

func NewChatService(st *store.Store, link workflow.RunnerLink, g *gateway.Gateway, wsRoot string) *ChatService {
	return &ChatService{st: st, link: link, g: g, wsRoot: wsRoot,
		execLog: make(map[string][]ProgressEntry), agentBusy: make(map[string]int), hub: newChannelHub()}
}

// channelHub fans a newly-persisted channel message out to the channel's live
// WS subscribers (PRD §9.1 realtime). In-memory, per-instance; subscribers
// reconnect on WS drop. Missing subscribers are dropped (best-effort fan-out).
type channelHub struct {
	mu   sync.Mutex
	subs map[string]map[chan store.ChannelMessage]struct{}
}

func newChannelHub() *channelHub {
	return &channelHub{subs: make(map[string]map[chan store.ChannelMessage]struct{})}
}

func (h *channelHub) subscribe(channelID string) (chan store.ChannelMessage, func()) {
	ch := make(chan store.ChannelMessage, 64)
	h.mu.Lock()
	if h.subs[channelID] == nil {
		h.subs[channelID] = make(map[chan store.ChannelMessage]struct{})
	}
	h.subs[channelID][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs[channelID], ch)
		if len(h.subs[channelID]) == 0 {
			delete(h.subs, channelID) // 避免空内层 map 累积
		}
		h.mu.Unlock()
	}
}

func (h *channelHub) publish(channelID string, msg store.ChannelMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[channelID] {
		select {
		case ch <- msg: // buffered; drop slow subscribers rather than block
		default:
		}
	}
}

// channelEventsWS streams a channel's messages live (PRD §9.1). It replays the
// recent history first so nothing is missed between the client's initial fetch
// and this subscription; the client dedups by message id.
func (cs *ChatService) channelEventsWS(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("id")
	if _, err := cs.st.GetChannel(r.Context(), channelID); err != nil {
		writeErr(w, http.StatusNotFound, "channel not found")
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()

	ch, unsub := cs.hub.subscribe(channelID)
	defer unsub()

	// Reader goroutine: drain client frames so a silent disconnect (no write for
	// a while) unblocks the writer via the read error, instead of leaking the
	// handler goroutine + hub subscription forever (M2).
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	if recent, err := cs.st.ListChannelMessages(r.Context(), channelID, 50); err == nil {
		for i := range recent {
			data, _ := json.Marshal(recent[i])
			if err := writeWS(conn, data); err != nil {
				return
			}
		}
	}

	for {
		select {
		case msg := <-ch:
			data, _ := json.Marshal(msg)
			if err := writeWS(conn, data); err != nil {
				return
			}
		case <-readDone:
			return // client disconnected / read error
		case <-r.Context().Done():
			return
		}
	}
}

// writeWS writes a frame with a deadline so a dead-but-open socket cannot stall
// the handler (M2).
func writeWS(conn *websocket.Conn, data []byte) error {
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return conn.WriteMessage(websocket.TextMessage, data)
}

func (cs *ChatService) busyOf(agentID string) int {
	cs.busyMu.Lock()
	defer cs.busyMu.Unlock()
	return cs.agentBusy[agentID]
}

func (cs *ChatService) markBusy(agentID string, delta int) {
	cs.busyMu.Lock()
	defer cs.busyMu.Unlock()
	cs.agentBusy[agentID] += delta
	if cs.agentBusy[agentID] < 0 {
		cs.agentBusy[agentID] = 0
	}
}

func (cs *ChatService) appendExec(channelID, kind, content string) {
	cs.execMu.Lock()
	defer cs.execMu.Unlock()
	log := cs.execLog[channelID]
	log = append(log, ProgressEntry{TS: time.Now().UnixMilli(), Kind: kind, Content: content})
	if n := len(log); n > 300 {
		log = log[n-300:]
	}
	cs.execLog[channelID] = log
}

func (cs *ChatService) execution(channelID string) []ProgressEntry {
	cs.execMu.Lock()
	defer cs.execMu.Unlock()
	// 必须返回 [] 而非 nil:nil 会 marshal 成 null,前端 .map 会崩。
	return append([]ProgressEntry{}, cs.execLog[channelID]...)
}

func (cs *ChatService) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/agents", func(w http.ResponseWriter, r *http.Request) {
		items, err := cs.st.ListAgents(r.Context())
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, items)
	})
	mux.HandleFunc("POST /api/agents", func(w http.ResponseWriter, r *http.Request) {
		var a store.AgentSpec
		if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		if a.Name == "" || a.Runtime == "" {
			writeErr(w, 400, "name and runtime are required")
			return
		}
		a.ID = "ag-" + randHex(6)
		a.EntityID = store.EntityOf(r.Context())
		a.OwnerID = store.OwnerOf(r.Context())
		if err := cs.st.CreateAgent(r.Context(), &a); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 201, a)
	})
	mux.HandleFunc("PUT /api/agents/{id}", func(w http.ResponseWriter, r *http.Request) {
		var a store.AgentSpec
		if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		a.ID = r.PathValue("id")
		existing, gerr := cs.st.GetAgent(r.Context(), a.ID)
		if gerr != nil {
			writeErr(w, 404, "agent not found")
			return
		}
		if !canManageAgent(r.Header.Get("X-Actor"), existing) {
			writeErr(w, 403, "只有 agent 创建者可以编辑")
			return
		}
		if err := cs.st.UpdateAgent(r.Context(), &a); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, a)
	})
	// PRD D.4:启动 / 停止(生命周期操作,不改配置)。
	mux.HandleFunc("POST /api/agents/{id}/status", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, 400, "请求体格式不正确")
			return
		}
		if body.Status != "running" && body.Status != "stopped" {
			writeErr(w, 400, "status 只能是 running 或 stopped")
			return
		}
		a, err := cs.st.GetAgent(r.Context(), r.PathValue("id"))
		if err != nil {
			writeErr(w, 404, "agent not found")
			return
		}
		if !canManageAgent(r.Header.Get("X-Actor"), a) {
			writeErr(w, 403, "只有 agent 创建者可以操作")
			return
		}
		if a.Status == "retired" {
			writeErr(w, 409, "已注销的数字人不能再启动")
			return
		}
		if body.Status == "running" && a.MachineID == "" {
			writeErr(w, 400, "启动前需要先指定所属 machine")
			return
		}
		if err := cs.st.SetAgentStatus(r.Context(), a.ID, body.Status); err != nil {
			slog.Error("set agent status", "agent", a.ID, "err", err)
			writeErr(w, 500, "更新状态失败")
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "status": body.Status})
	})

	// PRD D.4 / §6.2.1:注销。正常注销产出交接文档;强制注销跳过交接。
	mux.HandleFunc("POST /api/agents/{id}/retire", cs.retireAgent)

	mux.HandleFunc("DELETE /api/agents/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := cs.st.DeleteAgent(r.Context(), r.PathValue("id")); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})

	mux.HandleFunc("GET /api/channels", func(w http.ResponseWriter, r *http.Request) {
		items, err := cs.st.ListChannels(r.Context())
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, items)
	})
	mux.HandleFunc("POST /api/channels", func(w http.ResponseWriter, r *http.Request) {
		var c store.Channel
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		if c.Name == "" {
			writeErr(w, 400, "name is required")
			return
		}
		c.ID = "ch-" + randHex(6)
		if c.OwnerID == "" {
			c.OwnerID = "human" // 本地单用户;接入登录后改为当前用户
		}
		c.EntityID = store.EntityOf(r.Context())
		if err := cs.st.CreateChannel(r.Context(), &c); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		// 创建者自动加入频道(当前 human)。
		_ = cs.st.AddChannelMember(r.Context(), c.ID, "human", "human")
		writeJSON(w, 201, c)
	})
	// 邀请 Human:给目标邮箱发实体加入邀请邮件(带品牌 HTML + 链接)。
	mux.HandleFunc("POST /api/invite-human", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Email      string `json:"email"`
			EntityID   string `json:"entityId"`
			EntityName string `json:"entityName"`
			InviteURL  string `json:"inviteUrl"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Email == "" || body.InviteURL == "" {
			writeErr(w, 400, "email and inviteUrl are required")
			return
		}
		if err := cs.SendInviteEmail(r.Context(), body.Email, body.EntityName, body.InviteURL); err != nil {
			slog.Error("send invite email", "email", body.Email, "err", err)
			writeErr(w, 500, "发送邀请邮件失败")
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})

	// 解散频道:仅 owner 可操作(连同成员与消息一并删除)。
	mux.HandleFunc("DELETE /api/channels/{id}", func(w http.ResponseWriter, r *http.Request) {
		ch, err := cs.st.GetChannel(r.Context(), r.PathValue("id"))
		if err != nil {
			writeErr(w, 404, "channel not found")
			return
		}
		// 本地单用户没有会话身份,actor 由客户端声明;接入登录后换成会话主体。
		actor := r.Header.Get("X-Actor")
		if actor == "" {
			actor = "human"
		}
		if ch.OwnerID != actor {
			writeErr(w, 403, "只有频道创建者可以解散频道")
			return
		}
		if err := cs.st.DeleteChannel(r.Context(), ch.ID); err != nil {
			slog.Error("delete channel", "channel", ch.ID, "err", err)
			writeErr(w, 500, "解散频道失败")
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})

	mux.HandleFunc("POST /api/channels/{id}/members", func(w http.ResponseWriter, r *http.Request) {
		var m struct {
			MemberID string `json:"memberId"`
			Kind     string `json:"kind"`
		}
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil || m.MemberID == "" || m.Kind == "" {
			writeErr(w, 400, "memberId and kind are required")
			return
		}
		if err := cs.st.AddChannelMember(r.Context(), r.PathValue("id"), m.MemberID, m.Kind); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("GET /api/channels/{id}/members", func(w http.ResponseWriter, r *http.Request) {
		items, err := cs.st.ListChannelMembers(r.Context(), r.PathValue("id"))
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, items)
	})
	mux.HandleFunc("DELETE /api/channels/{id}/members/{memberId}/{kind}", func(w http.ResponseWriter, r *http.Request) {
		if err := cs.st.RemoveChannelMember(r.Context(), r.PathValue("id"), r.PathValue("memberId"), r.PathValue("kind")); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("GET /api/channels/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		items, err := cs.st.ListChannelMessages(r.Context(), r.PathValue("id"), 200)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, items)
	})
	mux.HandleFunc("GET /api/channels/{id}/execution", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, cs.execution(r.PathValue("id")))
	})
	mux.HandleFunc("POST /api/channels/{id}/messages", cs.postMessage)
	// PRD §9.1 实时:频道消息 WS——订阅后推送新消息(客户端仍可轮询兜底)。
	mux.HandleFunc("GET /api/channels/{id}/events", cs.channelEventsWS)

	cs.registerEmailRoutes(mux)
}

// postMessage appends a user message; if the channel has an agent member, routes
// it to that agent (which executes on a machine and writes output.json), then
// appends the agent's reply.
func (cs *ChatService) postMessage(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("id")
	var body struct {
		Text string `json:"text"`
		// Attachments 引用已上传到本频道的文件(图片/视频/文档)。
		Attachments []struct {
			ID       string `json:"id"`
			Filename string `json:"filename"`
			Mime     string `json:"mime"`
		} `json:"attachments"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || (body.Text == "" && len(body.Attachments) == 0) {
		writeErr(w, 400, "text is required")
		return
	}
	ctx := r.Context()

	payload := map[string]any{"text": body.Text}
	if len(body.Attachments) > 0 {
		payload["attachments"] = body.Attachments
	}
	userMsg := &store.ChannelMessage{
		ID: "msg-" + randHex(8), ChannelID: channelID,
		AuthorMemberID: "human", AuthorKind: "human",
		IdempotencyKey: fmt.Sprintf("%s:human:%s", channelID, randHex(8)),
		PayloadJSON:    mustJSON(payload),
	}
	if err := cs.st.AppendChannelMessage(ctx, userMsg); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	cs.hub.publish(channelID, *userMsg)

	// 路由到 agent 成员:@mention 指定执行者;无 @ 则默认第一个 agent。异步执行。
	members, err := cs.st.ListChannelMembers(ctx, channelID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	// 路由:@mention 指定;无则按职责匹配分 + 忙闲度选择(B),LLM 路由(A)后续接入。
	agent := cs.routeAgent(body.Text, members)

	if agent != nil {
		background := store.WithEntity(context.Background(), store.EntityOf(ctx))
		background = store.WithOwner(background, store.OwnerOf(ctx))
		go cs.runAgentReply(background, channelID, agent, body.Text)
	}

	writeJSON(w, 200, map[string]any{"ok": true})
}

// runAgentReply executes an agent in the background and appends its reply.
// The chat shows the agent's conversational `reply` (or `summary`), NOT the raw
// output.json — output.json is the machine artifact, not the conversation.
func (cs *ChatService) runAgentReply(ctx context.Context, channelID string, agent *store.AgentSpec, task string) {
	cs.markBusy(agent.ID, 1)
	defer cs.markBusy(agent.ID, -1)
	out, aerr := cs.runAgent(ctx, channelID, agent, task)
	replyText := string(out)
	if aerr != nil {
		replyText = "⚠️ " + aerr.Error()
	} else {
		var parsed struct {
			Reply   string `json:"reply"`
			Summary string `json:"summary"`
		}
		if json.Unmarshal([]byte(out), &parsed) == nil {
			if parsed.Reply != "" {
				replyText = parsed.Reply
			} else if parsed.Summary != "" {
				replyText = parsed.Summary
			}
		}
	}
	agentMsg := &store.ChannelMessage{
		ID: "msg-" + randHex(8), ChannelID: channelID,
		AuthorMemberID: agent.ID, AuthorKind: "agent",
		IdempotencyKey: fmt.Sprintf("%s:agent:%s", channelID, randHex(8)),
		// task 随消息带回,前端失败时可用它「重试」。
		PayloadJSON: mustJSON(map[string]any{"text": replyText, "task": task}),
	}
	if err := cs.st.AppendChannelMessage(ctx, agentMsg); err != nil {
		slog.Warn("append agent channel message", "channel", channelID, "err", err)
	} else {
		cs.hub.publish(channelID, *agentMsg)
	}
}

// runAgent executes the agent on the first connected machine, in the channel's
// workspace, asking it to complete the task and write output.json. It records the
// daemon's live activity (messages/tool calls) into the channel's execution log
// so the frontend can show progress in a popup without disturbing the chat.
func (cs *ChatService) runAgent(ctx context.Context, channelID string, agent *store.AgentSpec, task string) (string, error) {
	runners := cs.link.RegisteredRunners()
	if len(runners) == 0 {
		return "", errors.New("没有在线的 machine")
	}
	runnerID := runners[0]
	// 每个 daemon(machine)独立 workspace:会话/消息/任务/记忆都在 wsRoot/<machine>/<agent>。
	ws := filepath.Join(cs.wsRoot, runnerID, agent.ID)
	if err := os.MkdirAll(ws, 0o755); err != nil {
		return "", err
	}
	spawnID := fmt.Sprintf("chat-%s-%d", agent.ID, cs.seq.Add(1))
	prompt := fmt.Sprintf("用户任务: %s\n\n请完成任务,并把结果写入 workspace 根目录的 output.json(单个 JSON 对象,含 ok、summary(执行摘要)与 reply(你用聊天口吻给用户的回复,直接回答用户,不要提 output.json))。", task)

	cs.appendExec(channelID, "spawn", "已调度 agent「"+agent.Name+"」执行任务…")
	if err := cs.g.Spawn(ctx, runnerID, gateway.Spawn{
		RunID: "chat", NodeID: agent.ID, Attempt: 1, SpawnID: spawnID,
		ExecutorType: agent.Runtime, Prompt: prompt, Cwd: ws, SystemPrompt: agent.SystemPrompt,
	}); err != nil {
		cs.appendExec(channelID, "error", "调度失败: "+err.Error())
		return "", fmt.Errorf("agent 调度失败: %w", err)
	}

	maxCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	for {
		select {
		case ev := <-cs.g.Events():
			if ev.RunnerID != runnerID {
				continue
			}
			var p struct {
				SpawnID string `json:"spawnId"`
				Message string `json:"message"`
				Event   struct {
					Type string `json:"type"`
					Text string `json:"text"`
					Name string `json:"name"`
				} `json:"event"`
			}
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				continue
			}
			if p.SpawnID != spawnID {
				continue
			}
			switch ev.Type {
			case "spawn-event":
				content := p.Event.Text
				if p.Event.Type == "tool" {
					content = "🔧 " + p.Event.Name
				}
				cs.appendExec(channelID, p.Event.Type, content)
			case "spawn-done":
				out, err := cs.link.ReadArtifact(ctx, runnerID, spawnID, "output.json")
				if err != nil {
					cs.appendExec(channelID, "error", "读取产物失败")
					return "", err
				}
				cs.appendExec(channelID, "done", "agent 执行完成")
				return string(out), nil
			case "spawn-error":
				cs.appendExec(channelID, "error", p.Message)
				return "", errors.New(p.Message)
			}
		case <-maxCtx.Done():
			cs.appendExec(channelID, "error", "执行超时")
			return "", maxCtx.Err()
		}
	}
}

// routeAgent picks the agent for a task when no @mention: role-match score
// (system prompt vs task keywords) first, then the least-busy agent among ties
// — so with several backend/frontend agents the most relevant idle one runs.
func (cs *ChatService) routeAgent(task string, members []store.ChannelMember) *store.AgentSpec {
	target := mentionName(task)
	var best *store.AgentSpec
	bestScore, bestBusy := -1, 1<<30
	for _, m := range members {
		if m.Kind != "agent" {
			continue
		}
		spec, err := cs.st.GetAgent(context.Background(), m.MemberID)
		if err != nil {
			continue
		}
		if target != "" && spec.Name == target {
			return spec
		}
		score := roleMatchScore(spec.SystemPrompt, task)
		busy := cs.busyOf(m.MemberID)
		if score > bestScore || (score == bestScore && busy < bestBusy) {
			best, bestScore, bestBusy = spec, score, busy
		}
	}
	return best
}

// roleMatchScore counts keyword overlap between an agent's system prompt and the
// task (bag-of-tokens; CJK tokens are bigrams).
func roleMatchScore(systemPrompt, task string) int {
	promptSet := tokenSet(systemPrompt)
	score := 0
	for t, c := range tokenSet(task) {
		score += c * promptSet[t]
	}
	return score
}

func tokenSet(s string) map[string]int {
	set := map[string]int{}
	var sb []rune
	flush := func() {
		if len(sb) >= 2 {
			for i := 0; i+1 < len(sb); i++ {
				set[string(sb[i:i+2])]++
			}
		} else if len(sb) == 1 {
			set[string(sb[0])]++
		}
		sb = sb[:0]
	}
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r > 127 {
			sb = append(sb, unicode.ToLower(r))
		} else {
			flush()
		}
	}
	flush()
	return set
}

func canManageAgent(actor string, a *store.AgentSpec) bool {
	if actor == "" {
		return true // 无身份上下文(本地模式)放行
	}
	return a.OwnerID == "" || a.OwnerID == actor
}

// retireAgent 注销数字人:正常注销生成交接文档并落库(可查),强制注销直接下线。
// 两种方式都会把 agent 移出活跃列表,并从其所在频道退出。
func (cs *ChatService) retireAgent(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Force     bool   `json:"force"`
		Confirm   string `json:"confirm"`   // 强制注销必须回填数字人名称
		Successor string `json:"successor"` // 接手人 agentID,可空(稍后再定)
		Reason    string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "请求体格式不正确")
		return
	}
	a, err := cs.st.GetAgent(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "agent not found")
		return
	}
	if a.Status == "retired" {
		writeErr(w, 409, "该数字人已注销")
		return
	}
	if body.Force && body.Confirm != a.Name {
		writeErr(w, 400, "强制注销需要输入数字人名称二次确认")
		return
	}

	doc := ""
	if !body.Force {
		doc = buildHandoverDoc(a, body.Successor, body.Reason)
	}
	if err := cs.st.RetireAgent(r.Context(), a.ID, doc); err != nil {
		slog.Error("retire agent", "agent", a.ID, "err", err)
		writeErr(w, 500, "注销失败")
		return
	}
	// 退出所有频道:注销后不应再被路由到任务。
	if channels, err := cs.st.ListChannels(r.Context()); err == nil {
		for _, ch := range channels {
			_ = cs.st.RemoveChannelMember(r.Context(), ch.ID, a.ID, "agent")
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true, "handoverDoc": doc})
}

// buildHandoverDoc 生成交接文档(§6.2.1 正常注销的产出物)。
func buildHandoverDoc(a *store.AgentSpec, successor, reason string) string {
	successorText := successor
	if successorText == "" {
		successorText = "(稍后指定)"
	}
	if reason == "" {
		reason = "(未填写)"
	}
	return fmt.Sprintf(`# 交接文档:%s

- 数字人 ID:%s
- 职责(系统提示词):%s
- 描述:%s
- 所属 machine:%s
- 运行时:%s / 模型:%s
- 接手人:%s
- 注销原因:%s
- 注销时间:%s
`, a.Name, a.ID, a.SystemPrompt, a.Description, a.MachineID, a.Runtime, a.Model,
		successorText, reason, time.Now().Format("2006-01-02 15:04:05"))
}

// mentionName extracts "@name" from a message (letters/digits/underscore).
func mentionName(text string) string {
	i := 0
	for i < len(text) {
		if text[i] != '@' {
			i++
			continue
		}
		start := i + 1
		j := start
		// 名字允许连字符(be-dev / fe-dev 是常用命名),否则 @be-dev 只会匹配到 "be"。
		for j < len(text) && (text[j] == '_' || text[j] == '-' || text[j] >= 'a' && text[j] <= 'z' || text[j] >= 'A' && text[j] <= 'Z' || text[j] >= '0' && text[j] <= '9') {
			j++
		}
		if j > start {
			return text[start:j]
		}
		i = j
	}
	return ""
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
