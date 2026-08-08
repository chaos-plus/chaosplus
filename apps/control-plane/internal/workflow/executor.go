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
	// (run context ∪ completed node outputs). Returns the node's output JSON.
	RunAgent(ctx context.Context, node *Node, input json.RawMessage) (json.RawMessage, error)
	// Approve decides a human_approval gate. false = rejected.
	Approve(ctx context.Context, node *Node) (bool, error)
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

func (m *MockExecutor) Approve(ctx context.Context, node *Node) (bool, error) {
	_ = node
	return true, nil
}

var _ Executor = (*MockExecutor)(nil)
