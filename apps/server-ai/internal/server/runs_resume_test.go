package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/store"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/workflow"
)

func persistedRun(t *testing.T, st *store.Store, id string, def workflow.WorkflowDef, events ...RunEvent) {
	t.Helper()
	defJSON, _ := json.Marshal(def)
	if err := st.SaveRunDefinition(context.Background(), store.RunDef{
		ID: id, DefJSON: string(defJSON), Status: string(RunRunning), ContextJSON: `{}`,
		Workspace: t.TempDir(), InstanceID: "desktop", ProjectID: "project-1",
	}); err != nil {
		t.Fatal(err)
	}
	for i, event := range events {
		event.RunID = id
		payload, _ := json.Marshal(event)
		if err := st.Append(context.Background(), store.Event{
			ID: id + "-event-" + string(rune('a'+i)), RunID: id, Type: storeTypeFor(event),
			IdempotencyKey: id + "-key-" + string(rune('a'+i)), PayloadJSON: string(payload),
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLoadFromStoreTakesOverOnlyAfterPreviousLeaseReleases(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "takeover.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	def := workflow.WorkflowDef{ID: "takeover", Version: "1", Nodes: []workflow.Node{{
		ID: "work", Type: workflow.NodeAgent, Agent: &workflow.ExecutorAgentSpec{ID: "work", Executor: "mock"},
	}}}
	persistedRun(t, st, "run-takeover", def, RunEvent{Seq: 1, NodeID: "work", Status: workflow.StatusRunning})
	oldLease, err := st.AcquireRunLease(ctx, "run-takeover", "old-control", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	nc := startTestNATS(t)
	m := NewRunManager(nc, nil, st, "")
	var calls atomic.Int32
	m.baseFactory = func(string) workflow.Executor {
		return &workflow.MockExecutor{RunAgentFn: func(context.Context, *workflow.Node, json.RawMessage) (workflow.AgentResult, error) {
			calls.Add(1)
			return workflow.AgentResult{Output: json.RawMessage(`{"ok":true}`)}, nil
		}}
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	m.LoadFromStore(ctx)
	time.Sleep(200 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatal("run executed while the previous control plane held a live lease")
	}
	if _, visible := m.Get("run-takeover"); visible {
		t.Fatal("foreign-owned run was exposed as locally controlled")
	}
	if err := st.ReleaseRunLease(ctx, oldLease); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		run, ok := m.Get("run-takeover")
		return ok && run.Status() == RunCompleted
	}, 4*time.Second)
	if calls.Load() != 1 {
		t.Fatalf("takeover executed %d times, want once", calls.Load())
	}
}

func TestLoadFromStoreDoesNotReviveRunCompletedWhileWaitingForLease(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "terminal-takeover.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	def := workflow.WorkflowDef{ID: "terminal", Version: "1", Nodes: []workflow.Node{{
		ID: "work", Type: workflow.NodeAgent, Agent: &workflow.ExecutorAgentSpec{ID: "work", Executor: "mock"},
	}}}
	persistedRun(t, st, "run-terminal", def, RunEvent{Seq: 1, NodeID: "work", Status: workflow.StatusRunning})
	oldLease, err := st.AcquireRunLease(ctx, "run-terminal", "old-control", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	nc := startTestNATS(t)
	m := NewRunManager(nc, nil, st, "")
	var calls atomic.Int32
	m.baseFactory = func(string) workflow.Executor {
		return &workflow.MockExecutor{RunAgentFn: func(context.Context, *workflow.Node, json.RawMessage) (workflow.AgentResult, error) {
			calls.Add(1)
			return workflow.AgentResult{}, nil
		}}
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	m.LoadFromStore(ctx)
	terminal := RunEvent{Seq: 2, RunID: "run-terminal", RunStatus: RunCompleted}
	payload, _ := json.Marshal(terminal)
	if err := st.CommitRunEvents(ctx, oldLease, []store.Event{{
		ID: "run-terminal-2", RunID: "run-terminal", Type: "RUN_COMPLETED",
		IdempotencyKey: "run-terminal:completed:2", PayloadJSON: string(payload),
	}}); err != nil {
		t.Fatal(err)
	}
	if err := st.ReleaseRunLease(ctx, oldLease); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatalf("completed run was revived %d times", calls.Load())
	}
	if _, visible := m.Get("run-terminal"); visible {
		t.Fatal("terminal run should remain history-only after takeover refresh")
	}
}

func TestLoadFromStoreResumesInterruptedAgentAtNextAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "resume.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	def := workflow.WorkflowDef{
		ID: "resume-agent", Version: "1",
		Nodes: []workflow.Node{
			{ID: "done", Type: workflow.NodeAgent, Agent: &workflow.ExecutorAgentSpec{ID: "done", Executor: "mock"}},
			{ID: "interrupted", Type: workflow.NodeAgent, Agent: &workflow.ExecutorAgentSpec{ID: "interrupted", Executor: "mock"}},
		},
		Edges: []workflow.Edge{{From: "done", To: "interrupted", Condition: workflow.EdgeSuccess}},
	}
	persistedRun(t, st, "run-resume-agent", def,
		RunEvent{Seq: 1, NodeID: "done", Status: workflow.StatusCompleted, Output: json.RawMessage(`{"done":true}`), Attempt: 0},
		RunEvent{Seq: 2, NodeID: "interrupted", Status: workflow.StatusRunning, Attempt: 0},
	)

	nc := startTestNATS(t)
	m := NewRunManager(nc, nil, st, "")
	var mu sync.Mutex
	calls := []string{}
	m.baseFactory = func(string) workflow.Executor {
		return &workflow.MockExecutor{RunAgentFn: func(_ context.Context, node *workflow.Node, _ json.RawMessage) (workflow.AgentResult, error) {
			mu.Lock()
			calls = append(calls, node.ID)
			mu.Unlock()
			return workflow.AgentResult{Output: json.RawMessage(`{"ok":true}`)}, nil
		}}
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	m.LoadFromStore(ctx)
	waitFor(t, func() bool {
		run, ok := m.Get("run-resume-agent")
		return ok && run.Status() == RunCompleted
	}, 5*time.Second)
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 || calls[0] != "interrupted" {
		t.Fatalf("resumed calls = %v, want only interrupted", calls)
	}
	nodes, err := st.ListNodeExecutions(ctx, "run-resume-agent")
	if err != nil {
		t.Fatal(err)
	}
	foundAttempt2 := false
	for _, node := range nodes {
		if node.NodeID == "interrupted" && node.Attempt == 2 && node.Status == string(workflow.StatusCompleted) {
			foundAttempt2 = true
		}
	}
	if !foundAttempt2 {
		t.Fatalf("resumed attempt 2 not persisted: %+v", nodes)
	}
}

func TestLoadFromStoreRestoresLiveApproval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "approval.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	def := workflow.WorkflowDef{
		ID: "resume-approval", Version: "1",
		Nodes: []workflow.Node{{
			ID: "review", Type: workflow.NodeHumanApproval,
			HumanApproval: &workflow.HumanApprovalSpec{Approvers: workflow.Approvers{Any: true}, TimeoutMs: 60000, OnTimeout: "pause", OnReject: "pause"},
		}},
	}
	persistedRun(t, st, "run-resume-approval", def,
		RunEvent{Seq: 1, NodeID: "review", Status: workflow.StatusWaitingApproval},
	)
	nc := startTestNATS(t)
	m := NewRunManager(nc, nil, st, "")
	m.baseFactory = func(string) workflow.Executor { return &workflow.MockExecutor{} }
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	m.LoadFromStore(ctx)
	waitFor(t, func() bool {
		run, ok := m.Get("run-resume-approval")
		return ok && run.Status() == RunWaitingApproval
	}, 5*time.Second)
	if err := m.Approve("run-resume-approval", "review", true, "approved after restart", nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		run, _ := m.Get("run-resume-approval")
		return run.Status() == RunCompleted
	}, 5*time.Second)
}
