package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/machine"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/workflow"
)

// startTestNATS boots an in-process broker shared by all server tests.
func startTestNATS(t *testing.T) *nats.Conn {
	t.Helper()
	ns, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatalf("nats server: %v", err)
	}
	go ns.Start()
	t.Cleanup(ns.Shutdown)
	if !ns.ReadyForConnections(2 * time.Second) {
		t.Fatal("nats not ready")
	}
	nc, err := nats.Connect(ns.ClientURL())
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	t.Cleanup(nc.Close)
	return nc
}

func TestRunManagerPauseResumeAndCancel(t *testing.T) {
	nc := startTestNATS(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	m := NewRunManager(nc, nil, nil, "runner-1")
	var calls atomic.Int32
	m.baseFactory = func(_ string) workflow.Executor {
		return &workflow.MockExecutor{RunAgentFn: func(ctx context.Context, _ *workflow.Node, _ json.RawMessage) (workflow.AgentResult, error) {
			if calls.Add(1) == 1 {
				<-ctx.Done()
				return workflow.AgentResult{}, ctx.Err()
			}
			return workflow.AgentResult{Output: json.RawMessage(`{"ok":true}`)}, nil
		}}
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	def := json.RawMessage(`{"id":"lifecycle","version":"1","nodes":[{"id":"agent","type":"agent","agent":{"id":"agent","role":"dev","executor":"mock"}}],"edges":[]}`)
	run, err := m.Launch(ctx, LaunchRequest{WorkflowJSON: def, Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		for _, event := range run.Events() {
			if event.NodeID == "agent" && event.Status == workflow.StatusRunning {
				return true
			}
		}
		return false
	}, 3*time.Second)
	if err := m.Pause(run.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		run.mu.Lock()
		done := run.done
		run.mu.Unlock()
		if done == nil {
			return false
		}
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, 3*time.Second)
	if err := m.Resume(run.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return run.Status() == RunCompleted }, 3*time.Second)

	var cancelCalls atomic.Int32
	m.baseFactory = func(_ string) workflow.Executor {
		return &workflow.MockExecutor{RunAgentFn: func(ctx context.Context, _ *workflow.Node, _ json.RawMessage) (workflow.AgentResult, error) {
			cancelCalls.Add(1)
			<-ctx.Done()
			return workflow.AgentResult{}, ctx.Err()
		}}
	}
	cancelRun, err := m.Launch(ctx, LaunchRequest{WorkflowJSON: def, Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return cancelCalls.Load() > 0 }, 3*time.Second)
	if err := m.Cancel(cancelRun.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return cancelRun.Status() == RunCancelled }, 3*time.Second)
}

func TestRunLifecycleHTTPAndTenantIsolation(t *testing.T) {
	nc := startTestNATS(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	m := NewRunManager(nc, nil, nil, "runner-1")
	var calls atomic.Int32
	m.baseFactory = func(_ string) workflow.Executor {
		return &workflow.MockExecutor{RunAgentFn: func(ctx context.Context, _ *workflow.Node, _ json.RawMessage) (workflow.AgentResult, error) {
			if calls.Add(1) == 1 {
				<-ctx.Done()
				return workflow.AgentResult{}, ctx.Err()
			}
			return workflow.AgentResult{Output: json.RawMessage(`{"ok":true}`)}, nil
		}}
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	hub := machine.NewHub(nc, machine.NewTokenStore(), nil)
	ts := httptest.NewServer(NewHandler(m, hub, nil))
	defer ts.Close()

	request := func(method, path, entity string, body any) *http.Response {
		t.Helper()
		var payload []byte
		if body != nil {
			var err error
			payload, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
		req, err := http.NewRequest(method, ts.URL+path, bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Entity", entity)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	def := map[string]any{
		"id": "http-lifecycle", "version": "1",
		"nodes": []map[string]any{{
			"id": "agent", "type": "agent",
			"agent": map[string]any{"id": "agent", "role": "dev", "executor": "mock"},
		}},
		"edges": []any{},
	}
	launch := request(http.MethodPost, "/api/runs", "tenant-a", map[string]any{
		"workflowJSON": def, "workspace": t.TempDir(),
	})
	if launch.StatusCode != http.StatusCreated {
		t.Fatalf("launch status = %d", launch.StatusCode)
	}
	var launched struct {
		RunID string `json:"runId"`
	}
	if err := json.NewDecoder(launch.Body).Decode(&launched); err != nil {
		t.Fatal(err)
	}
	_ = launch.Body.Close()
	run, ok := m.Get(launched.RunID)
	if !ok {
		t.Fatal("launched run missing")
	}
	waitFor(t, func() bool { return calls.Load() > 0 }, 3*time.Second)

	for _, path := range []string{
		"/api/runs/" + launched.RunID,
		"/api/runs/" + launched.RunID + "/pause",
	} {
		method := http.MethodGet
		if path[len(path)-5:] == "pause" {
			method = http.MethodPost
		}
		resp := request(method, path, "tenant-b", nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s as tenant-b = %d, want 404", path, resp.StatusCode)
		}
	}

	pause := request(http.MethodPost, "/api/runs/"+launched.RunID+"/pause", "tenant-a", nil)
	_ = pause.Body.Close()
	if pause.StatusCode != http.StatusOK {
		t.Fatalf("pause status = %d", pause.StatusCode)
	}
	waitFor(t, func() bool { return run.Status() == RunPaused }, 3*time.Second)
	resume := request(http.MethodPost, "/api/runs/"+launched.RunID+"/resume", "tenant-a", nil)
	_ = resume.Body.Close()
	if resume.StatusCode != http.StatusOK {
		t.Fatalf("resume status = %d", resume.StatusCode)
	}
	waitFor(t, func() bool { return run.Status() == RunCompleted }, 3*time.Second)

	m.baseFactory = func(_ string) workflow.Executor {
		return &workflow.MockExecutor{RunAgentFn: func(ctx context.Context, _ *workflow.Node, _ json.RawMessage) (workflow.AgentResult, error) {
			<-ctx.Done()
			return workflow.AgentResult{}, ctx.Err()
		}}
	}
	cancelLaunch := request(http.MethodPost, "/api/runs", "tenant-a", map[string]any{
		"workflowJSON": def, "workspace": t.TempDir(),
	})
	var cancelRun struct {
		RunID string `json:"runId"`
	}
	if err := json.NewDecoder(cancelLaunch.Body).Decode(&cancelRun); err != nil {
		t.Fatal(err)
	}
	_ = cancelLaunch.Body.Close()
	waitFor(t, func() bool {
		r, exists := m.Get(cancelRun.RunID)
		return exists && len(r.Events()) >= 2
	}, 3*time.Second)
	cancel := request(http.MethodPost, "/api/runs/"+cancelRun.RunID+"/cancel", "tenant-a", nil)
	_ = cancel.Body.Close()
	if cancel.StatusCode != http.StatusOK {
		t.Fatalf("cancel status = %d", cancel.StatusCode)
	}
	waitFor(t, func() bool {
		r, _ := m.Get(cancelRun.RunID)
		return r.Status() == RunCancelled
	}, 3*time.Second)
}

// testDefRaw: trigger -> approval(human) -(approved)-> agent. JSON string so
// Approvers round-trips through its UnmarshalJSON ("any_human").
const testDefRaw = `{
  "id":"t","version":"1",
  "nodes":[
    {"id":"t0","type":"trigger","trigger":{"source":"manual"}},
    {"id":"ap","type":"human_approval","humanApproval":{"approvers":"any_human","timeoutMs":60000,"onTimeout":"pause","onReject":"pause"}},
    {"id":"a0","type":"agent","agent":{"id":"a0","role":"r","executor":"mock","systemPrompt":"x"}}
  ],
  "edges":[{"from":"t0","to":"ap"},{"from":"ap","to":"a0","condition":"approved"}]
}`

func TestRunManagerLaunchAndApprove(t *testing.T) {
	nc := startTestNATS(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	m := NewRunManager(nc, nil, nil, "runner-1")
	m.baseFactory = func(_ string) workflow.Executor { return &workflow.MockExecutor{} }
	if err := m.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	run, err := m.Launch(ctx, LaunchRequest{WorkflowJSON: json.RawMessage(testDefRaw), Workspace: t.TempDir()})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}

	// run 停在审批节点等待。
	waitFor(t, func() bool { return run.Status() == RunWaitingApproval }, 3*time.Second)

	// 通过 → 继续到 a0 → 完成。
	if err := m.Approve(run.ID, "ap", true, "", nil); err != nil {
		t.Fatalf("approve: %v", err)
	}
	waitFor(t, func() bool { return run.Status() == RunCompleted }, 3*time.Second)

	if len(run.Events()) == 0 {
		t.Fatal("no events recorded")
	}
}

func TestRunManagerRejectPauses(t *testing.T) {
	nc := startTestNATS(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	m := NewRunManager(nc, nil, nil, "runner-1")
	m.baseFactory = func(_ string) workflow.Executor { return &workflow.MockExecutor{} }
	if err := m.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	run, err := m.Launch(ctx, LaunchRequest{WorkflowJSON: json.RawMessage(testDefRaw), Workspace: t.TempDir()})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	waitFor(t, func() bool { return run.Status() == RunWaitingApproval }, 3*time.Second)

	if err := m.Approve(run.ID, "ap", false, "wrong spec", &workflow.Feedback{Category: workflow.FeedbackDeviation, Detail: "与需求不符"}); err != nil {
		t.Fatalf("reject: %v", err)
	}
	waitFor(t, func() bool { return run.Status() == RunPaused }, 3*time.Second)
}

func TestRunManagerRejectRoutingWithRejectedEdge(t *testing.T) {
	nc := startTestNATS(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	m := NewRunManager(nc, nil, nil, "runner-1")
	m.baseFactory = func(_ string) workflow.Executor { return &workflow.MockExecutor{} }
	if err := m.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	// onReject=retry + rejected 出边:拒绝走 rejected 分支而非暂停。
	defJSON, _ := json.Marshal(map[string]any{
		"id": "t", "version": "1",
		"nodes": []map[string]any{
			{"id": "t0", "type": "trigger", "trigger": map[string]any{"source": "manual"}},
			{"id": "ap", "type": "human_approval", "humanApproval": map[string]any{"approvers": "any_human", "timeoutMs": 60000, "onTimeout": "pause", "onReject": "retry"}},
			{"id": "a0", "type": "agent", "agent": map[string]any{"id": "a0", "role": "r", "executor": "mock"}},
			{"id": "alt", "type": "agent", "agent": map[string]any{"id": "alt", "role": "r", "executor": "mock"}},
		},
		"edges": []map[string]any{
			{"from": "t0", "to": "ap"},
			{"from": "ap", "to": "a0", "condition": "approved"},
			{"from": "ap", "to": "alt", "condition": "rejected"},
		},
	})

	run, err := m.Launch(ctx, LaunchRequest{WorkflowJSON: defJSON, Workspace: t.TempDir()})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	waitFor(t, func() bool { return run.Status() == RunWaitingApproval }, 3*time.Second)
	if err := m.Approve(run.ID, "ap", false, "nope", &workflow.Feedback{Category: workflow.FeedbackFunctional, Detail: "功能不对"}); err != nil {
		t.Fatalf("reject: %v", err)
	}
	waitFor(t, func() bool { return run.Status() == RunCompleted }, 3*time.Second)
}

func waitFor(t *testing.T, cond func() bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}
