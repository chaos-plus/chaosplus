package workflow

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

func runStartedRecord(id, runID int64, key string) EventRecord {
	payload, _ := json.Marshal(RunEvent{RunID: 41, RunStatus: RunRunning, Snapshot: &RunSnapshot{Workflow: json.RawMessage(`{"id":"wf","version":"1","nodes":[],"edges":[]}`), Context: json.RawMessage(`{}`), Workspace: "/tmp/work", CreatedAt: 100}})
	return EventRecord{ID: guidID(id), TenantID: 11, EntityID: 21, ProjectID: 51, ActorID: 31, RunID: guidID(runID), Type: "RUN_STARTED", IdempotencyKey: key, PayloadJSON: string(payload)}
}

func guidID(id int64) guid.ID { return guid.ID(id) }

func TestRunLeaseFencesPreviousWriter(t *testing.T) {
	repository, ctx := newTestRepository(t)
	if err := repository.SaveRunDefinition(ctx, RunDef{ID: 41, ProjectID: 51, DefJSON: `{"id":"wf","version":"1","nodes":[],"edges":[]}`, ContextJSON: `{}`, Workspace: "/tmp/work"}); err != nil {
		t.Fatal(err)
	}
	lease1, err := repository.AcquireRunLease(ctx, 41, 71, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.AcquireRunLease(ctx, 41, 72, time.Minute); !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("second acquire = %v", err)
	}
	if err := repository.ReleaseRunLease(ctx, lease1); err != nil {
		t.Fatal(err)
	}
	lease2, err := repository.AcquireRunLease(ctx, 41, 72, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if lease2.FencingToken <= lease1.FencingToken {
		t.Fatal("fencing token did not advance")
	}
	if err := repository.CommitRunEvents(ctx, lease1, []EventRecord{runStartedRecord(61, 41, "stale")}); !errors.Is(err, ErrLeaseStale) {
		t.Fatalf("stale commit = %v", err)
	}
	if err := repository.CommitRunEvents(ctx, lease2, []EventRecord{runStartedRecord(62, 41, "current")}); err != nil {
		t.Fatal(err)
	}
	items, err := repository.ListEvents(ctx, 41, 0, 10)
	if err != nil || len(items) != 1 || items[0].ID != 62 {
		t.Fatalf("events = (%+v, %v)", items, err)
	}
}

func TestCommitRollsBackEventWhenProjectionFails(t *testing.T) {
	repository, ctx := newTestRepository(t)
	if err := repository.SaveRunDefinition(ctx, RunDef{ID: 41, ProjectID: 51, DefJSON: `{"id":"wf","version":"1","nodes":[],"edges":[]}`, ContextJSON: `{}`, Workspace: "/tmp/work"}); err != nil {
		t.Fatal(err)
	}
	lease, err := repository.AcquireRunLease(ctx, 41, 71, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	bad := EventRecord{ID: 61, TenantID: 11, EntityID: 21, ProjectID: 51, ActorID: 31, RunID: 41, Type: "RUN_STARTED", IdempotencyKey: "bad", PayloadJSON: `{}`}
	if err := repository.CommitRunEvents(ctx, lease, []EventRecord{bad}); err == nil {
		t.Fatal("invalid projection must fail")
	}
	items, err := repository.ListEvents(ctx, 41, 0, 10)
	if err != nil || len(items) != 0 {
		t.Fatalf("rolled back events = (%+v, %v)", items, err)
	}
}

func TestListRunMetricsUsesAuthenticatedScope(t *testing.T) {
	repository, ctx := newTestRepository(t)
	if err := repository.SaveRunDefinition(ctx, RunDef{ID: 41, ProjectID: 51, DefJSON: `{"id":"wf","version":"1","nodes":[],"edges":[]}`, ContextJSON: `{"taskId":"61"}`, Workspace: "/tmp/work"}); err != nil {
		t.Fatal(err)
	}
	items, err := repository.ListRunMetrics(ctx)
	if err != nil || len(items) != 1 || items[0].ID != 41 || items[0].ContextJSON != `{"taskId":"61"}` {
		t.Fatalf("run metrics = (%+v, %v)", items, err)
	}
}
