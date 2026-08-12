package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
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
