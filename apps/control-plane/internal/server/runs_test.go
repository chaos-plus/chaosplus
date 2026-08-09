package server

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/store"
	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/workflow"
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

// retryDefRaw: agent node with bounded retry; used to verify finalStatus treats
// a transient failure (retried to success) as completed and exhaustion as failed.
const retryDefRaw = `{
  "id":"rt","version":"1",
  "nodes":[
    {"id":"t0","type":"trigger","trigger":{"source":"manual"}},
    {"id":"a0","type":"agent","agent":{"id":"a0","role":"r","executor":"mock","systemPrompt":"x","retry":{"maxAttempts":2,"backoffSeconds":[0]}}}
  ],
  "edges":[{"from":"t0","to":"a0"}]
}`

// M1 (round-1 review): a node that fails once and retries to success must end
// the run as completed, not failed.
func TestRunManagerRetryRecoversToCompleted(t *testing.T) {
	nc := startTestNATS(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	calls := 0
	m := NewRunManager(nc, nil, nil, "runner-1")
	m.baseFactory = func(_ string) workflow.Executor {
		return &workflow.MockExecutor{RunAgentFn: func(_ context.Context, _ *workflow.Node, _ json.RawMessage) (json.RawMessage, error) {
			calls++
			if calls == 1 {
				return nil, fmt.Errorf("flaky: compile error")
			}
			return json.RawMessage(`{"ok":true}`), nil
		}}
	}
	if err := m.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	run, err := m.Launch(ctx, LaunchRequest{WorkflowJSON: json.RawMessage(retryDefRaw), Workspace: t.TempDir()})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	waitFor(t, func() bool { return run.Status() == RunCompleted }, 3*time.Second)
	if calls != 2 {
		t.Fatalf("agent ran %d times, want 2 (retried once)", calls)
	}
}

// M1 (round-1 review): exhausting retries must end the run as failed.
func TestRunManagerRetryExhaustsToFailed(t *testing.T) {
	nc := startTestNATS(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	calls := 0
	m := NewRunManager(nc, nil, nil, "runner-1")
	m.baseFactory = func(_ string) workflow.Executor {
		return &workflow.MockExecutor{RunAgentFn: func(_ context.Context, _ *workflow.Node, _ json.RawMessage) (json.RawMessage, error) {
			calls++
			return nil, fmt.Errorf("always fails")
		}}
	}
	if err := m.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	run, err := m.Launch(ctx, LaunchRequest{WorkflowJSON: json.RawMessage(retryDefRaw), Workspace: t.TempDir()})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	waitFor(t, func() bool { return run.Status() == RunFailed }, 3*time.Second)
	if calls != 2 {
		t.Fatalf("agent ran %d times, want maxAttempts=2", calls)
	}
}

// PRD F.2: a human rejection persists a validation_results verdict AND a
// feedback_log entry; an approval persists a passed verdict.
func TestRunManagerWritesReviewProjections(t *testing.T) {
	nc := startTestNATS(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "cp.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	m := NewRunManager(nc, nil, st, "runner-1")
	m.baseFactory = func(_ string) workflow.Executor { return &workflow.MockExecutor{} }
	if err := m.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	run, err := m.Launch(ctx, LaunchRequest{WorkflowJSON: json.RawMessage(testDefRaw), Workspace: t.TempDir()})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	waitFor(t, func() bool { return run.Status() == RunWaitingApproval }, 3*time.Second)

	if err := m.Approve(run.ID, "ap", false, "打回", &workflow.Feedback{Category: workflow.FeedbackDeviation, Location: "a.ts:1", Detail: "与需求不符"}); err != nil {
		t.Fatalf("reject: %v", err)
	}
	waitFor(t, func() bool { return run.Status() == RunPaused }, 3*time.Second)

	vs, err := st.ListValidationResults(ctx, run.ID, 10)
	if err != nil {
		t.Fatalf("list validation: %v", err)
	}
	if len(vs) != 1 || vs[0].Passed != 0 || vs[0].ExecutionID != "ap" {
		t.Fatalf("validation results = %+v, want one rejected verdict for ap", vs)
	}
	fb, err := st.ListFeedbackLog(ctx, run.ID, 10)
	if err != nil {
		t.Fatalf("list feedback: %v", err)
	}
	if len(fb) != 1 || fb[0].Category != "需求偏差" || fb[0].Detail != "与需求不符" || fb[0].Location != "a.ts:1" {
		t.Fatalf("feedback log = %+v, want structured rejection", fb)
	}
}

// PRD §15.1: a launched run is persisted and survives as history.
func TestRunManagerPersistsRunToStore(t *testing.T) {
	nc := startTestNATS(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "cp.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	m := NewRunManager(nc, nil, st, "runner-1")
	m.baseFactory = func(_ string) workflow.Executor { return &workflow.MockExecutor{} }
	if err := m.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	run, err := m.Launch(ctx, LaunchRequest{WorkflowJSON: json.RawMessage(testDefRaw), Workspace: t.TempDir()})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	waitFor(t, func() bool { return run.Status() == RunWaitingApproval }, 3*time.Second)
	if err := m.Approve(run.ID, "ap", true, "", nil); err != nil {
		t.Fatalf("approve: %v", err)
	}
	waitFor(t, func() bool { return run.Status() == RunCompleted }, 3*time.Second)

	recs, err := st.ListRuns(ctx, 10)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(recs) != 1 || recs[0].ID != run.ID || recs[0].Status != "completed" {
		t.Fatalf("persisted run = %+v, want %s completed", recs, run.ID)
	}
	nodes, err := st.ListNodeExecutions(ctx, run.ID)
	if err != nil {
		t.Fatalf("list node executions: %v", err)
	}
	if len(nodes) < 2 {
		t.Fatalf("expected node executions persisted, got %d", len(nodes))
	}
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
