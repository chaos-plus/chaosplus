package store

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func openSharedStores(t *testing.T) (*Store, *Store) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "leases.db")
	first, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Open(context.Background(), dsn)
	if err != nil {
		_ = first.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = first.Close()
		_ = second.Close()
	})
	return first, second
}

func TestImmediateTransactionDSN(t *testing.T) {
	tests := map[string]string{
		"state.db":                           "state.db?_txlock=immediate",
		"state.db?mode=rwc":                  "state.db?mode=rwc&_txlock=immediate",
		"state.db?_txlock=exclusive":         "state.db?_txlock=exclusive",
		"state.db?mode=rwc&_txlock=deferred": "state.db?mode=rwc&_txlock=deferred",
	}
	for input, want := range tests {
		if got := immediateTransactionDSN(input); got != want {
			t.Errorf("immediateTransactionDSN(%q) = %q, want %q", input, got, want)
		}
	}
}

func runStartedEvent(runID, key string) Event {
	payload, _ := json.Marshal(map[string]any{
		"runId":     runID,
		"runStatus": "running",
		"snapshot": map[string]any{
			"workflow": map[string]any{"id": "wf", "version": "1", "nodes": []any{}, "edges": []any{}},
			"context":  map[string]any{}, "workspace": "/tmp/work", "instanceId": "i", "projectId": "p",
		},
	})
	return Event{ID: key, RunID: runID, InstanceID: "i", Type: "RUN_STARTED", IdempotencyKey: key, PayloadJSON: string(payload)}
}

func TestRunLeaseFencesPreviousWriterAcrossStoreConnections(t *testing.T) {
	ctx := context.Background()
	first, second := openSharedStores(t)
	lease1, err := first.AcquireRunLease(ctx, "run-1", "control-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.AcquireRunLease(ctx, "run-1", "control-b", time.Minute); !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("second live owner acquire = %v, want ErrLeaseHeld", err)
	}
	if err := first.ReleaseRunLease(ctx, lease1); err != nil {
		t.Fatal(err)
	}
	lease2, err := second.AcquireRunLease(ctx, "run-1", "control-b", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if lease2.FencingToken <= lease1.FencingToken {
		t.Fatalf("new fencing token %d must exceed old %d", lease2.FencingToken, lease1.FencingToken)
	}
	if err := first.CommitRunEvents(ctx, lease1, []Event{runStartedEvent("run-1", "stale")}); !errors.Is(err, ErrLeaseStale) {
		t.Fatalf("stale writer commit = %v, want ErrLeaseStale", err)
	}
	if err := second.CommitRunEvents(ctx, lease2, []Event{runStartedEvent("run-1", "current")}); err != nil {
		t.Fatal(err)
	}
	events, err := first.ListEvents(ctx, "run-1", 0, 10)
	if err != nil || len(events) != 1 || events[0].ID != "current" {
		t.Fatalf("events after takeover = (%+v, %v)", events, err)
	}
	run, err := first.GetRunDef(ctx, "run-1")
	if err != nil || run.Status != "running" || run.InstanceID != "i" {
		t.Fatalf("run projection after takeover = (%+v, %v)", run, err)
	}
}

func TestCommitRunEventsRollsBackEventWhenProjectionFails(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	lease, err := st.AcquireRunLease(ctx, "run-bad", "control-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	bad := Event{ID: "bad", RunID: "run-bad", Type: "RUN_STARTED", IdempotencyKey: "bad", PayloadJSON: `{}`}
	if err := st.CommitRunEvents(ctx, lease, []Event{bad}); err == nil {
		t.Fatal("invalid projection must fail the transaction")
	}
	events, err := st.ListEvents(ctx, "run-bad", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("rolled-back event remained: %+v", events)
	}
}

func TestCommitArtifactEventIfChecksumRejectsStaleSnapshot(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "artifact-cas.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	artifact := Artifact{ID: "artifact-1", InstanceID: "instance-1", ProjectID: "project-1",
		LogicalID: "release", LogicalPath: "release.json", Checksum: "sha256:old",
		ProducerRunID: "run-1", ProducerNode: "build"}
	if err := st.UpsertArtifact(ctx, artifact, nil); err != nil {
		t.Fatal(err)
	}
	artifact.Checksum = "sha256:new-producer"
	if err := st.UpsertArtifact(ctx, artifact, nil); err != nil {
		t.Fatal(err)
	}

	stale := artifact
	stale.Checksum = "sha256:stale-reconcile"
	stale.Status = ArtifactInvalid
	payload, err := json.Marshal(stale)
	if err != nil {
		t.Fatal(err)
	}
	committed, err := st.CommitArtifactEventIfChecksum(ctx, Event{
		ID: "stale-reconcile", InstanceID: stale.InstanceID, RunID: stale.ProducerRunID,
		Type: "ARTIFACT_INVALIDATED", IdempotencyKey: "stale-reconcile", PayloadJSON: string(payload),
	}, "sha256:old")
	if err != nil {
		t.Fatal(err)
	}
	if committed {
		t.Fatal("stale reconciliation event was committed")
	}
	got, err := st.GetArtifact(ctx, artifact.ID, artifact.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Checksum != "sha256:new-producer" || got.Status != ArtifactValid {
		t.Fatalf("artifact overwritten by stale reconciliation: %+v", got)
	}
	events, err := st.ListEvents(ctx, artifact.ProducerRunID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.ID == "stale-reconcile" {
			t.Fatal("stale reconciliation event persisted without its projection")
		}
	}
}

func TestArtifactInvalidationPersistsZeroByteSize(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	artifact := Artifact{ID: "empty", InstanceID: "i", ProjectID: "p", LogicalID: "empty",
		LogicalPath: "empty.txt", Checksum: "sha256:nonempty", SizeBytes: 10, ProducerRunID: "run"}
	if err := st.UpsertArtifact(ctx, artifact, nil); err != nil {
		t.Fatal(err)
	}
	artifact.Checksum, artifact.SizeBytes, artifact.Status = "sha256:empty", 0, ArtifactInvalid
	payload, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	committed, err := st.CommitArtifactEventIfChecksum(ctx, Event{ID: "empty-invalidated", RunID: "run",
		Type: "ARTIFACT_INVALIDATED", IdempotencyKey: "empty-invalidated", PayloadJSON: string(payload)}, "sha256:nonempty")
	if err != nil || !committed {
		t.Fatalf("commit zero-byte invalidation = (%v, %v)", committed, err)
	}
	got, err := st.GetArtifact(ctx, "empty", "i")
	if err != nil || got.SizeBytes != 0 || got.Checksum != "sha256:empty" || got.Status != ArtifactInvalid {
		t.Fatalf("zero-byte artifact projection = (%+v, %v)", got, err)
	}
}
