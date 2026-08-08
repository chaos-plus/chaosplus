package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/gateway"
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

	m := NewRunManager(nc, gateway.New(nc), nil, "runner-1")
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
	if err := m.Approve(run.ID, "ap", true, ""); err != nil {
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

	m := NewRunManager(nc, gateway.New(nc), nil, "runner-1")
	m.baseFactory = func(_ string) workflow.Executor { return &workflow.MockExecutor{} }
	if err := m.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	run, err := m.Launch(ctx, LaunchRequest{WorkflowJSON: json.RawMessage(testDefRaw), Workspace: t.TempDir()})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	waitFor(t, func() bool { return run.Status() == RunWaitingApproval }, 3*time.Second)

	if err := m.Approve(run.ID, "ap", false, "wrong spec"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	waitFor(t, func() bool { return run.Status() == RunPaused }, 3*time.Second)
}

func TestRunManagerRejectRoutingWithRejectedEdge(t *testing.T) {
	nc := startTestNATS(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	m := NewRunManager(nc, gateway.New(nc), nil, "runner-1")
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
	if err := m.Approve(run.ID, "ap", false, "nope"); err != nil {
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
