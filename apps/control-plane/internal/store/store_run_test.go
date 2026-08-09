package store

import (
	"context"
	"testing"
)

func TestRunRecordLifecycle(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if err := s.CreateRun(ctx, RunRecord{ID: "run-1", WorkflowID: "wf", WorkflowVer: "1",
		DefSnapshot: `{"id":"wf","nodes":[]}`, Status: "running", Workspace: "ws"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	// 幂等:重复创建不报错。
	if err := s.CreateRun(ctx, RunRecord{ID: "run-1", Status: "running"}); err != nil {
		t.Fatalf("create dup: %v", err)
	}

	// 节点执行 upsert(attempt=1 最后状态胜出)。
	for _, st := range []struct{ status, errText string }{
		{"running", ""}, {"retrying", "flaky"}, {"completed", ""},
	} {
		if err := s.UpsertNodeExecution(ctx, NodeExecution{RunID: "run-1", NodeID: "a0", Attempt: 1,
			Status: st.status, Error: st.errText, CompletedAt: 123}); err != nil {
			t.Fatalf("upsert %s: %v", st.status, err)
		}
	}
	ne, err := s.ListNodeExecutions(ctx, "run-1")
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	if len(ne) != 1 || ne[0].Status != "completed" {
		t.Fatalf("node executions = %+v, want completed", ne)
	}

	if err := s.UpdateRunStatus(ctx, "run-1", "completed", 456); err != nil {
		t.Fatalf("update status: %v", err)
	}
	runs, err := s.ListRuns(ctx, 10)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 || runs[0].Status != "completed" || runs[0].CompletedAt != 456 {
		t.Fatalf("run record = %+v, want completed with completed_at 456", runs)
	}
}

// PRD §15.1: on restart, runs left mid-flight are crash-recovered to failed.
func TestCrashRecoverRunning(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	for _, id := range []string{"r-running", "r-waiting", "r-paused", "r-done"} {
		status := "running"
		if id == "r-waiting" {
			status = "waiting_approval"
		} else if id == "r-paused" {
			status = "paused"
		} else if id == "r-done" {
			status = "completed"
		}
		if err := s.CreateRun(ctx, RunRecord{ID: id, Status: status, WorkflowVer: "1"}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	n, err := s.CrashRecoverRunning(ctx)
	if err != nil {
		t.Fatalf("crash recover: %v", err)
	}
	if n != 2 {
		t.Fatalf("recovered %d runs, want 2 (running + waiting_approval)", n)
	}
	runs, _ := s.ListRuns(ctx, 10)
	byID := map[string]string{}
	for _, r := range runs {
		byID[r.ID] = r.Status
	}
	// running/waiting_approval → failed; paused (terminal-by-design, F.5) stays.
	if byID["r-running"] != "failed" || byID["r-waiting"] != "failed" || byID["r-paused"] != "paused" || byID["r-done"] != "completed" {
		t.Fatalf("crash recovery statuses wrong: %+v", byID)
	}
}
