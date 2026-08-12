package workflow

import "testing"

func TestNodeExecutionRoundTrip(t *testing.T) {
	repository, ctx := newTestRepository(t)
	if err := repository.SaveRunDefinition(ctx, RunDef{ID: 41, ProjectID: 51, DefJSON: `{"id":"wf","version":"1","nodes":[],"edges":[]}`, ContextJSON: `{}`, Workspace: "/tmp/work"}); err != nil {
		t.Fatal(err)
	}
	for _, state := range []struct{ status, errText string }{{"running", ""}, {"retrying", "flaky"}, {"completed", ""}} {
		if err := repository.UpsertNodeExecution(ctx, NodeExecution{TenantID: 11, EntityID: 21, RunID: 41, NodeKey: "a0", Attempt: 1, Status: state.status, Error: state.errText, CompletedAt: 123}); err != nil {
			t.Fatalf("upsert %s: %v", state.status, err)
		}
	}
	nodes, err := repository.ListNodeExecutions(ctx, 41)
	if err != nil || len(nodes) != 1 || nodes[0].Status != "completed" {
		t.Fatalf("node executions = (%+v, %v), want completed", nodes, err)
	}
}
