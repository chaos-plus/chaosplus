package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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
	mux.HandleFunc("GET /api/work-items/{id}", func(w http.ResponseWriter, r *http.Request) {
		it, err := cs.st.GetWorkItem(r.Context(), r.PathValue("id"))
		if err != nil {
			writeErr(w, 404, "work item not found")
			return
		}
		writeJSON(w, 200, it)
	})
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
	mux.HandleFunc("POST /api/channels/{id}/attachments", cs.uploadAttachmentFor("channel"))
	mux.HandleFunc("GET /api/channels/{id}/attachments", func(w http.ResponseWriter, r *http.Request) {
		items, err := cs.st.ListAttachments(r.Context(), "channel", r.PathValue("id"))
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, items)
	})
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
	body.EntityID = store.EntityOf(r.Context())
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

// updateWorkItem 是部分更新:只覆盖请求里显式出现的字段。
// 用指针接收 —— 否则一个只带 {status} 的 PUT 会把 estimate/progress/spent/
// parent 等一并清零(前端正是这么调的),并冲掉正在执行的 run 投影。
func (cs *ChatService) updateWorkItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, err := cs.st.GetWorkItem(r.Context(), id)
	if err != nil {
		writeErr(w, 404, "work item not found")
		return
	}
	var patch struct {
		Type          *string  `json:"type"`
		Title         *string  `json:"title"`
		Description   *string  `json:"description"`
		Status        *string  `json:"status"`
		ParentID      *string  `json:"parentId"`
		EstimateHours *float64 `json:"estimateHours"`
		SpentHours    *float64 `json:"spentHours"`
		Progress      *int     `json:"progress"`
		WorkflowRunID *string  `json:"workflowRunId"`
		AssigneeAgent *string  `json:"assigneeAgent"`
		ChannelID     *string  `json:"channelId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeErr(w, 400, "请求体格式不正确")
		return
	}

	updated := *existing
	if patch.Type != nil {
		updated.Type = *patch.Type
	}
	if patch.Title != nil && *patch.Title != "" {
		updated.Title = *patch.Title
	}
	if patch.Description != nil {
		updated.Description = *patch.Description
	}
	if patch.Status != nil && *patch.Status != "" {
		updated.Status = *patch.Status
	}
	if patch.ParentID != nil {
		// 不允许自引用,否则前端树递归会成环。
		if *patch.ParentID == updated.ID {
			writeErr(w, 400, "工作项不能以自己为父级")
			return
		}
		updated.ParentID = *patch.ParentID
	}
	if patch.EstimateHours != nil {
		updated.EstimateHours = *patch.EstimateHours
	}
	if patch.SpentHours != nil {
		updated.SpentHours = *patch.SpentHours
	}
	if patch.Progress != nil {
		updated.Progress = *patch.Progress
	}
	if patch.WorkflowRunID != nil {
		updated.WorkflowRunID = *patch.WorkflowRunID
	}
	if patch.AssigneeAgent != nil {
		updated.AssigneeAgent = *patch.AssigneeAgent
	}
	if patch.ChannelID != nil {
		updated.ChannelID = *patch.ChannelID
	}

	changed := updated.Status != existing.Status
	if err := cs.st.UpdateWorkItem(r.Context(), &updated); err != nil {
		slog.Error("update work item", "id", id, "err", err)
		writeErr(w, 500, "更新工作项失败")
		return
	}
	if changed {
		cs.notifyWorkItemChange(r.Context(), &updated, "status")
	}
	writeJSON(w, 200, updated)
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
		slog.Error("workitem workspace", "err", err)
		writeErr(w, 500, "创建工作目录失败")
		return
	}
	// 用 Background:HTTP 处理器返回后 run 仍需继续(r.Context() 会取消它)。
	run, err := cs.rm.Launch(context.Background(), LaunchRequest{WorkflowJSON: defJSON, Context: ctxJSON, Workspace: workspace, RunnerID: cs.runnerID})
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
// relayRunEvents 把 run 的新事件回灌到关联频道(PRD D.5:工作流事件 + 审批卡片)。
// 返回已转发到的事件序号,调用方持有游标避免重复推送。
func (cs *ChatService) relayRunEvents(ctx context.Context, it *store.WorkItem, run *Run, cursor int) int {
	if it.ChannelID == "" {
		return cursor
	}
	for _, ev := range run.Events() {
		if ev.Seq <= cursor {
			continue
		}
		cursor = ev.Seq
		payload := map[string]any{"runId": run.ID, "nodeId": ev.NodeID, "workItemId": it.ID}
		switch {
		case ev.Status == workflow.StatusWaitingApproval && ev.NodeID != "":
			// 审批卡片:标题(节点)、摘要、artifact 链接、通过/拒绝按钮由前端渲染。
			payload["kind"] = "approval"
			payload["text"] = fmt.Sprintf("节点「%s」等待人工审批", ev.NodeID)
			payload["summary"] = it.Title
			payload["artifacts"] = runArtifacts(run, ev.NodeID)
		case ev.NodeID == "" && ev.Status == workflow.StatusRunning:
			payload["kind"] = "run"
			payload["text"] = fmt.Sprintf("▶ 工作流已启动(%s)", run.ID)
		case ev.Status == workflow.StatusFailed:
			payload["kind"] = "run"
			payload["text"] = fmt.Sprintf("✖ 节点「%s」失败:%s", ev.NodeID, ev.Error)
		case ev.NodeID != "" && ev.Status == workflow.StatusCompleted:
			payload["kind"] = "run"
			payload["text"] = fmt.Sprintf("✔ 节点「%s」完成", ev.NodeID)
		default:
			continue // 其余中间态不刷屏
		}
		if err := cs.st.AppendChannelMessage(ctx, &store.ChannelMessage{
			ID: "msg-" + randHex(8), ChannelID: it.ChannelID,
			AuthorMemberID: "workflow", AuthorKind: "workflow",
			IdempotencyKey: fmt.Sprintf("%s:%s:%d", it.ChannelID, run.ID, ev.Seq),
			PayloadJSON:    mustJSON(payload),
		}); err != nil {
			slog.Warn("relay run event to channel", "run", run.ID, "seq", ev.Seq, "err", err)
		}
	}
	return cursor
}

// runArtifacts 列出送审内容:审批节点自己不产出 artifact,要看它的上游节点
// 产了什么(PRD D.5 审批卡片的 artifact 链接列表)。
func runArtifacts(run *Run, nodeID string) []string {
	out := []string{}
	if run.Def == nil {
		return out
	}
	upstream := map[string]bool{nodeID: true}
	for _, e := range run.Def.Edges {
		if e.To == nodeID {
			upstream[e.From] = true
		}
	}
	seen := map[string]bool{}
	for i := range run.Def.Nodes {
		n := &run.Def.Nodes[i]
		if !upstream[n.ID] || n.Agent == nil || n.Agent.OutputSpec == nil {
			continue
		}
		for _, p := range n.Agent.OutputSpec.Produces {
			if p.Path == "" || seen[p.Path] {
				continue
			}
			seen[p.Path] = true
			out = append(out, p.Path)
		}
	}
	return out
}

func (cs *ChatService) trackRunProgress(it *store.WorkItem, run *Run) {
	start := time.Now()
	ctx := context.Background()
	lastProgress := 0
	relayed := 0
	// 绝对上限:run 卡住(节点无超时、审批无人处理)时不能让这个 goroutine
	// 连同每 2s 一次的写库永远活着。
	deadline := start.Add(trackRunMaxDuration)
	for {
		time.Sleep(2 * time.Second)
		st := run.Status()
		if st == RunCompleted || st == RunFailed || st == RunPaused {
			break
		}
		if time.Now().After(deadline) {
			slog.Warn("stop tracking run: exceeded max duration", "run", run.ID, "workItem", it.ID)
			if err := cs.st.UpdateWorkItemRun(ctx, it.ID, lastProgress, "review", time.Since(start).Hours()); err != nil {
				slog.Error("project stalled run", "workItem", it.ID, "err", err)
			}
			it.Status = "review"
			cs.notifyWorkItemChange(ctx, it, "status")
			return
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
		relayed = cs.relayRunEvents(ctx, it, run, relayed)
		lastProgress = progress
		if err := cs.st.UpdateWorkItemRun(ctx, it.ID, progress, "in_progress", time.Since(start).Hours()); err != nil {
			slog.Error("project run progress", "workItem", it.ID, "err", err)
		}
	}
	final, finalProgress := "done", 100
	if run.Status() == RunFailed || run.Status() == RunPaused {
		// 失败/暂停不是 100%:保留最后一次真实进度,否则父项汇总也会被抬高。
		final, finalProgress = "review", lastProgress
	}
	cs.relayRunEvents(ctx, it, run, relayed)
	spent := time.Since(start).Hours()
	if err := cs.st.UpdateWorkItemRun(ctx, it.ID, finalProgress, final, spent); err != nil {
		slog.Error("project run result", "workItem", it.ID, "err", err)
	}
	if final == "done" {
		if err := cs.st.CalibrateEstimate(ctx, it.ID, spent); err != nil { // 未人工估时 → 用实际耗时校准
			slog.Warn("calibrate estimate", "workItem", it.ID, "err", err)
		}
	}
	if err := cs.st.RollupParent(ctx, it.ParentID); err != nil { // 子任务工时/进度汇总到父项
		slog.Warn("rollup parent", "parent", it.ParentID, "err", err)
	}
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
				"outputSpec": map[string]any{
					"produces": []map[string]any{{"id": "output", "path": "output.json", "type": "document"}},
				},
			}},
			// 终审节点:PRD V1-M2 闸门要求每次执行完成一次人工审批;拒绝按 §13
			// 必填结构化反馈,run 暂停,工作项回到「评审中」。
			{"id": "review", "type": "human_approval", "humanApproval": map[string]any{
				"approvers": "any_human", "timeoutMs": 86400000, "onTimeout": "pause", "onReject": "pause",
			}},
		},
		"edges": []map[string]any{
			{"from": "trigger", "to": "do"},
			{"from": "do", "to": "review"},
		},
	}
	ctx, _ := json.Marshal(map[string]any{"title": it.Title, "description": it.Description, "type": it.Type})
	return def, ctx
}

// uploadAttachment 接收 multipart 文件,存控制面 ARTIFACT_ROOT,元数据落库。
// ownerType 决定归属:work_item(工作项描述)或 channel(聊天引用)。
func (cs *ChatService) uploadAttachmentFor(ownerType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { cs.storeUpload(w, r, ownerType) }
}

func (cs *ChatService) uploadAttachment(w http.ResponseWriter, r *http.Request) {
	cs.storeUpload(w, r, "work_item")
}

// maxUploadBytes caps a single upload. ParseMultipartForm 的参数只是内存缓冲上限,
// 不限制落盘大小 —— 必须用 MaxBytesReader 真正封顶,否则可被写满磁盘。
const maxUploadBytes = 20 << 20

// trackRunProgress 的绝对上限:超过就停止跟踪并把工作项置为待人工处理。
const trackRunMaxDuration = 6 * time.Hour

// safeMime 决定回放时用什么 Content-Type。客户端声明的 mime 不可信:允许
// text/html 或 image/svg+xml 同源回放 = 存储型 XSS。只放行确定安全的类型,
// 其余一律当二进制附件下载。
func safeMime(filename string) (mime string, inline bool) {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".png":
		return "image/png", true
	case ".jpg", ".jpeg":
		return "image/jpeg", true
	case ".gif":
		return "image/gif", true
	case ".webp":
		return "image/webp", true
	case ".mp4":
		return "video/mp4", true
	case ".webm":
		return "video/webm", true
	case ".pdf":
		return "application/pdf", true
	default:
		return "application/octet-stream", false
	}
}

func (cs *ChatService) storeUpload(w http.ResponseWriter, r *http.Request, ownerType string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		writeErr(w, 400, "上传失败:文件过大或格式不正确")
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
		slog.Error("attachment dir", "err", err)
		writeErr(w, 500, "存储附件失败")
		return
	}
	path := filepath.Join(dir, id)
	dst, err := os.Create(path)
	if err != nil {
		slog.Error("create attachment", "err", err)
		writeErr(w, 500, "存储附件失败")
		return
	}
	n, err := io.Copy(dst, file)
	dst.Close()
	if err != nil {
		_ = os.Remove(path) // 半截文件不留在磁盘上
		slog.Error("write attachment", "err", err)
		writeErr(w, 500, "存储附件失败")
		return
	}
	mime, _ := safeMime(header.Filename)
	a := &store.Attachment{
		ID: id, OwnerType: ownerType, OwnerID: r.PathValue("id"),
		Filename: header.Filename, Mime: mime,
		SizeBytes: n, StorePath: path,
	}
	if err := cs.st.CreateAttachment(r.Context(), a); err != nil {
		_ = os.Remove(path) // 元数据没落库 → 文件成孤儿,删掉
		slog.Error("persist attachment", "err", err)
		writeErr(w, 500, "存储附件失败")
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
	mime, inline := safeMime(a.Filename)
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	disposition := "attachment"
	if inline {
		disposition = "inline"
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename*=UTF-8''%s", disposition, url.PathEscape(a.Filename)))
	http.ServeFile(w, r, a.StorePath)
}

// validateKeyResults 拒绝形状不对的 KR —— 存进去就会让前端进度计算出 NaN。
func validateKeyResults(raw string) error {
	if raw == "" {
		return nil
	}
	var krs []struct {
		Title    string   `json:"title"`
		Target   *float64 `json:"target"`
		Progress *float64 `json:"progress"`
		Unit     string   `json:"unit"`
	}
	if err := json.Unmarshal([]byte(raw), &krs); err != nil {
		return fmt.Errorf("keyResults 必须是 [{title,target,progress,unit}] 数组")
	}
	for i, k := range krs {
		if k.Title == "" {
			return fmt.Errorf("keyResults[%d].title 不能为空", i)
		}
		if k.Target == nil || k.Progress == nil {
			return fmt.Errorf("keyResults[%d] 需要数值型 target 与 progress", i)
		}
	}
	return nil
}

func (cs *ChatService) createOkr(w http.ResponseWriter, r *http.Request) {
	var o store.Okr
	if err := json.NewDecoder(r.Body).Decode(&o); err != nil || o.Title == "" {
		writeErr(w, 400, "title is required")
		return
	}
	o.ID = "okr-" + randHex(6)
	o.EntityID = store.EntityOf(r.Context())
	if o.KeyResults == "" {
		o.KeyResults = "[]"
	}
	if err := validateKeyResults(o.KeyResults); err != nil {
		writeErr(w, 400, err.Error())
		return
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
	if err := validateKeyResults(o.KeyResults); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := cs.st.UpdateOkr(r.Context(), &o); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, o)
}

// canManageAgent 校验 actor 是否有权编辑/管理该 agent(仅 owner)。
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
