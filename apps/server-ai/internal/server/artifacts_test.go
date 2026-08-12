package server

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/gateway"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/store"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/workflow"
)

func TestRunPersistsProducedArtifactAndEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "artifacts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	nc := startTestNATS(t)
	m := NewRunManager(nc, nil, st, "")
	m.baseFactory = func(string) workflow.Executor {
		return &workflow.MockExecutor{RunAgentFn: func(context.Context, *workflow.Node, json.RawMessage) (workflow.AgentResult, error) {
			return workflow.AgentResult{
				Output: json.RawMessage(`{"ok":true}`),
				Artifacts: []workflow.ProducedArtifact{{
					ID: "release", Path: "release.json", Type: "json", Checksum: "sha256:abc",
					SizeBytes: 11, RunnerID: "runner-1", SpawnID: "spawn-1",
				}},
			}, nil
		}}
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	def := workflow.WorkflowDef{
		ID: "artifact-run", Version: "1",
		Nodes: []workflow.Node{{ID: "build", Type: workflow.NodeAgent, Agent: &workflow.ExecutorAgentSpec{
			ID: "build", Executor: "mock",
			OutputSpec: &workflow.OutputSpec{Produces: []workflow.ProduceSpec{{ID: "release", Path: "release.json", Type: "json", Required: true}}},
		}}},
	}
	body, _ := json.Marshal(def)
	run, err := m.Launch(ctx, LaunchRequest{WorkflowJSON: body, Workspace: t.TempDir(), ProjectID: "project-1", InstanceID: "instance-1"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return run.Status() == RunCompleted }, 5*time.Second)
	artifacts, err := st.ListArtifacts(ctx, "instance-1", "project-1", "")
	if err != nil || len(artifacts) != 1 {
		t.Fatalf("artifacts = (%+v, %v)", artifacts, err)
	}
	if artifacts[0].LogicalID != "release" || artifacts[0].Checksum != "sha256:abc" || artifacts[0].Attempt != 1 {
		t.Fatalf("artifact projection = %+v", artifacts[0])
	}
	events, err := st.ListEvents(ctx, run.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		found = found || event.Type == "ARTIFACT_PRODUCED"
	}
	if !found {
		t.Fatalf("ARTIFACT_PRODUCED missing from %+v", events)
	}
}

type reconciliationLink struct {
	content []byte
	err     error
}

func (l *reconciliationLink) SpawnAndWait(context.Context, string, gateway.Spawn, time.Duration, time.Duration) (gateway.SpawnResult, error) {
	return gateway.SpawnResult{}, errors.New("not used")
}
func (l *reconciliationLink) Kill(context.Context, string, string) error { return nil }
func (l *reconciliationLink) ReadArtifact(context.Context, string, string, string) ([]byte, error) {
	return l.content, l.err
}
func (l *reconciliationLink) RunCmd(context.Context, string, string, string, int) (gateway.CmdResult, error) {
	return gateway.CmdResult{}, errors.New("not used")
}
func (l *reconciliationLink) RegisteredRunners() []string { return []string{"runner-1"} }

func TestReconcileArtifactsInvalidatesExternalChanges(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	artifact := store.Artifact{
		ID: "a", InstanceID: "i", ProjectID: "p", LogicalID: "release", LogicalPath: "release.json",
		Checksum: "sha256:old", RunnerID: "runner-1", SpawnID: "spawn-1", ProducerRunID: "run-1",
	}
	if err := st.UpsertArtifact(ctx, artifact, nil); err != nil {
		t.Fatal(err)
	}
	m := NewRunManager(nil, &reconciliationLink{content: []byte("externally changed")}, st, "")
	report, err := m.ReconcileArtifacts(ctx)
	if err != nil || report.Checked != 1 || report.Changed != 1 {
		t.Fatalf("reconcile = (%+v, %v)", report, err)
	}
	invalid, _ := st.ListArtifacts(ctx, "i", "p", store.ArtifactInvalid)
	if len(invalid) != 1 {
		t.Fatalf("invalid artifacts = %+v", invalid)
	}
}
