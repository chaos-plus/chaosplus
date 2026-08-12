package workflow

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

func workflowTestContext() context.Context {
	return authn.WithClaims(context.Background(), &authn.Claims{TenantID: 11, EntityID: 21, PrincipalID: 31})
}

func newTestRunManager(t *testing.T, nc *nats.Conn) *RunManager {
	t.Helper()
	repository, _ := newTestRepository(t)
	next := guid.ID(1000)
	return NewRunManager(nc, nil, repository, "runner-1", 91, func() (guid.ID, error) {
		next++
		return next, nil
	})
}

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
	ctx, stop := context.WithCancel(workflowTestContext())
	defer stop()
	m := newTestRunManager(t, nc)
	var calls atomic.Int32
	m.baseFactory = func(_ guid.ID) Executor {
		return &MockExecutor{RunAgentFn: func(ctx context.Context, _ *Node, _ json.RawMessage) (AgentResult, error) {
			if calls.Add(1) == 1 {
				<-ctx.Done()
				return AgentResult{}, ctx.Err()
			}
			return AgentResult{Output: json.RawMessage(`{"ok":true}`)}, nil
		}}
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	def := json.RawMessage(`{"id":"lifecycle","version":"1","nodes":[{"id":"agent","type":"agent","agent":{"id":"agent","role":"dev","executor":"mock"}}],"edges":[]}`)
	run, err := m.Launch(ctx, LaunchRequest{WorkflowJSON: def, Workspace: t.TempDir(), ProjectID: 51})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		for _, event := range run.Events() {
			if event.NodeID == "agent" && event.Status == StatusRunning {
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
	m.baseFactory = func(_ guid.ID) Executor {
		return &MockExecutor{RunAgentFn: func(ctx context.Context, _ *Node, _ json.RawMessage) (AgentResult, error) {
			cancelCalls.Add(1)
			<-ctx.Done()
			return AgentResult{}, ctx.Err()
		}}
	}
	cancelRun, err := m.Launch(ctx, LaunchRequest{WorkflowJSON: def, Workspace: t.TempDir(), ProjectID: 51})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return cancelCalls.Load() > 0 }, 3*time.Second)
	if err := m.Cancel(cancelRun.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return cancelRun.Status() == RunCancelled }, 3*time.Second)
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
	ctx, stop := context.WithCancel(workflowTestContext())
	defer stop()

	m := newTestRunManager(t, nc)
	m.baseFactory = func(_ guid.ID) Executor { return &MockExecutor{} }
	if err := m.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	run, err := m.Launch(ctx, LaunchRequest{WorkflowJSON: json.RawMessage(testDefRaw), Workspace: t.TempDir(), ProjectID: 51})
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

func TestRunManagerPersistsRunBeforeAcquiringLease(t *testing.T) {
	nc := startTestNATS(t)
	ctx, stop := context.WithCancel(workflowTestContext())
	defer stop()

	repository, _ := newTestRepository(t)
	next := guid.ID(1000)
	m := NewRunManager(nc, nil, repository, "runner-1", 91, func() (guid.ID, error) {
		next++
		return next, nil
	})
	m.baseFactory = func(_ guid.ID) Executor { return &MockExecutor{} }
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	run, err := m.Launch(ctx, LaunchRequest{
		WorkflowJSON: json.RawMessage(`{"id":"persisted","version":"1","nodes":[{"id":"start","type":"trigger","trigger":{"source":"manual"}}],"edges":[]}`),
		Workspace:    t.TempDir(), ProjectID: 51,
	})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repository.GetRunDef(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ID != run.ID || stored.ProjectID != 51 || stored.TenantID != run.TenantID || stored.EntityID != run.EntityID {
		t.Fatalf("stored run = %+v", stored)
	}
	switch stored.Status {
	case RunRunning, RunCompleted:
	default:
		t.Fatalf("stored run status = %q", stored.Status)
	}
}

func TestRunManagerRejectPauses(t *testing.T) {
	nc := startTestNATS(t)
	ctx, stop := context.WithCancel(workflowTestContext())
	defer stop()

	m := newTestRunManager(t, nc)
	m.baseFactory = func(_ guid.ID) Executor { return &MockExecutor{} }
	if err := m.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	run, err := m.Launch(ctx, LaunchRequest{WorkflowJSON: json.RawMessage(testDefRaw), Workspace: t.TempDir(), ProjectID: 51})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	waitFor(t, func() bool { return run.Status() == RunWaitingApproval }, 3*time.Second)

	if err := m.Approve(run.ID, "ap", false, "wrong spec", &Feedback{Category: FeedbackDeviation, Detail: "与需求不符"}); err != nil {
		t.Fatalf("reject: %v", err)
	}
	waitFor(t, func() bool { return run.Status() == RunPaused }, 3*time.Second)
}

func TestRunManagerRejectRoutingWithRejectedEdge(t *testing.T) {
	nc := startTestNATS(t)
	ctx, stop := context.WithCancel(workflowTestContext())
	defer stop()

	m := newTestRunManager(t, nc)
	m.baseFactory = func(_ guid.ID) Executor { return &MockExecutor{} }
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

	run, err := m.Launch(ctx, LaunchRequest{WorkflowJSON: defJSON, Workspace: t.TempDir(), ProjectID: 51})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	waitFor(t, func() bool { return run.Status() == RunWaitingApproval }, 3*time.Second)
	if err := m.Approve(run.ID, "ap", false, "nope", &Feedback{Category: FeedbackFunctional, Detail: "功能不对"}); err != nil {
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
