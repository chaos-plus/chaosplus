package workflow

import (
	"context"
	"encoding/json"
)

// AgentResult is the output of RunAgent: the node's JSON output + optional
// inline preview for live rendering on canvas nodes (ComfyUI pattern).
type AgentResult struct {
	Output  json.RawMessage
	Preview *struct {
		Type    string `json:"type"`
		Content string `json:"content"`
	}
}

// Executor runs agent nodes and decides human-approval gates. The engine is
// executor-agnostic (PRD §18).
type Executor interface {
	// RunAgent executes an agent node. input is the JSON Logic variable scope
	// (run context ∪ completed node outputs), plus a `rejection_feedback` key
	// (last structured rejection, PRD §13 / F.8 layer 4) and a `last_error` key
	// (previous attempt's failure). Returns the node's output.
	RunAgent(ctx context.Context, node *Node, input json.RawMessage) (AgentResult, error)
	// Approve decides a human_approval gate. The returned Decision carries the
	// resolution: OK (approved) or rejected with structured Feedback (PRD §13).
	Approve(ctx context.Context, node *Node) (Decision, error)
}

// MockExecutor is a deterministic executor for tests and the M1 example.
type MockExecutor struct {
	RunAgentFn func(ctx context.Context, node *Node, input json.RawMessage) (AgentResult, error)
}

func (m *MockExecutor) RunAgent(ctx context.Context, node *Node, input json.RawMessage) (AgentResult, error) {
	if m.RunAgentFn != nil {
		return m.RunAgentFn(ctx, node, input)
	}
	out, _ := json.Marshal(map[string]any{"ok": true, "node": node.ID})
	return AgentResult{Output: out}, nil
}

func (m *MockExecutor) Approve(ctx context.Context, node *Node) (Decision, error) {
	_ = node
	return Decision{OK: true}, nil
}

var _ Executor = (*MockExecutor)(nil)
