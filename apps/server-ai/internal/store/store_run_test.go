package store

import (
	"context"
	"testing"
)

// node_executions upsert: last status for (run,node,attempt=1) wins.
func TestNodeExecutionRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

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
		t.Fatalf("list: %v", err)
	}
	if len(ne) != 1 || ne[0].Status != "completed" {
		t.Fatalf("node executions = %+v, want completed", ne)
	}
}
