package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/machine"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/workflow"
)

func TestHTTPLaunchAndWS(t *testing.T) {
	nc := startTestNATS(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	m := NewRunManager(nc, nil, nil, "r")
	m.baseFactory = func(_ string) workflow.Executor { return &workflow.MockExecutor{} }
	if err := m.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	ts := httptest.NewServer(NewHandler(m, machine.NewHub(nc, machine.NewTokenStore(), nil), nil))
	defer ts.Close()

	// 发起 run。
	body := `{"workflowJSON":` + testDefRaw + `,"workspace":"ws"}`
	resp, err := http.Post(ts.URL+"/api/runs", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post run: %v", err)
	}
	if resp.StatusCode != 201 {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var launched struct {
		RunID string `json:"runId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&launched); err != nil {
		t.Fatalf("decode: %v", err)
	}
	_ = resp.Body.Close()

	// 连 WS,应收到 waiting_approval 事件。
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/runs/" + launched.RunID + "/events"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer func() { _ = ws.Close() }()

	var gotReview bool
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var ev RunEvent
		if err := ws.ReadJSON(&ev); err != nil {
			break
		}
		if ev.NodeID == "ap" && ev.Status == workflow.StatusWaitingApproval {
			gotReview = true
			break
		}
	}
	if !gotReview {
		t.Fatal("WS did not receive waiting_approval event")
	}

	// 审批通过。
	req, _ := http.NewRequest("POST", ts.URL+"/api/runs/"+launched.RunID+"/approvals/ap", strings.NewReader(`{"approve":true}`))
	req.Header.Set("Content-Type", "application/json")
	r2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if r2.StatusCode != 200 {
		t.Fatalf("approve status = %d, want 200", r2.StatusCode)
	}
	_ = r2.Body.Close()

	// run 终态 completed。
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		run, _ := m.Get(launched.RunID)
		if run != nil && run.Status() == RunCompleted {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	run, _ := m.Get(launched.RunID)
	if run == nil || run.Status() != RunCompleted {
		t.Fatalf("run status = %v, want completed", run.Status())
	}
}

// httpRetryDefRaw: agent a0 retries once then a human gate rejects with
// feedback; the rejected-edge fixer must receive the feedback. End-to-end
// verification of PRD §13 over the real HTTP + WS + engine stack.
const httpRetryDefRaw = `{
  "id":"http-retry","version":"1",
  "nodes":[
    {"id":"t0","type":"trigger","trigger":{"source":"manual"}},
    {"id":"a0","type":"agent","agent":{"id":"a0","role":"dev","executor":"mock","systemPrompt":"x","retry":{"maxAttempts":2,"backoffSeconds":[0]}}},
    {"id":"ap","type":"human_approval","humanApproval":{"approvers":"any_human","timeoutMs":60000,"onTimeout":"pause","onReject":"retry"}},
    {"id":"fix","type":"agent","agent":{"id":"fix","role":"dev","executor":"mock","systemPrompt":"y"}}
  ],
  "edges":[
    {"from":"t0","to":"a0"},
    {"from":"a0","to":"ap"},
    {"from":"ap","to":"fix","condition":"rejected"}
  ]
}`

func TestHTTPRetryAndRejectionFeedback(t *testing.T) {
	nc := startTestNATS(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	a0Calls := 0
	var fixInput json.RawMessage
	m := NewRunManager(nc, nil, nil, "r")
	m.baseFactory = func(_ string) workflow.Executor {
		return &workflow.MockExecutor{RunAgentFn: func(_ context.Context, n *workflow.Node, input json.RawMessage) (workflow.AgentResult, error) {
			switch n.ID {
			case "a0":
				a0Calls++
				if a0Calls == 1 {
					return workflow.AgentResult{}, fmt.Errorf("flaky: compile error")
				}
			case "fix":
				fixInput = append(json.RawMessage(nil), input...)
			}
			return workflow.AgentResult{Output: json.RawMessage(`{"ok":true}`)}, nil
		}}
	}
	if err := m.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	ts := httptest.NewServer(NewHandler(m, machine.NewHub(nc, machine.NewTokenStore(), nil), nil))
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/runs", "application/json",
		strings.NewReader(`{"workflowJSON":`+httpRetryDefRaw+`,"workspace":"ws"}`))
	if err != nil {
		t.Fatalf("post run: %v", err)
	}
	if resp.StatusCode != 201 {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var launched struct {
		RunID string `json:"runId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&launched); err != nil {
		t.Fatalf("decode: %v", err)
	}
	_ = resp.Body.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/runs/" + launched.RunID + "/events"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer func() { _ = ws.Close() }()

	sawRetry, sawReview, sawRejectedFeedback, sentReject := false, false, false, false
	seqCount := map[int]int{}
	// 事件流读完后 ReadJSON 会阻塞;设读截止时间保证循环在截止处退出。
	_ = ws.SetReadDeadline(time.Now().Add(4 * time.Second))
	for {
		var ev RunEvent
		if err := ws.ReadJSON(&ev); err != nil {
			break
		}
		seqCount[ev.Seq]++
		if ev.NodeID == "a0" && ev.Status == workflow.StatusRetrying {
			sawRetry = true
		}
		// 事件经本地投递 + NATS 回环各到一次;拒绝只发一次,重复事件不再触发。
		if ev.NodeID == "ap" && ev.Status == workflow.StatusWaitingApproval && !sentReject {
			sentReject = true
			sawReview = true
			// 拒绝并带结构化反馈。
			req, _ := http.NewRequest("POST", ts.URL+"/api/runs/"+launched.RunID+"/approvals/ap",
				strings.NewReader(`{"approve":false,"reason":"打回","feedback":{"category":"功能缺陷","location":"main.go:12","detail":"缺空值校验"}}`))
			req.Header.Set("Content-Type", "application/json")
			r2, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("reject: %v", err)
			}
			if r2.StatusCode != 200 {
				var eb map[string]any
				_ = json.NewDecoder(r2.Body).Decode(&eb)
				_ = r2.Body.Close()
				t.Fatalf("reject status = %d, want 200 (body=%v)", r2.StatusCode, eb)
			}
			_ = r2.Body.Close()
		}
		if ev.Review != nil && !ev.Review.Approved && ev.Review.Feedback != nil && ev.Review.Feedback.Detail == "缺空值校验" {
			sawRejectedFeedback = true
		}
	}

	if !sawRetry {
		t.Fatal("WS did not observe a0 retrying (NODE_RETRY_SCHEDULED)")
	}
	if !sawReview {
		t.Fatal("WS did not observe approval waiting")
	}
	if !sawRejectedFeedback {
		t.Fatal("WS did not observe REVIEW_REJECTED with structured feedback")
	}
	if a0Calls != 2 {
		t.Fatalf("a0 ran %d times, want 2 (retried once)", a0Calls)
	}
	// WS 去重:每个 seq 事件只到一次(NATS 回环不再重复投递)。
	for seq, n := range seqCount {
		if n > 1 {
			t.Fatalf("WS delivered seq %d %d times, want once (double-delivery)", seq, n)
		}
	}
	if len(fixInput) == 0 {
		t.Fatal("fix node never ran")
	}
	var scope map[string]json.RawMessage
	if err := json.Unmarshal(fixInput, &scope); err != nil {
		t.Fatalf("fix input not JSON: %v", err)
	}
	if fb := string(scope["rejection_feedback"]); !strings.Contains(fb, "缺空值校验") {
		t.Fatalf("fix must receive rejection_feedback (got %q)", fb)
	}
}

// §17.2/F.7: when CONTROL_API_TOKEN is set, state-changing commands require a
// Bearer token; read-only (GET/WS) stays free; unset = near no-op.
func TestHTTPBearerEnforcement(t *testing.T) {
	nc := startTestNATS(t)

	// Server A: no token configured → near no-op (state-changing allowed).
	AuthToken = ""
	mA := NewRunManager(nc, nil, nil, "r")
	tsA := httptest.NewServer(NewHandler(mA, machine.NewHub(nc, machine.NewTokenStore(), nil), nil))
	defer tsA.Close()
	r1, _ := http.Post(tsA.URL+"/api/runs", "application/json",
		strings.NewReader(`{"workflowJSON":`+testDefRaw+`,"workspace":"ws"}`))
	_ = r1.Body.Close()
	if r1.StatusCode != 201 {
		t.Fatalf("no-token config: POST /api/runs = %d, want 201 (near no-op)", r1.StatusCode)
	}

	// Server B: token configured → 401 without, 200 GET, 201 with, 401 wrong.
	AuthToken = "s3cret"
	defer func() { AuthToken = "" }()
	mB := NewRunManager(nc, nil, nil, "r")
	tsB := httptest.NewServer(NewHandler(mB, machine.NewHub(nc, machine.NewTokenStore(), nil), nil))
	defer tsB.Close()

	r2, _ := http.Post(tsB.URL+"/api/runs", "application/json",
		strings.NewReader(`{"workflowJSON":`+testDefRaw+`,"workspace":"ws"}`))
	_ = r2.Body.Close()
	if r2.StatusCode != 401 {
		t.Fatalf("configured: POST without token = %d, want 401", r2.StatusCode)
	}
	reqG, _ := http.NewRequest("GET", tsB.URL+"/api/runs", nil)
	reqG.Header.Set("Authorization", "Bearer s3cret")
	r3, _ := http.DefaultClient.Do(reqG)
	_ = r3.Body.Close()
	if r3.StatusCode != 200 {
		t.Fatalf("configured: GET /api/runs with token = %d, want 200", r3.StatusCode)
	}

	req, _ := http.NewRequest("POST", tsB.URL+"/api/runs",
		strings.NewReader(`{"workflowJSON":`+testDefRaw+`,"workspace":"ws"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer s3cret")
	r4, _ := http.DefaultClient.Do(req)
	_ = r4.Body.Close()
	if r4.StatusCode != 201 {
		t.Fatalf("configured: POST with token = %d, want 201", r4.StatusCode)
	}

	reqBad, _ := http.NewRequest("POST", tsB.URL+"/api/runs",
		strings.NewReader(`{"workflowJSON":`+testDefRaw+`,"workspace":"ws"}`))
	reqBad.Header.Set("Content-Type", "application/json")
	reqBad.Header.Set("Authorization", "Bearer wrong")
	r5, _ := http.DefaultClient.Do(reqBad)
	_ = r5.Body.Close()
	if r5.StatusCode != 401 {
		t.Fatalf("configured: POST with wrong token = %d, want 401", r5.StatusCode)
	}
}

func TestHTTPErrors(t *testing.T) {
	nc := startTestNATS(t)
	m := NewRunManager(nc, nil, nil, "r")
	ts := httptest.NewServer(NewHandler(m, machine.NewHub(nc, machine.NewTokenStore(), nil), nil))
	defer ts.Close()

	// 非法工作流(空 nodes)→ 4xx 而非 500。
	r, _ := http.Post(ts.URL+"/api/runs", "application/json",
		strings.NewReader(`{"workflowJSON":{"id":"","version":"","nodes":[]},"workspace":"ws"}`))
	if r.StatusCode == 201 {
		_ = r.Body.Close()
		t.Fatal("invalid workflow should not launch")
	}
	if r.StatusCode == 500 {
		_ = r.Body.Close()
		t.Fatalf("invalid input must be 4xx, got %d", r.StatusCode)
	}
	_ = r.Body.Close()

	// 未知 run 的审批 → 404。
	req, _ := http.NewRequest("POST", ts.URL+"/api/runs/nope/approvals/x", strings.NewReader(`{"approve":true}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	if resp.StatusCode != 404 {
		t.Fatalf("unknown run approve = %d, want 404", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// GET / → 200 且含页面标记。
	get, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("get /: %v", err)
	}
	if get.StatusCode != 200 {
		t.Fatalf("GET / = %d", get.StatusCode)
	}
	buf := make([]byte, 512)
	n, _ := get.Body.Read(buf)
	_ = get.Body.Close()
	if !strings.Contains(string(buf[:n]), "chaos.plus") {
		t.Error("GET / does not serve the UI page")
	}
}
