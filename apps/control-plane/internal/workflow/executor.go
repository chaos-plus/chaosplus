package workflow

import (
	"context"
	"encoding/json"
)

// Executor runs agent nodes and decides human-approval gates. The engine is
// executor-agnostic (PRD §18): M1 ships MockExecutor; a runner-backed executor
// dispatches via the NATS RunnerGateway in a later phase.
type Executor interface {
	// RunAgent executes an agent node. input is the JSON Logic variable scope
	// (run context ∪ completed node outputs), plus a `rejection_feedback` key
	// (last structured rejection for this node, PRD §13 / F.8 layer 4) and a
	// `last_error` key (previous attempt's failure), if any. Returns the node's
	// output JSON.
	RunAgent(ctx context.Context, node *Node, input json.RawMessage) (json.RawMessage, error)
	// Approve decides a human_approval gate. The returned Decision carries the
	// resolution: OK (approved) or rejected with structured Feedback (PRD §13).
	Approve(ctx context.Context, node *Node) (Decision, error)
}

// MockExecutor is a deterministic executor for tests and the M1 example. Agent
// output is `{"ok":true,"node":"<id>"}`; approvals always pass. Override
// RunAgent to script scenario behavior in tests.
type MockExecutor struct {
	RunAgentFn func(ctx context.Context, node *Node, input json.RawMessage) (json.RawMessage, error)
}

func (m *MockExecutor) RunAgent(ctx context.Context, node *Node, input json.RawMessage) (json.RawMessage, error) {
	if m.RunAgentFn != nil {
		return m.RunAgentFn(ctx, node, input)
	}
	out, err := json.Marshal(map[string]any{"ok": true, "node": node.ID})
	return out, err
}

func (m *MockExecutor) Approve(ctx context.Context, node *Node) (Decision, error) {
	_ = node
	return Decision{OK: true}, nil
}

var _ Executor = (*MockExecutor)(nil)
