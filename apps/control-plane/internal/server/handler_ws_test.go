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

	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/machine"
	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/store"
	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/workflow"
)

func newHandlerTestServer(t *testing.T) (*httptest.Server, *RunManager) {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "ws.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	nc := startTestNATS(t)
	ctx, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)
	m := NewRunManager(nc, nil, st, "runner-1")
	m.baseFactory = func(_ string) workflow.Executor { return &workflow.MockExecutor{} }
	if err := m.Start(ctx); err != nil {
		t.Fatalf("run manager: %v", err)
	}
	cs := NewChatService(st, nil, nil, m, "runner-1", t.TempDir())
	srv := httptest.NewServer(NewHandler(m, nil, cs))
	t.Cleanup(srv.Close)
	return srv, m
}

// 网页端通过 WS 订阅 run 事件:必须收到节点推进,直到审批处停下。
func TestRunEventsWebSocketStreamsProgress(t *testing.T) {
	srv, _ := newHandlerTestServer(t)

	var launched struct {
		RunID string `json:"runId"`
	}
	if code := doJSON(t, "POST", srv.URL+"/api/runs",
		map[string]any{"workflowJSON": json.RawMessage(testDefRaw), "workspace": t.TempDir()}, &launched); code != 200 && code != 201 {
		t.Fatalf("launch via HTTP: %d", code)
	}
	if launched.RunID == "" {
		t.Fatal("no runId from POST /api/runs")
	}

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/runs/" + launched.RunID + "/events"
	c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer c.Close()

	_ = c.SetReadDeadline(time.Now().Add(15 * time.Second))
	sawWaiting := false
	for i := 0; i < 10 && !sawWaiting; i++ {
		_, data, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("ws read: %v", err)
		}
		var ev RunEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			t.Fatalf("ws payload not a RunEvent: %s", data)
		}
		if ev.Status == workflow.StatusWaitingApproval {
			sawWaiting = true
		}
	}
	if !sawWaiting {
		t.Fatal("never saw waiting_approval over the websocket")
	}
}

// 审批 REST:通过后 run 走完;未知 run/节点要报错而不是静默成功。
func TestApprovalEndpointResumesRun(t *testing.T) {
	srv, m := newHandlerTestServer(t)

	var launched struct {
		RunID string `json:"runId"`
	}
	doJSON(t, "POST", srv.URL+"/api/runs",
		map[string]any{"workflowJSON": json.RawMessage(testDefRaw), "workspace": t.TempDir()}, &launched)

	var run *Run
	waitFor(t, func() bool {
		for _, r := range m.List() {
			if r.ID == launched.RunID && r.Status() == RunWaitingApproval {
				run = r
				return true
			}
		}
		return false
	}, 15*time.Second)

	if code := doJSON(t, "POST", srv.URL+"/api/runs/"+launched.RunID+"/approvals/ap",
		map[string]any{"approve": true}, nil); code != 200 {
		t.Fatalf("approve: %d", code)
	}
	waitFor(t, func() bool { return run.Status() == RunCompleted }, 15*time.Second)

	// 未知 run。
	if code := doJSON(t, "POST", srv.URL+"/api/runs/run-nope/approvals/ap", map[string]any{"approve": true}, nil); code == 200 {
		t.Fatal("approving an unknown run must not succeed")
	}
}

func TestRunDetailAndListEndpoints(t *testing.T) {
	srv, _ := newHandlerTestServer(t)

	var launched struct {
		RunID string `json:"runId"`
	}
	doJSON(t, "POST", srv.URL+"/api/runs",
		map[string]any{"workflowJSON": json.RawMessage(testDefRaw), "workspace": t.TempDir()}, &launched)

	var list []map[string]any
	if code := doJSON(t, "GET", srv.URL+"/api/runs", nil, &list); code != 200 || len(list) != 1 {
		t.Fatalf("list runs: %d %+v", code, list)
	}
	var detail map[string]any
	if code := doJSON(t, "GET", srv.URL+"/api/runs/"+launched.RunID, nil, &detail); code != 200 || detail["def"] == nil {
		t.Fatalf("run detail missing def: %d %+v", code, detail)
	}
	if code := doJSON(t, "GET", srv.URL+"/api/runs/run-nope", nil, nil); code != 404 {
		t.Fatalf("unknown run should 404, got %d", code)
	}
}

func TestLaunchEndpointRejectsBadPayload(t *testing.T) {
	srv, _ := newHandlerTestServer(t)

	if code := doJSON(t, "POST", srv.URL+"/api/runs", map[string]any{"workspace": t.TempDir()}, nil); code == 200 {
		t.Fatal("launch without workflowJSON must fail")
	}
	if code := doJSON(t, "POST", srv.URL+"/api/runs",
		map[string]any{"workflowJSON": json.RawMessage(`{"id":"x","version":"1","nodes":[],"edges":[]}`), "workspace": t.TempDir()}, nil); code == 200 {
		t.Fatal("empty DAG must be rejected")
	}
}

func TestWebSocketOnUnknownRunFails(t *testing.T) {
	srv, _ := newHandlerTestServer(t)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/runs/run-nope/events"
	c, res, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		c.Close()
		t.Fatal("websocket on an unknown run should be rejected")
	}
	if res != nil && res.StatusCode == http.StatusOK {
		t.Fatalf("unexpected 200 for unknown run")
	}
}

// PRD §13:HTTP 拒绝缺结构化反馈 → 400;完整反馈 → 通过并回传到事件。
func TestApprovalRejectionRequiresStructuredFeedbackOverHTTP(t *testing.T) {
	srv, m := newHandlerTestServer(t)

	var launched struct {
		RunID string `json:"runId"`
	}
	doJSON(t, "POST", srv.URL+"/api/runs",
		map[string]any{"workflowJSON": json.RawMessage(testDefRaw), "workspace": t.TempDir()}, &launched)

	var run *Run
	waitFor(t, func() bool {
		for _, r := range m.List() {
			if r.ID == launched.RunID && r.Status() == RunWaitingApproval {
				run = r
				return true
			}
		}
		return false
	}, 15*time.Second)

	base := srv.URL + "/api/runs/" + launched.RunID + "/approvals/ap"
	if code := doJSON(t, "POST", base, map[string]any{"approve": false, "reason": "不行"}, nil); code != 400 {
		t.Fatalf("rejection without feedback should 400, got %d", code)
	}
	if code := doJSON(t, "POST", base,
		map[string]any{"approve": false, "feedback": map[string]any{"category": "功能缺陷"}}, nil); code != 400 {
		t.Fatalf("feedback without detail should 400, got %d", code)
	}
	if code := doJSON(t, "POST", base,
		map[string]any{"approve": false, "feedback": map[string]any{"category": "瞎写", "detail": "x"}}, nil); code != 400 {
		t.Fatalf("unknown category should 400, got %d", code)
	}

	full := map[string]any{
		"approve": false,
		"reason":  "打回",
		"feedback": map[string]any{
			"category": "需求偏差", "location": "login.tsx:42",
			"expected": "跳转首页", "detail": "点击后停在原页",
		},
	}
	if code := doJSON(t, "POST", base, full, nil); code != 200 {
		t.Fatalf("valid rejection should succeed, got %d", code)
	}

	// 反馈必须出现在 run 事件里(供下次尝试与前端展示)。
	waitFor(t, func() bool {
		for _, ev := range run.Events() {
			if ev.Review != nil && ev.Review.Feedback != nil && ev.Review.Feedback.Location == "login.tsx:42" {
				return true
			}
		}
		return false
	}, 10*time.Second)
}

// PRD D.1:仪表盘必须给出 run 状态分布、runner 健康、待审批队列、今日成本。
func TestDashboardStatsEndpoint(t *testing.T) {
	srv, _ := newHandlerTestServer(t)

	var empty map[string]any
	if code := doJSON(t, "GET", srv.URL+"/api/stats/dashboard", nil, &empty); code != 200 {
		t.Fatalf("stats: %d", code)
	}
	for _, k := range []string{"runsByStatus", "pendingApprovals", "machinesTotal", "machinesOnline", "lastHeartbeatAt", "costTodayUsd"} {
		if _, ok := empty[k]; !ok {
			t.Errorf("stats missing key %q: %+v", k, empty)
		}
	}
	// 空数组不能是 null,前端要 .map。
	if _, ok := empty["pendingApprovals"].([]any); !ok {
		t.Fatalf("pendingApprovals must be an array, got %T", empty["pendingApprovals"])
	}

	// 起一个停在审批门的 run → 状态分布与待审批队列都要反映出来。
	var launched struct {
		RunID string `json:"runId"`
	}
	doJSON(t, "POST", srv.URL+"/api/runs",
		map[string]any{"workflowJSON": json.RawMessage(testDefRaw), "workspace": t.TempDir()}, &launched)

	waitFor(t, func() bool {
		var s map[string]any
		doJSON(t, "GET", srv.URL+"/api/stats/dashboard", nil, &s)
		byStatus, _ := s["runsByStatus"].(map[string]any)
		pending, _ := s["pendingApprovals"].([]any)
		return byStatus["waiting_approval"] != nil && len(pending) > 0
	}, 15*time.Second)

	var s map[string]any
	doJSON(t, "GET", srv.URL+"/api/stats/dashboard", nil, &s)
	pending := s["pendingApprovals"].([]any)
	first := pending[0].(map[string]any)
	if first["runId"] != launched.RunID || first["nodeId"] == "" {
		t.Fatalf("pending approval missing run/node reference: %+v", first)
	}
}

// hub/chat 缺省时(最小装配)仪表盘不能 panic,要给出零值而不是 500。
func TestDashboardStatsDegradesWithoutHubOrStore(t *testing.T) {
	nc := startTestNATS(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	m := NewRunManager(nc, nil, nil, "runner-1")
	m.baseFactory = func(_ string) workflow.Executor { return &workflow.MockExecutor{} }
	if err := m.Start(ctx); err != nil {
		t.Fatalf("run manager: %v", err)
	}
	cs := NewChatService(nil, nil, nil, m, "runner-1", t.TempDir())
	srv := httptest.NewServer(NewHandler(m, nil, cs))
	defer srv.Close()

	var s map[string]any
	if code := doJSON(t, "GET", srv.URL+"/api/stats/dashboard", nil, &s); code != 200 {
		t.Fatalf("stats without hub/store: %d", code)
	}
	if s["machinesTotal"].(float64) != 0 || s["costTodayUsd"].(float64) != 0 {
		t.Fatalf("expected zeroed stats, got %+v", s)
	}
}

// PRD D.3/D.4:machines 列表要带托管 agent 数与可用运行时(runtime 下拉的数据源)。
func TestMachinesListCarriesAgentCountAndRuntimes(t *testing.T) {
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "mach.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()

	nc := startTestNATS(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	rm := NewRunManager(nc, nil, st, "runner-1")
	rm.baseFactory = func(_ string) workflow.Executor { return &workflow.MockExecutor{} }
	if err := rm.Start(ctx); err != nil {
		t.Fatalf("run manager: %v", err)
	}
	hub := machine.NewHub(nc, machine.NewTokenStore(), st)
	cs := NewChatService(st, nil, nil, rm, "runner-1", t.TempDir())
	srv := httptest.NewServer(NewHandler(rm, hub, cs))
	defer srv.Close()

	if err := st.UpsertMachine(ctx, store.Machine{ID: "m-x", Address: "127.0.0.1", Status: "confirmed"}); err != nil {
		t.Fatalf("seed machine: %v", err)
	}
	// 两个 agent 绑到该机,其中一个已注销 —— 注销的不该计入。
	for _, a := range []*store.AgentSpec{
		{ID: "ag-1", Name: "a1", Runtime: "claude", MachineID: "m-x", Status: "running"},
		{ID: "ag-2", Name: "a2", Runtime: "claude", MachineID: "m-x", Status: "retired"},
	} {
		if err := st.CreateAgent(ctx, a); err != nil {
			t.Fatalf("seed agent: %v", err)
		}
	}

	var list []map[string]any
	if code := doJSON(t, "GET", srv.URL+"/api/machines", nil, &list); code != 200 || len(list) != 1 {
		t.Fatalf("machines: %d %+v", code, list)
	}
	if list[0]["agentCount"].(float64) != 1 {
		t.Fatalf("agentCount should exclude retired agents: %+v", list[0])
	}
	// 离线机器没有运行时,但字段必须是数组而非 null。
	if _, ok := list[0]["runtimes"].([]any); !ok {
		t.Fatalf("runtimes must be an array, got %T", list[0]["runtimes"])
	}
}
