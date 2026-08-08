package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/gateway"
	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/workflow"
)

func TestHTTPLaunchAndWS(t *testing.T) {
	nc := startTestNATS(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	m := NewRunManager(nc, gateway.New(nc), nil, "r")
	m.baseFactory = func(_ string) workflow.Executor { return &workflow.MockExecutor{} }
	if err := m.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	ts := httptest.NewServer(NewHandler(m))
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
	var launched struct{ RunID string `json:"runId"` }
	if err := json.NewDecoder(resp.Body).Decode(&launched); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()

	// 连 WS,应收到 waiting_approval 事件。
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/runs/" + launched.RunID + "/events"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer ws.Close()

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
	r2.Body.Close()

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

func TestHTTPErrors(t *testing.T) {
	nc := startTestNATS(t)
	m := NewRunManager(nc, gateway.New(nc), nil, "r")
	ts := httptest.NewServer(NewHandler(m))
	defer ts.Close()

	// 非法工作流(空 nodes)→ 4xx 而非 500。
	r, _ := http.Post(ts.URL+"/api/runs", "application/json",
		strings.NewReader(`{"workflowJSON":{"id":"","version":"","nodes":[]},"workspace":"ws"}`))
	if r.StatusCode == 201 {
		r.Body.Close()
		t.Fatal("invalid workflow should not launch")
	}
	if r.StatusCode == 500 {
		r.Body.Close()
		t.Fatalf("invalid input must be 4xx, got %d", r.StatusCode)
	}
	r.Body.Close()

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
	resp.Body.Close()

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
	get.Body.Close()
	if !strings.Contains(string(buf[:n]), "chaos.plus") {
		t.Error("GET / does not serve the UI page")
	}
}
