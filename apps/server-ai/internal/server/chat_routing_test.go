package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/store"
)

func TestMentionName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"@be-dev 修一下", "be-dev"},
		{"没有提及", ""},
		{"前缀 @fe_dev2 后缀", "fe_dev2"},
		{"@", ""},
	}
	for _, c := range cases {
		if got := mentionName(c.in); got != c.want {
			t.Errorf("mentionName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRoleMatchScorePrefersMatchingRole(t *testing.T) {
	bePrompt := "你是后端开发,负责 Go/Python/API/数据库。"
	fePrompt := "你是前端开发,负责 React/HTML/CSS/UI 界面。"

	task := "write a python api service"
	if roleMatchScore(bePrompt, task) <= roleMatchScore(fePrompt, task) {
		t.Fatalf("backend task should score higher for be-dev (be=%d fe=%d)",
			roleMatchScore(bePrompt, task), roleMatchScore(fePrompt, task))
	}

	task = "build a react component for the UI"
	if roleMatchScore(fePrompt, task) <= roleMatchScore(bePrompt, task) {
		t.Fatalf("frontend task should score higher for fe-dev (fe=%d be=%d)",
			roleMatchScore(fePrompt, task), roleMatchScore(bePrompt, task))
	}
}

func TestRouteAgentMentionBeatsScoreAndBusyBreaksTies(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "route.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()

	be1 := &store.AgentSpec{ID: "ag-be1", Name: "be-one", Runtime: "claude", SystemPrompt: "你是后端开发,负责 Go/API/数据库。"}
	be2 := &store.AgentSpec{ID: "ag-be2", Name: "be-two", Runtime: "claude", SystemPrompt: "你是后端开发,负责 Go/API/数据库。"}
	fe := &store.AgentSpec{ID: "ag-fe", Name: "fe-one", Runtime: "claude", SystemPrompt: "你是前端开发,负责 React/CSS/UI。"}
	for _, a := range []*store.AgentSpec{be1, be2, fe} {
		if err := st.CreateAgent(ctx, a); err != nil {
			t.Fatalf("create agent: %v", err)
		}
	}

	cs := NewChatService(st, nil, nil, nil, "", t.TempDir())
	members := []store.ChannelMember{
		{MemberID: be1.ID, Kind: "agent"},
		{MemberID: be2.ID, Kind: "agent"},
		{MemberID: fe.ID, Kind: "agent"},
		{MemberID: "human", Kind: "human"},
	}

	// @mention 优先于职责打分。
	if got := cs.routeAgent("@fe-one write a go api", members); got == nil || got.ID != fe.ID {
		t.Fatalf("mention must win, got %+v", got)
	}

	// 无 mention → 职责匹配(前端任务给前端)。
	if got := cs.routeAgent("build a react ui component", members); got == nil || got.ID != fe.ID {
		t.Fatalf("role match failed, got %+v", got)
	}

	// 同职责多人 → 选更闲的:把 be1 标忙,应选 be2。
	cs.markBusy(be1.ID, 1)
	if cs.busyOf(be1.ID) != 1 {
		t.Fatalf("busy not tracked: %d", cs.busyOf(be1.ID))
	}
	got := cs.routeAgent("write a go api endpoint", members)
	if got == nil || got.ID != be2.ID {
		t.Fatalf("busy tie-break failed, want be-two, got %+v", got)
	}

	// 释放后不应低于 0。
	cs.markBusy(be1.ID, -1)
	cs.markBusy(be1.ID, -1)
	if cs.busyOf(be1.ID) != 0 {
		t.Fatalf("busy must clamp at 0, got %d", cs.busyOf(be1.ID))
	}

	// 没有 agent 成员 → nil。
	if got := cs.routeAgent("anything", []store.ChannelMember{{MemberID: "human", Kind: "human"}}); got != nil {
		t.Fatalf("expected nil with no agent members, got %+v", got)
	}
}

func TestExecutionLogServesEmptyArray(t *testing.T) {
	srv, _ := newWorkspaceTestServer(t)

	var ch store.Channel
	doJSON(t, "POST", srv.URL+"/api/channels", map[string]any{"name": "ops"}, &ch)

	// 初始为空数组(不能是 null,前端会 .map 崩)。
	var entries []ProgressEntry
	if code := doJSON(t, "GET", srv.URL+"/api/channels/"+ch.ID+"/execution", nil, &entries); code != 200 {
		t.Fatalf("execution: %d", code)
	}
	if entries == nil {
		t.Fatal("execution must return [] not null")
	}
}

func TestPostMessageRequiresTextAndPersists(t *testing.T) {
	srv, _ := newWorkspaceTestServer(t)

	var ch store.Channel
	doJSON(t, "POST", srv.URL+"/api/channels", map[string]any{"name": "dev"}, &ch)

	if code := doJSON(t, "POST", srv.URL+"/api/channels/"+ch.ID+"/messages", map[string]any{"text": ""}, nil); code != 400 {
		t.Fatalf("empty text should 400, got %d", code)
	}

	// 有效消息落库(没有 agent 成员时不触发执行)。
	if code := doJSON(t, "POST", srv.URL+"/api/channels/"+ch.ID+"/messages", map[string]any{"text": "hello"}, nil); code != 200 {
		t.Fatalf("post message: %d", code)
	}
	var msgs []store.ChannelMessage
	doJSON(t, "GET", srv.URL+"/api/channels/"+ch.ID+"/messages", nil, &msgs)
	if len(msgs) == 0 {
		t.Fatal("message not persisted")
	}
}

func TestChannelMemberLifecycleHTTP(t *testing.T) {
	srv, _ := newWorkspaceTestServer(t)

	var ag store.AgentSpec
	doJSON(t, "POST", srv.URL+"/api/agents", map[string]any{"name": "dev", "runtime": "claude"}, &ag)
	var ch store.Channel
	doJSON(t, "POST", srv.URL+"/api/channels", map[string]any{"name": "team"}, &ch)

	if code := doJSON(t, "POST", srv.URL+"/api/channels/"+ch.ID+"/members",
		map[string]any{"memberId": ag.ID, "kind": "agent"}, nil); code != 200 {
		t.Fatalf("add member: %d", code)
	}
	var members []store.ChannelMember
	doJSON(t, "GET", srv.URL+"/api/channels/"+ch.ID+"/members", nil, &members)
	hasAgent := false
	for _, m := range members {
		if m.Kind == "agent" && m.MemberID == ag.ID {
			hasAgent = true
		}
	}
	if !hasAgent {
		t.Fatalf("agent member missing: %+v", members)
	}

	if code := doJSON(t, "DELETE", srv.URL+"/api/channels/"+ch.ID+"/members/"+ag.ID+"/agent", nil, nil); code != 200 {
		t.Fatalf("remove member: %d", code)
	}
	doJSON(t, "GET", srv.URL+"/api/channels/"+ch.ID+"/members", nil, &members)
	for _, m := range members {
		if m.Kind == "agent" && m.MemberID == ag.ID {
			t.Fatalf("member not removed: %+v", members)
		}
	}
}

func TestAgentHTTPCRUD(t *testing.T) {
	srv, _ := newWorkspaceTestServer(t)

	var a store.AgentSpec
	code := doJSON(t, "POST", srv.URL+"/api/agents",
		map[string]any{"name": "be", "runtime": "claude", "systemPrompt": "后端"}, &a)
	if code != 201 && code != 200 {
		t.Fatalf("create agent: %d", code)
	}
	if a.ID == "" {
		t.Fatalf("no id: %+v", a)
	}
	if code := doJSON(t, "POST", srv.URL+"/api/agents", map[string]any{"name": ""}, nil); code != 400 {
		t.Fatalf("empty name should 400, got %d", code)
	}
	if code := doJSON(t, "PUT", srv.URL+"/api/agents/"+a.ID,
		map[string]any{"name": "be2", "runtime": "claude", "systemPrompt": "后端2"}, nil); code != 200 {
		t.Fatalf("update: %d", code)
	}
	var list []store.AgentSpec
	doJSON(t, "GET", srv.URL+"/api/agents", nil, &list)
	if len(list) != 1 || list[0].Name != "be2" {
		t.Fatalf("list wrong: %+v", list)
	}
	if code := doJSON(t, "DELETE", srv.URL+"/api/agents/"+a.ID, nil, nil); code != 200 {
		t.Fatalf("delete: %d", code)
	}
}

// PRD D.4:数字人生命周期 —— 绑定 machine、启停、注销(交接文档 / 强制二次确认)。
func TestAgentLifecycleStatusAndRetire(t *testing.T) {
	srv, st := newWorkspaceTestServer(t)
	ctx := context.Background()

	var a store.AgentSpec
	doJSON(t, "POST", srv.URL+"/api/agents",
		map[string]any{"name": "bot", "runtime": "claude", "systemPrompt": "职责", "description": "desc"}, &a)
	if a.Status != "stopped" {
		t.Fatalf("new agent should start stopped, got %q", a.Status)
	}

	// 未绑定 machine 不能启动。
	if code := doJSON(t, "POST", srv.URL+"/api/agents/"+a.ID+"/status", map[string]any{"status": "running"}, nil); code != 400 {
		t.Fatalf("starting without a machine should 400, got %d", code)
	}
	// 非法状态被拒。
	if code := doJSON(t, "POST", srv.URL+"/api/agents/"+a.ID+"/status", map[string]any{"status": "zombie"}, nil); code != 400 {
		t.Fatalf("invalid status should 400, got %d", code)
	}

	// 绑定 machine 后可启停。
	doJSON(t, "PUT", srv.URL+"/api/agents/"+a.ID,
		map[string]any{"name": "bot", "runtime": "claude", "systemPrompt": "职责", "machineId": "m-1"}, nil)
	if code := doJSON(t, "POST", srv.URL+"/api/agents/"+a.ID+"/status", map[string]any{"status": "running"}, nil); code != 200 {
		t.Fatalf("start: %d", code)
	}
	if g, _ := st.GetAgent(ctx, a.ID); g.Status != "running" || g.MachineID != "m-1" {
		t.Fatalf("agent not running on its machine: %+v", g)
	}
	// 托管数按 machine 统计。
	counts, err := st.CountAgentsByMachine(ctx)
	if err != nil || counts["m-1"] != 1 {
		t.Fatalf("CountAgentsByMachine = %+v, %v", counts, err)
	}
	if code := doJSON(t, "POST", srv.URL+"/api/agents/"+a.ID+"/status", map[string]any{"status": "stopped"}, nil); code != 200 {
		t.Fatalf("stop: %d", code)
	}

	// 强制注销必须回填名称。
	if code := doJSON(t, "POST", srv.URL+"/api/agents/"+a.ID+"/retire", map[string]any{"force": true, "confirm": "nope"}, nil); code != 400 {
		t.Fatalf("force retire without confirmation should 400, got %d", code)
	}

	// 正常注销产出交接文档并落库。
	var res struct {
		HandoverDoc string `json:"handoverDoc"`
	}
	if code := doJSON(t, "POST", srv.URL+"/api/agents/"+a.ID+"/retire",
		map[string]any{"successor": "peer", "reason": "项目结束"}, &res); code != 200 {
		t.Fatalf("retire: %d", code)
	}
	if !strings.Contains(res.HandoverDoc, "bot") || !strings.Contains(res.HandoverDoc, "peer") {
		t.Fatalf("handover doc missing agent/successor: %q", res.HandoverDoc)
	}
	g, _ := st.GetAgent(ctx, a.ID)
	if g.Status != "retired" || g.HandoverDoc == "" || g.RetiredAt == 0 {
		t.Fatalf("retire not persisted: %+v", g)
	}
	// 注销后不再计入托管数。
	counts, _ = st.CountAgentsByMachine(ctx)
	if counts["m-1"] != 0 {
		t.Fatalf("retired agent still counted: %+v", counts)
	}
	// 重复注销 / 注销后启动都要被拒。
	if code := doJSON(t, "POST", srv.URL+"/api/agents/"+a.ID+"/retire", map[string]any{}, nil); code != 409 {
		t.Fatalf("double retire should 409, got %d", code)
	}
	if code := doJSON(t, "POST", srv.URL+"/api/agents/"+a.ID+"/status", map[string]any{"status": "running"}, nil); code != 409 {
		t.Fatalf("starting a retired agent should 409, got %d", code)
	}
}

// 注销要把 agent 从所有频道移除,否则还会被路由到任务。
func TestRetireRemovesAgentFromChannels(t *testing.T) {
	srv, st := newWorkspaceTestServer(t)
	ctx := context.Background()

	var a store.AgentSpec
	doJSON(t, "POST", srv.URL+"/api/agents", map[string]any{"name": "leaver", "runtime": "claude"}, &a)
	var ch store.Channel
	doJSON(t, "POST", srv.URL+"/api/channels", map[string]any{"name": "team"}, &ch)
	doJSON(t, "POST", srv.URL+"/api/channels/"+ch.ID+"/members", map[string]any{"memberId": a.ID, "kind": "agent"}, nil)

	doJSON(t, "POST", srv.URL+"/api/agents/"+a.ID+"/retire", map[string]any{}, nil)

	members, _ := st.ListChannelMembers(ctx, ch.ID)
	for _, m := range members {
		if m.Kind == "agent" && m.MemberID == a.ID {
			t.Fatalf("retired agent still in channel: %+v", members)
		}
	}
}

// 解散频道:仅 owner 可操作,且连同消息与成员一起清掉。
func TestDissolveChannelOwnerOnly(t *testing.T) {
	srv, st := newWorkspaceTestServer(t)
	ctx := context.Background()

	var ch store.Channel
	doJSON(t, "POST", srv.URL+"/api/channels", map[string]any{"name": "doomed"}, &ch)
	if ch.OwnerID != "human" {
		t.Fatalf("creator should own the channel, got %q", ch.OwnerID)
	}
	doJSON(t, "POST", srv.URL+"/api/channels/"+ch.ID+"/messages", map[string]any{"text": "hi"}, nil)

	// 非 owner 被拒。
	req, _ := http.NewRequest("DELETE", srv.URL+"/api/channels/"+ch.ID, nil)
	req.Header.Set("X-Actor", "intruder")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatalf("non-owner dissolve should 403, got %d", res.StatusCode)
	}
	if _, err := st.GetChannel(ctx, ch.ID); err != nil {
		t.Fatal("channel must survive a rejected dissolve")
	}

	// owner 解散成功,数据全清。
	req2, _ := http.NewRequest("DELETE", srv.URL+"/api/channels/"+ch.ID, nil)
	req2.Header.Set("X-Actor", "human")
	res2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	res2.Body.Close()
	if res2.StatusCode != 200 {
		t.Fatalf("owner dissolve: %d", res2.StatusCode)
	}
	if _, err := st.GetChannel(ctx, ch.ID); err == nil {
		t.Fatal("channel should be gone")
	}
	if msgs, _ := st.ListChannelMessages(ctx, ch.ID, 10); len(msgs) != 0 {
		t.Fatalf("messages should be purged: %+v", msgs)
	}
	if members, _ := st.ListChannelMembers(ctx, ch.ID); len(members) != 0 {
		t.Fatalf("members should be purged: %+v", members)
	}

	// 不存在的频道 404。
	req3, _ := http.NewRequest("DELETE", srv.URL+"/api/channels/ch-nope", nil)
	res3, _ := http.DefaultClient.Do(req3)
	res3.Body.Close()
	if res3.StatusCode != 404 {
		t.Fatalf("unknown channel should 404, got %d", res3.StatusCode)
	}
}

// 解散后可以重建同名频道,新频道是干净的(不继承旧消息)。
func TestChannelCanBeRecreatedAfterDissolve(t *testing.T) {
	srv, st := newWorkspaceTestServer(t)
	ctx := context.Background()

	var first store.Channel
	doJSON(t, "POST", srv.URL+"/api/channels", map[string]any{"name": "reuse"}, &first)
	doJSON(t, "POST", srv.URL+"/api/channels/"+first.ID+"/messages", map[string]any{"text": "old"}, nil)

	req, _ := http.NewRequest("DELETE", srv.URL+"/api/channels/"+first.ID, nil)
	req.Header.Set("X-Actor", "human")
	res, _ := http.DefaultClient.Do(req)
	res.Body.Close()

	var second store.Channel
	doJSON(t, "POST", srv.URL+"/api/channels", map[string]any{"name": "reuse"}, &second)
	if second.ID == first.ID {
		t.Fatal("recreated channel should get a fresh id")
	}
	msgs, _ := st.ListChannelMessages(ctx, second.ID, 10)
	if len(msgs) != 0 {
		t.Fatalf("new channel must start empty: %+v", msgs)
	}
	// 创建者仍自动入群。
	members, _ := st.ListChannelMembers(ctx, second.ID)
	if len(members) == 0 {
		t.Fatal("creator should be auto-joined")
	}
}

// PRD §9.1 realtime: a channel WS subscriber receives newly-posted messages.
func TestChannelEventsWSRealtime(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "ws.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()

	ch := &store.Channel{ID: "ch-ws", Name: "ws-test", OwnerID: "human"}
	if err := st.CreateChannel(ctx, ch); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	cs := NewChatService(st, nil, nil, nil, "", t.TempDir())
	mux := http.NewServeMux()
	cs.register(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/channels/" + ch.ID + "/events"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer ws.Close()
	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))

	// 先收到回放(空)或直接收到实时消息。
	resp, err := http.Post(ts.URL+"/api/channels/"+ch.ID+"/messages",
		"application/json", strings.NewReader(`{"text":"hello ws"}`))
	if err != nil {
		t.Fatalf("post message: %v", err)
	}
	resp.Body.Close()

	var got store.ChannelMessage
	if err := ws.ReadJSON(&got); err != nil {
		t.Fatalf("read ws: %v", err)
	}
	var payload map[string]any
	_ = json.Unmarshal([]byte(got.PayloadJSON), &payload)
	if got.ChannelID != ch.ID || payload["text"] != "hello ws" {
		t.Fatalf("ws message = %+v, want text 'hello ws' in %s", got, ch.ID)
	}
}
