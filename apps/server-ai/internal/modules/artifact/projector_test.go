package artifact

import (
	"encoding/json"
	"testing"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workflow"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

func TestWorkflowProjectorCreatesScopedArtifact(t *testing.T) {
	repository, ctx := newTestRepository(t)
	next := guid.ID(2000)
	projector := NewWorkflowProjector(func() (guid.ID, error) { next++; return next, nil })
	payload, err := json.Marshal(workflow.RunEvent{RunID: 51, NodeID: "build", Attempt: 0, Artifacts: []workflow.ProducedArtifact{{ID: "release", Path: "release.json", Type: "json", Checksum: "sha256:abc", SizeBytes: 11, RunnerID: "runner-1", SpawnID: "spawn-1"}}})
	if err != nil {
		t.Fatal(err)
	}
	record := workflow.EventRecord{ID: 61, TenantID: 11, EntityID: 21, ProjectID: 41, ActorID: 31, RunID: 51, TS: 123456789, Type: "NODE_COMPLETED", PayloadJSON: string(payload)}
	if err := projector.Project(ctx, repository.db, record); err != nil {
		t.Fatal(err)
	}
	items, err := repository.ListArtifacts(ctx, 21, 41, "")
	if err != nil || len(items) != 1 {
		t.Fatalf("artifacts = (%+v, %v)", items, err)
	}
	got := items[0]
	if got.ID != 2001 || got.TenantID != 11 || got.EntityID != 21 || got.ProjectID != 41 || got.OwnerID != 31 || got.CreatedAt != record.TS || got.RunnerHandle != "runner-1" || got.SpawnHandle != "spawn-1" {
		t.Fatalf("unexpected artifact projection: %+v", got)
	}
}
