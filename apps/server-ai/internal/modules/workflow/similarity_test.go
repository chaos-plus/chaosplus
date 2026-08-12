package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestSimilarityRatio(t *testing.T) {
	cases := []struct {
		name     string
		current  map[string]string
		previous map[string]string
		want     float64
	}{
		{"identical", map[string]string{"a": "x", "b": "y"}, map[string]string{"a": "x", "b": "y"}, 0},
		{"one changed", map[string]string{"a": "x", "b": "z"}, map[string]string{"a": "x", "b": "y"}, 0.5},
		{"no baseline", map[string]string{"a": "x"}, nil, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := similarityRatio(tc.current, tc.previous); got != tc.want {
				t.Fatalf("similarityRatio = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestSimilarityPausesStuckRetry verifies F.10: an agent that retries while
// producing near-identical files is paused_for_human instead of burning retries.
func TestSimilarityPausesStuckRetry(t *testing.T) {
	def := &WorkflowDef{ID: "wf", Version: "1", Name: "wf", Nodes: []Node{
		{ID: "gen", Type: NodeAgent, Agent: &ExecutorAgentSpec{ID: "gen", Role: "demo", Executor: "mock", SystemPrompt: "x",
			Retry: &RetrySpec{MaxAttempts: 3, BackoffSeconds: []int{0, 0}}}}},
	}
	exec := &stuckStubExecutor{
		artifacts: []ProducedArtifact{{ID: "out", Path: "output.json", Checksum: "sha256:same"}},
		err:       errors.New("stub fail"),
	}
	eng, err := NewEngine(def, exec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Run(context.Background(), json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	st := eng.states["gen"]
	if st.status != StatusPausedForHuman {
		t.Fatalf("expected paused_for_human, got %s", st.status)
	}
	if st.attempts >= 3 {
		t.Fatalf("retries were not saved: attempts=%d", st.attempts)
	}
}

// stuckStubExecutor returns fixed partial artifacts and a failure every attempt.
type stuckStubExecutor struct {
	artifacts []ProducedArtifact
	err       error
}

var _ Executor = (*stuckStubExecutor)(nil)

func (s *stuckStubExecutor) RunAgent(_ context.Context, _ *Node, _ json.RawMessage) (AgentResult, error) {
	return AgentResult{Artifacts: s.artifacts}, s.err
}

func (s *stuckStubExecutor) Approve(_ context.Context, _ *Node) (Decision, error) { return Decision{OK: true}, nil }
