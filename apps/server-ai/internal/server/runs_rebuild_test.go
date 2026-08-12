package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/store"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/workflow"
)

func TestWorldReconstructionRebuildsCoreProjections(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "wrt.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	nc := startTestNATS(t)
	m := NewRunManager(nc, nil, st, "")
	m.baseFactory = func(string) workflow.Executor {
		return &workflow.MockExecutor{RunAgentFn: func(_ context.Context, node *workflow.Node, _ json.RawMessage) (workflow.AgentResult, error) {
			id, path, checksum := "source", "source.json", "sha256:source"
			if node.ID == "build" {
				id, path, checksum = "build", "build.json", "sha256:build"
			}
			return workflow.AgentResult{Output: json.RawMessage(`{"ok":true}`), Artifacts: []workflow.ProducedArtifact{{
				ID: id, Path: path, Type: "json", Checksum: checksum, SizeBytes: 11, RunnerID: "runner-1", SpawnID: "spawn-" + node.ID,
			}}}, nil
		}}
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	def := workflow.WorkflowDef{ID: "wrt", Version: "1", Name: "WRT", Nodes: []workflow.Node{
		{ID: "source", Type: workflow.NodeAgent, Agent: &workflow.ExecutorAgentSpec{ID: "source", Role: "source", Executor: "mock", OutputSpec: &workflow.OutputSpec{Produces: []workflow.ProduceSpec{{ID: "source", Path: "source.json", Type: "json", Required: true}}}}},
		{ID: "review", Type: workflow.NodeHumanApproval, HumanApproval: &workflow.HumanApprovalSpec{Approvers: workflow.Approvers{Any: true}, TimeoutMs: 60000, OnTimeout: "pause", OnReject: "pause"}},
		{ID: "build", Type: workflow.NodeAgent, Agent: &workflow.ExecutorAgentSpec{ID: "build", Role: "build", Executor: "mock", InputSpec: &workflow.InputSpec{Consumes: []workflow.ConsumeSpec{{ID: "source", Type: "json", Required: true}}}, OutputSpec: &workflow.OutputSpec{Produces: []workflow.ProduceSpec{{ID: "build", Path: "build.json", Type: "json", Required: true}}}}},
	}, Edges: []workflow.Edge{{From: "source", To: "review", Condition: workflow.EdgeSuccess}, {From: "review", To: "build", Condition: workflow.EdgeApproved}}}
	raw, _ := json.Marshal(def)
	run, err := m.Launch(ctx, LaunchRequest{WorkflowJSON: raw, Context: json.RawMessage(`{"ticket":"CP-1"}`), Workspace: t.TempDir(), InstanceID: "instance-wrt", ProjectID: "project-wrt"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return run.Status() == RunWaitingApproval }, 3*time.Second)
	if err := m.Approve(run.ID, "review", true, "approved", nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return run.Status() == RunCompleted }, 3*time.Second)

	beforeRun, err := st.GetRunDef(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeNodes, _ := st.ListNodeExecutions(ctx, run.ID)
	beforeArtifacts, _ := st.ListArtifacts(ctx, "instance-wrt", "project-wrt", "")
	beforeValidations, _ := st.ListValidationResults(ctx, run.ID, 10)
	var buildID string
	for _, artifact := range beforeArtifacts {
		if artifact.LogicalID == "build" {
			buildID = artifact.ID
		}
	}
	beforeDeps, _ := st.ListArtifactDependencies(ctx, buildID)
	if len(beforeNodes) == 0 || len(beforeArtifacts) != 2 || len(beforeDeps) != 1 || len(beforeValidations) != 1 {
		t.Fatalf("precondition projections incomplete: nodes=%d artifacts=%d deps=%d validations=%d", len(beforeNodes), len(beforeArtifacts), len(beforeDeps), len(beforeValidations))
	}

	if err := st.RebuildCoreProjections(ctx); err != nil {
		t.Fatal(err)
	}
	afterRun, err := st.GetRunDef(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	afterNodes, _ := st.ListNodeExecutions(ctx, run.ID)
	afterArtifacts, _ := st.ListArtifacts(ctx, "instance-wrt", "project-wrt", "")
	afterDeps, _ := st.ListArtifactDependencies(ctx, buildID)
	afterValidations, _ := st.ListValidationResults(ctx, run.ID, 10)
	if afterRun.Status != beforeRun.Status || afterRun.DefJSON != beforeRun.DefJSON || afterRun.ContextJSON != beforeRun.ContextJSON || afterRun.Workspace != beforeRun.Workspace {
		t.Fatalf("run projection mismatch after rebuild: before=%+v after=%+v", beforeRun, afterRun)
	}
	if len(afterNodes) != len(beforeNodes) || len(afterArtifacts) != len(beforeArtifacts) || len(afterDeps) != len(beforeDeps) || len(afterValidations) != len(beforeValidations) {
		t.Fatalf("projection counts mismatch after rebuild: nodes %d/%d artifacts %d/%d deps %d/%d validations %d/%d",
			len(afterNodes), len(beforeNodes), len(afterArtifacts), len(beforeArtifacts), len(afterDeps), len(beforeDeps), len(afterValidations), len(beforeValidations))
	}
}
