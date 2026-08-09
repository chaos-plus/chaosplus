package server

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/store"
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
