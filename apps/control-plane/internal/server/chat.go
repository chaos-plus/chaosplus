package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	st     *store.Store
	link   workflow.RunnerLink
	g      *gateway.Gateway
	wsRoot string
	seq    atomic.Int64

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
	return &ChatService{st: st, link: link, g: g, wsRoot: wsRoot, execLog: make(map[string][]ProgressEntry), agentBusy: make(map[string]int)}
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
