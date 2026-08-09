package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/gateway"
	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/store"
	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/workflow"
)

// ChatService owns the 会话区 domain: team agents, channels, members, and the
// message log. A user message in a channel that has an agent member is routed to
// that agent, which executes on a connected machine (claude/codex) and writes
// its deliverable to output.json in the channel's workspace; the result is
// appended back into the channel as the agent's reply (PRD §9, §23).
type ChatService struct {
	st       *store.Store
	link     workflow.RunnerLink
	g        *gateway.Gateway
	rm       *RunManager
	runnerID string
	wsRoot   string
	seq      atomic.Int64

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

func NewChatService(st *store.Store, link workflow.RunnerLink, g *gateway.Gateway, rm *RunManager, runnerID, wsRoot string) *ChatService {
	return &ChatService{st: st, link: link, g: g, rm: rm, runnerID: runnerID, wsRoot: wsRoot, execLog: make(map[string][]ProgressEntry), agentBusy: make(map[string]int)}
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
	return append([]ProgressEntry(nil), cs.execLog[channelID]...)
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
		if err := cs.st.UpdateAgent(r.Context(), &a); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, a)
	})
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
		if err := cs.st.CreateChannel(r.Context(), &c); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		// 创建者自动加入频道(当前 human)。
		_ = cs.st.AddChannelMember(r.Context(), c.ID, "human", "human")
		writeJSON(w, 201, c)
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

	// ---- 工作区 work-items(需求/任务/测试/缺陷)+ 执行 + 附件 + OKR + 群聊订阅 ----
	mux.HandleFunc("GET /api/work-items", func(w http.ResponseWriter, r *http.Request) {
		items, err := cs.st.ListWorkItems(r.Context(), r.URL.Query().Get("type"), r.URL.Query().Get("status"), r.URL.Query().Get("parent"))
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, items)
	})
	mux.HandleFunc("POST /api/work-items", cs.createWorkItem)
	mux.HandleFunc("PUT /api/work-items/{id}", cs.updateWorkItem)
	mux.HandleFunc("DELETE /api/work-items/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := cs.st.DeleteWorkItem(r.Context(), r.PathValue("id")); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /api/work-items/{id}/execute", cs.executeWorkItem)
	mux.HandleFunc("GET /api/work-items/{id}/attachments", func(w http.ResponseWriter, r *http.Request) {
		items, err := cs.st.ListAttachments(r.Context(), "work_item", r.PathValue("id"))
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, items)
	})
	mux.HandleFunc("POST /api/work-items/{id}/attachments", cs.uploadAttachment)
	mux.HandleFunc("GET /api/attachments/{id}", cs.serveAttachment)
	mux.HandleFunc("GET /api/okrs", func(w http.ResponseWriter, r *http.Request) {
		items, err := cs.st.ListOkrs(r.Context())
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, items)
	})
	mux.HandleFunc("POST /api/okrs", cs.createOkr)
	mux.HandleFunc("PUT /api/okrs/{id}", cs.updateOkr)
	mux.HandleFunc("DELETE /api/okrs/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := cs.st.DeleteOkr(r.Context(), r.PathValue("id")); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /api/channels/{id}/work-items", cs.createWorkItemFromChannel)
}

// createWorkItem 创建工作项;关联频道时向群聊推送订阅通知。
func (cs *ChatService) createWorkItem(w http.ResponseWriter, r *http.Request) {
	var body store.WorkItem
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Title == "" {
		writeErr(w, 400, "title is required")
		return
	}
	if body.Type == "" {
		body.Type = "task"
	}
	body.ID = "wi-" + randHex(8)
	if err := cs.st.CreateWorkItem(r.Context(), &body); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	cs.notifyWorkItemChange(r.Context(), &body, "created")
	writeJSON(w, 201, body)
}

func (cs *ChatService) createWorkItemFromChannel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Type        string `json:"type"`
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Title == "" {
		writeErr(w, 400, "title is required")
		return
	}
	it := store.WorkItem{ID: "wi-" + randHex(8), Type: body.Type, Title: body.Title, Description: body.Description, ChannelID: r.PathValue("id")}
	if it.Type == "" {
		it.Type = "task"
	}
	if err := cs.st.CreateWorkItem(r.Context(), &it); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	cs.notifyWorkItemChange(r.Context(), &it, "created")
	writeJSON(w, 201, it)
}

func (cs *ChatService) updateWorkItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, err := cs.st.GetWorkItem(r.Context(), id)
	if err != nil {
		writeErr(w, 404, "work item not found")
		return
	}
	var body store.WorkItem
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if body.Title == "" {
		body.Title = existing.Title
	}
	body.ID = existing.ID
	if body.ChannelID == "" {
		body.ChannelID = existing.ChannelID
	}
	changed := body.Status != existing.Status
	if err := cs.st.UpdateWorkItem(r.Context(), &body); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if changed {
		cs.notifyWorkItemChange(r.Context(), &body, "status")
	}
	writeJSON(w, 200, body)
}

// notifyWorkItemChange 把工作项变化推送到关联的群聊频道(human/agent 实时跟进)。
func (cs *ChatService) notifyWorkItemChange(ctx context.Context, it *store.WorkItem, reason string) {
	if it.ChannelID == "" {
		return
	}
	text := fmt.Sprintf("📌 %s #%s「%s」", workItemTypeLabel(it.Type), it.ID, it.Title)
	switch reason {
	case "created":
		text += " 已创建(" + workItemStatusLabel(it.Status) + ")"
	case "status":
		text += " 状态 → " + workItemStatusLabel(it.Status)
	}
	_ = cs.st.AppendChannelMessage(ctx, &store.ChannelMessage{
		ID: "msg-" + randHex(8), ChannelID: it.ChannelID,
		AuthorMemberID: "workflow", AuthorKind: "workflow",
		IdempotencyKey: fmt.Sprintf("%s:workflow:%s", it.ChannelID, randHex(8)),
		PayloadJSON:    mustJSON(map[string]any{"text": text}),
	})
}

func workItemTypeLabel(t string) string {
	switch t {
	case "requirement":
		return "需求"
	case "bug":
		return "缺陷"
	default:
		return "任务"
	}
}

func workItemStatusLabel(s string) string {
	switch s {
	case "in_progress":
		return "进行中"
	case "review":
		return "评审中"
	case "done":
		return "已完成"
	default:
		return "待办"
	}
}

// postMessage appends a user message; if the channel has an agent member, routes
// it to that agent (which executes on a machine and writes output.json), then
// appends the agent's reply.
func (cs *ChatService) postMessage(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("id")
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Text == "" {
		writeErr(w, 400, "text is required")
		return
	}
	ctx := r.Context()

	userMsg := &store.ChannelMessage{
		ID: "msg-" + randHex(8), ChannelID: channelID,
		AuthorMemberID: "human", AuthorKind: "human",
		IdempotencyKey: fmt.Sprintf("%s:human:%s", channelID, randHex(8)),
		PayloadJSON:    mustJSON(map[string]any{"text": body.Text}),
	}
	if err := cs.st.AppendChannelMessage(ctx, userMsg); err != nil {
		writeErr(w, 500, err.Error())
		return
	}

	// 路由到 agent 成员:@mention 指定执行者;无 @ 则默认第一个 agent。异步执行。
	members, err := cs.st.ListChannelMembers(ctx, channelID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	// 路由:@mention 指定;无则按职责匹配分 + 忙闲度选择(B),LLM 路由(A)后续接入。
	agent := cs.routeAgent(body.Text, members)

	if agent != nil {
		go cs.runAgentReply(context.Background(), channelID, agent, body.Text)
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
	_ = cs.st.AppendChannelMessage(ctx, agentMsg)
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
	ws := filepath.Join(cs.wsRoot, agent.ID)
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

// executeWorkItem 用工作项上下文发起一个 workflow run,并把进度/状态投影回工作项。
func (cs *ChatService) executeWorkItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	it, err := cs.st.GetWorkItem(r.Context(), id)
	if err != nil {
		writeErr(w, 404, "work item not found")
		return
	}
	if it.Status == "in_progress" && it.WorkflowRunID != "" {
		writeErr(w, 409, "该工作项正在执行中")
		return
	}
	def, ctxJSON := taskWorkflowDef(it)
	defJSON, _ := json.Marshal(def)
	workspace := filepath.Join(cs.wsRoot, "workitem-"+it.ID)
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	run, err := cs.rm.Launch(r.Context(), LaunchRequest{WorkflowJSON: defJSON, Context: ctxJSON, Workspace: workspace, RunnerID: cs.runnerID})
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	it.Status = "in_progress"
	it.WorkflowRunID = run.ID
	_ = cs.st.UpdateWorkItem(r.Context(), it)
	cs.notifyWorkItemChange(r.Context(), it, "status")
	go cs.trackRunProgress(it, run)
	writeJSON(w, 200, map[string]any{"runId": run.ID})
}

// trackRunProgress 轮询 run 直到终态,把 progress/status/spent_hours 投影回 work_items
// (RunManager 是内存态,必须持久化投影,事件表仍是重放真相)。
func (cs *ChatService) trackRunProgress(it *store.WorkItem, run *Run) {
	start := time.Now()
	ctx := context.Background()
	for {
		time.Sleep(2 * time.Second)
		st := run.Status()
		if st == RunCompleted || st == RunFailed || st == RunPaused {
			break
		}
		evs := run.Events()
		var done, total int
		seen := map[string]bool{}
		for _, ev := range evs {
			if ev.NodeID == "" || seen[ev.NodeID] {
				continue
			}
			seen[ev.NodeID] = true
			total++
			if ev.Status == workflow.StatusCompleted || ev.Status == workflow.StatusFailed {
				done++
			}
		}
		progress := 0
		if total > 0 {
			progress = done * 100 / total
		}
		_ = cs.st.UpdateWorkItemRun(ctx, it.ID, progress, "in_progress", time.Since(start).Hours())
	}
	final := "done"
	if run.Status() == RunFailed || run.Status() == RunPaused {
		final = "review"
	}
	_ = cs.st.UpdateWorkItemRun(ctx, it.ID, 100, final, time.Since(start).Hours())
	it.Status = final
	cs.notifyWorkItemChange(ctx, it, "status")
}

// taskWorkflowDef 构造「trigger→agent(完成任务)」的静态 DAG,以工作项为上下文。
func taskWorkflowDef(it *store.WorkItem) (map[string]any, json.RawMessage) {
	def := map[string]any{
		"id": "task-" + it.ID, "version": "1",
		"nodes": []map[string]any{
			{"id": "trigger", "type": "trigger", "trigger": map[string]any{"source": "manual"}},
			{"id": "do", "type": "agent", "agent": map[string]any{
				"id": "do", "role": "executor", "executor": "claude",
				"systemPrompt": "你是任务执行 agent。根据 context 里的任务要求完成工作,并把结果写入 workspace 根目录的 output.json(含 ok、summary、reply)。",
			}},
		},
		"edges": []map[string]any{{"from": "trigger", "to": "do"}},
	}
	ctx, _ := json.Marshal(map[string]any{"title": it.Title, "description": it.Description, "type": it.Type})
	return def, ctx
}

// uploadAttachment 接收 multipart 文件,存控制面 ARTIFACT_ROOT,元数据落库。
func (cs *ChatService) uploadAttachment(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(20 << 20); err != nil { // 20MB
		writeErr(w, 400, "bad multipart: "+err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "file field required")
		return
	}
	defer file.Close()
	root := os.Getenv("ARTIFACT_ROOT")
	if root == "" {
		root = filepath.Join(cs.wsRoot, "attachments")
	}
	id := "att-" + randHex(8)
	dir := filepath.Join(root, "attachments")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	path := filepath.Join(dir, id)
	dst, err := os.Create(path)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	n, err := io.Copy(dst, file)
	dst.Close()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	a := &store.Attachment{
		ID: id, OwnerType: "work_item", OwnerID: r.PathValue("id"),
		Filename: header.Filename, Mime: header.Header.Get("Content-Type"),
		SizeBytes: n, StorePath: path,
	}
	if a.Mime == "" {
		a.Mime = "application/octet-stream"
	}
	if err := cs.st.CreateAttachment(r.Context(), a); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, a)
}

// serveAttachment 按 id 返回文件(路径仅来自 DB,杜绝客户端路径注入)。
func (cs *ChatService) serveAttachment(w http.ResponseWriter, r *http.Request) {
	a, err := cs.st.GetAttachment(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "attachment not found")
		return
	}
	w.Header().Set("Content-Type", a.Mime)
	http.ServeFile(w, r, a.StorePath)
}

func (cs *ChatService) createOkr(w http.ResponseWriter, r *http.Request) {
	var o store.Okr
	if err := json.NewDecoder(r.Body).Decode(&o); err != nil || o.Title == "" {
		writeErr(w, 400, "title is required")
		return
	}
	o.ID = "okr-" + randHex(6)
	if o.KeyResults == "" {
		o.KeyResults = "[]"
	}
	if err := cs.st.CreateOkr(r.Context(), &o); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, o)
}

func (cs *ChatService) updateOkr(w http.ResponseWriter, r *http.Request) {
	var o store.Okr
	if err := json.NewDecoder(r.Body).Decode(&o); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	o.ID = r.PathValue("id")
	if err := cs.st.UpdateOkr(r.Context(), &o); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, o)
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
		for j < len(text) && (text[j] == '_' || text[j] >= 'a' && text[j] <= 'z' || text[j] >= 'A' && text[j] <= 'Z' || text[j] >= '0' && text[j] <= '9') {
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
