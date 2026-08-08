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
	"sync/atomic"
	"time"

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
	wsRoot string
	seq    atomic.Int64
}

func NewChatService(st *store.Store, link workflow.RunnerLink, wsRoot string) *ChatService {
	return &ChatService{st: st, link: link, wsRoot: wsRoot}
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
	mux.HandleFunc("GET /api/channels/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		items, err := cs.st.ListChannelMessages(r.Context(), r.PathValue("id"), 200)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, items)
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

	// Route to the first agent member if any.
	members, err := cs.st.ListChannelMembers(ctx, channelID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var agent *store.AgentSpec
	for _, m := range members {
		if m.Kind == "agent" {
			if spec, e := cs.st.GetAgent(ctx, m.MemberID); e == nil {
				agent = spec
				break
			}
		}
	}

	if agent != nil {
		replyText, aerr := cs.runAgent(ctx, agent, body.Text)
		if aerr != nil {
			replyText = "⚠️ " + aerr.Error()
		}
		agentMsg := &store.ChannelMessage{
			ID: "msg-" + randHex(8), ChannelID: channelID,
			AuthorMemberID: agent.ID, AuthorKind: "agent",
			IdempotencyKey: fmt.Sprintf("%s:agent:%s", channelID, randHex(8)),
			PayloadJSON:    mustJSON(map[string]any{"text": replyText}),
		}
		_ = cs.st.AppendChannelMessage(ctx, agentMsg)
	}

	writeJSON(w, 200, map[string]any{"ok": true})
}

// runAgent executes the agent on the first connected machine, in the channel's
// workspace, asking it to complete the task and write output.json.
func (cs *ChatService) runAgent(ctx context.Context, agent *store.AgentSpec, task string) (string, error) {
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
	prompt := fmt.Sprintf("用户任务: %s\n\n请完成任务,并把结果写入 workspace 根目录的 output.json(单个 JSON 对象,含 ok 与 summary 字段)。", task)
	res, err := cs.link.SpawnAndWait(ctx, runnerID, gateway.Spawn{
		RunID: "chat", NodeID: agent.ID, Attempt: 1, SpawnID: spawnID,
		ExecutorType: agent.Runtime, Prompt: prompt, Cwd: ws, SystemPrompt: agent.SystemPrompt,
	}, 0, 5*time.Minute)
	if err != nil {
		return "", fmt.Errorf("agent 执行失败: %w", err)
	}
	if !res.OK {
		return "", fmt.Errorf("agent 退出码 %d: %s", res.ExitCode, res.Error)
	}
	out, err := cs.link.ReadArtifact(ctx, runnerID, spawnID, "output.json")
	if err != nil {
		return "", err
	}
	return string(out), nil
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
