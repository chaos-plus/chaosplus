package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/gateway"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/store"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/workflow"
)

// chatFakeRunner answers spawn commands over real NATS, writes the artifact the
// chat path reads back, and reports tool + done events on the runner's subject.
func chatFakeRunner(t *testing.T, nc *nats.Conn, runnerID, artifactDir string, ok bool, reply string) {
	t.Helper()
	cmdSubj := "chaos.runner." + runnerID + ".cmd"
	evtSubj := "chaos.runner." + runnerID + ".evt"
	_, err := nc.Subscribe(cmdSubj, func(m *nats.Msg) {
		var cmd struct {
			Type  string `json:"type"`
			Path  string `json:"path"`
			Spawn struct {
				SpawnID string `json:"spawnId"`
				Cwd     string `json:"cwd"`
			} `json:"spawn"`
		}
		if err := json.Unmarshal(m.Data, &cmd); err != nil {
			_ = m.Respond([]byte(`{"ok":false,"data":{"error":"bad payload"}}`))
			return
		}
		if cmd.Type == "read-file" {
			// 真实 daemon 从 workspace 读文件后回传内容。
			body, err := os.ReadFile(filepath.Join(artifactDir, cmd.Path))
			if err != nil {
				_ = m.Respond([]byte(`{"ok":false,"data":{"error":"not found"}}`))
				return
			}
			resp, _ := json.Marshal(map[string]any{"ok": true, "data": map[string]any{"content": string(body)}})
			_ = m.Respond(resp)
			return
		}
		if cmd.Type != "spawn" {
			_ = m.Respond([]byte(`{"ok":true}`))
			return
		}
		_ = m.Respond([]byte(`{"ok":true}`))
		if ok {
			out, _ := json.Marshal(map[string]any{"ok": true, "summary": "done", "reply": reply})
			_ = os.MkdirAll(artifactDir, 0o755)
			_ = os.WriteFile(filepath.Join(artifactDir, "output.json"), out, 0o644)
		}
		// 调用方在 Spawn 返回后才开始收事件,立刻发会丢。
		go func() {
			time.Sleep(150 * time.Millisecond)
			tool, _ := json.Marshal(map[string]any{
				"type": "spawn-event", "spawnId": cmd.Spawn.SpawnID,
				"event": map[string]any{"type": "tool", "name": "Write"},
			})
			_ = nc.Publish(evtSubj, tool)
			if ok {
				d, _ := json.Marshal(map[string]any{"type": "spawn-done", "spawnId": cmd.Spawn.SpawnID, "ok": true, "exitCode": 0})
				_ = nc.Publish(evtSubj, d)
				return
			}
			e, _ := json.Marshal(map[string]any{"type": "spawn-error", "spawnId": cmd.Spawn.SpawnID, "message": "agent crashed"})
			_ = nc.Publish(evtSubj, e)
		}()
	})
	if err != nil {
		t.Fatalf("fake runner subscribe: %v", err)
	}
}

func newChatOverNats(t *testing.T, ok bool, reply string) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	nc := startTestNATS(t)
	g := gateway.New(nc)
	ctx, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)
	go func() { _ = g.Start(ctx) }() // Start 是阻塞循环
	time.Sleep(150 * time.Millisecond)

	artifactDir := t.TempDir()
	chatFakeRunner(t, nc, "runner-1", artifactDir, ok, reply)
	if _, err := nc.Request("chaos.runner.register", []byte(`{"runnerId":"runner-1"}`), 2*time.Second); err != nil {
		t.Fatalf("register runner: %v", err)
	}

	cs := NewChatService(st, &workflow.NatsRunnerLink{G: g}, g, t.TempDir())
	mux := http.NewServeMux()
	cs.register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, st
}

func seedChannelWithAgent(t *testing.T, srv *httptest.Server, agentName string) string {
	t.Helper()
	var ag store.AgentSpec
	doJSON(t, "POST", srv.URL+"/api/agents", map[string]any{"name": agentName, "runtime": "claude", "systemPrompt": "后端"}, &ag)
	var ch store.Channel
	doJSON(t, "POST", srv.URL+"/api/channels", map[string]any{"name": "dev"}, &ch)
	doJSON(t, "POST", srv.URL+"/api/channels/"+ch.ID+"/members", map[string]any{"memberId": ag.ID, "kind": "agent"}, nil)
	return ch.ID
}

func agentMessages(t *testing.T, st *store.Store, channelID string) []map[string]any {
	t.Helper()
	waitFor(t, func() bool {
		msgs, err := st.ListChannelMessages(context.Background(), channelID, 50)
		if err != nil {
			return false
		}
		for _, m := range msgs {
			if m.AuthorKind == "agent" {
				return true
			}
		}
		return false
	}, 30*time.Second)

	msgs, _ := st.ListChannelMessages(context.Background(), channelID, 50)
	out := []map[string]any{}
	for _, m := range msgs {
		if m.AuthorKind != "agent" {
			continue
		}
		var p map[string]any
		_ = json.Unmarshal([]byte(m.PayloadJSON), &p)
		out = append(out, p)
	}
	return out
}

func TestAgentRepliesInChannelAndRecordsExecutionLog(t *testing.T) {
	srv, st := newChatOverNats(t, true, "已完成:创建了 hello.py")
	ch := seedChannelWithAgent(t, srv, "be-dev")

	if code := doJSON(t, "POST", srv.URL+"/api/channels/"+ch+"/messages", map[string]any{"text": "写个 hello"}, nil); code != 200 {
		t.Fatalf("post: %d", code)
	}

	replies := agentMessages(t, st, ch)
	if len(replies) == 0 {
		t.Fatal("no agent reply")
	}
	// 聊天里显示 reply 字段,而不是原始 output.json。
	if text, _ := replies[0]["text"].(string); text != "已完成:创建了 hello.py" {
		t.Fatalf("agent reply should surface the reply field, got %q", text)
	}

	var entries []ProgressEntry
	doJSON(t, "GET", srv.URL+"/api/channels/"+ch+"/execution", nil, &entries)
	kinds := map[string]bool{}
	for _, e := range entries {
		kinds[e.Kind] = true
	}
	if !kinds["spawn"] || !kinds["done"] {
		t.Fatalf("execution log missing spawn/done: %+v", entries)
	}
}

func TestAgentFailureIsReportedWithRetryPayload(t *testing.T) {
	srv, st := newChatOverNats(t, false, "")
	ch := seedChannelWithAgent(t, srv, "be-dev")
	doJSON(t, "POST", srv.URL+"/api/channels/"+ch+"/messages", map[string]any{"text": "做点什么"}, nil)

	replies := agentMessages(t, st, ch)
	reported, hasTask := false, false
	for _, p := range replies {
		if s, _ := p["text"].(string); strings.Contains(s, "失败") || strings.Contains(s, "⚠") {
			reported = true
		}
		if task, _ := p["task"].(string); task != "" {
			hasTask = true // 前端重试需要原任务
		}
	}
	if !reported {
		t.Fatalf("failure not surfaced to channel: %+v", replies)
	}
	if !hasTask {
		t.Fatalf("failure message must carry the original task for retry: %+v", replies)
	}
}
