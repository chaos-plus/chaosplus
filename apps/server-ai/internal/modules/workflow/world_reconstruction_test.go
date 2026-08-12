package workflow

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

// TestWorldReconstruction is the PRD §15.1 publish gate: after deleting every
// projection, the control plane must reach an identical state by replaying the
// append-only event log alone (plus artifacts).
func TestWorldReconstruction(t *testing.T) {
	repository, ctx := newTestRepository(t)

	// Seed a run definition plus a complete event log for it.
	defJSON := `{"id":"wf","version":"1","name":"wf","nodes":[{"id":"trigger","type":"trigger","trigger":{"source":"manual"}},{"id":"gen","type":"agent","agent":{"id":"gen","role":"demo","executor":"mock","systemPrompt":"x"}}],"edges":[{"from":"trigger","to":"gen","condition":"success"}]}`
	if err := repository.SaveRunDefinition(ctx, RunDef{ID: 41, ProjectID: 51, DefJSON: defJSON, ContextJSON: `{"task":"t"}`, Workspace: "/tmp/work"}); err != nil {
		t.Fatal(err)
	}
	lease, err := repository.AcquireRunLease(ctx, 41, 71, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	start, _ := json.Marshal(RunEvent{RunID: 41, RunStatus: RunRunning, Snapshot: &RunSnapshot{Workflow: json.RawMessage(defJSON), Context: json.RawMessage(`{"task":"t"}`), Workspace: "/tmp/work", CreatedAt: 100}})
	completed, _ := json.Marshal(RunEvent{RunID: 41, NodeID: "gen", Status: StatusCompleted, Output: json.RawMessage(`{"ok":true}`), Attempt: 1})
	events := []EventRecord{
		{ID: guid.ID(101), TenantID: 11, EntityID: 21, ProjectID: 51, ActorID: 31, RunID: 41, Type: "RUN_STARTED", IdempotencyKey: "41:RUN_STARTED:1", PayloadJSON: string(start)},
		{ID: guid.ID(102), TenantID: 11, EntityID: 21, ProjectID: 51, ActorID: 31, RunID: 41, Type: "NODE_COMPLETED", IdempotencyKey: "41:NODE_COMPLETED:2", PayloadJSON: string(completed)},
	}
	if err := repository.CommitRunEvents(ctx, lease, events); err != nil {
		t.Fatal(err)
	}
	// Release the lease so the projection stays active for the rebuild check;
	// LoadActiveRunDefinitions filters by status/deleted_at, not lease state.
	_ = repository.ReleaseRunLease(ctx, lease)

	// Capture the pre-rebuild projection state.
	runsBefore, err := repository.LoadAllActiveRunDefinitions(ctx)
	if err != nil || len(runsBefore) != 1 {
		t.Fatalf("expected 1 active run before rebuild, got %d (err=%v)", len(runsBefore), err)
	}
	if runsBefore[0].Status != RunRunning {
		t.Fatalf("expected run status running, got %s", runsBefore[0].Status)
	}

	// Corrupt/delete projections, then rebuild purely from the event log.
	for _, table := range []string{"node_executions", "workflow_runs"} {
		if _, err := repository.db.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			t.Fatalf("delete %s: %v", table, err)
		}
	}
	if err := repository.RebuildCoreProjections(ctx); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	// Verify identical projection state after replay.
	runsAfter, err := repository.LoadAllActiveRunDefinitions(ctx)
	if err != nil || len(runsAfter) != 1 {
		t.Fatalf("expected 1 active run after rebuild, got %d (err=%v)", len(runsAfter), err)
	}
	if runsAfter[0].Status != runsBefore[0].Status || runsAfter[0].Workspace != runsBefore[0].Workspace || runsAfter[0].DefJSON != runsBefore[0].DefJSON {
		t.Fatalf("run projection diverged after replay: before=%+v after=%+v", runsBefore[0], runsAfter[0])
	}
	var nodes []struct {
		NodeKey string `bun:"node_key"`
		Status  string `bun:"status"`
	}
	if err := repository.db.NewSelect().Table("node_executions").Column("node_key", "status").Scan(ctx, &nodes); err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].NodeKey != "gen" || nodes[0].Status != string(StatusCompleted) {
		t.Fatalf("node projection diverged after replay: %+v", nodes)
	}
}
