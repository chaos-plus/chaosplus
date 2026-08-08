package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/gateway"
)

// RunnerExecutor dispatches agent nodes to a real machine runner over NATS
// (PRD §18: executor via runner spawn). Each agent is asked to write its
// deliverable to output.json in the workspace; RunnerExecutor reads it back
// after spawn-done, so downstream condition/transform nodes see real output.
//
// The engine runs synchronously, so at most one SpawnAndWait is in flight and
// draining the gateway's event stream is safe.
type RunnerExecutor struct {
	g         *gateway.Gateway
	runnerID  string
	workspace string // cwd every agent spawns in (workspace root, PRD artifact paths resolve here)
	runID     string
	seq       int
	mu        sync.Mutex
}

// NewRunnerExecutor wires an Executor to one runner + workspace for a run.
func NewRunnerExecutor(g *gateway.Gateway, runnerID, workspace, runID string) *RunnerExecutor {
	return &RunnerExecutor{g: g, runnerID: runnerID, workspace: workspace, runID: runID}
}

func (r *RunnerExecutor) RunAgent(ctx context.Context, node *Node, input json.RawMessage) (json.RawMessage, error) {
	r.mu.Lock()
	r.seq++
	spawnID := fmt.Sprintf("%s-%s-%d", r.runID, node.ID, r.seq)
	r.mu.Unlock()

	prompt := r.buildPrompt(node, input)
	res, err := r.g.SpawnAndWait(ctx, r.runnerID, gateway.Spawn{
		RunID:        r.runID,
		NodeID:       node.ID,
		Attempt:      1,
		SpawnID:      spawnID,
		ExecutorType: node.Agent.Executor,
		Prompt:       prompt,
		Cwd:          r.workspace,
		SystemPrompt: node.Agent.SystemPrompt,
	})
	if err != nil {
		return nil, fmt.Errorf("node %s: spawn: %w", node.ID, err)
	}
	if !res.OK {
		return nil, fmt.Errorf("node %s: agent failed (exit %d): %s", node.ID, res.ExitCode, res.Error)
	}

	out, err := r.g.ReadArtifact(ctx, r.runnerID, spawnID, "output.json")
	if err != nil {
		return nil, fmt.Errorf("node %s: read output.json: %w", node.ID, err)
	}
	// Real agents often write a UTF-8 BOM (EF BB BF) ahead of the JSON — strip
	// it before validating so the object survives round-trips.
	out = bytes.TrimPrefix(out, []byte{0xEF, 0xBB, 0xBF})
	if !json.Valid(out) {
		return nil, fmt.Errorf("node %s: output.json is not valid JSON", node.ID)
	}
	return out, nil
}

func (r *RunnerExecutor) Approve(ctx context.Context, node *Node) (bool, error) {
	_ = ctx
	_ = node
	return true, nil // V1-M1: no web UI yet; approval is wired in V1-M2
}

var _ Executor = (*RunnerExecutor)(nil)

// buildPrompt tells the agent what to produce and that its deliverable must
// land in output.json (the read-back contract for the engine's node output).
func (r *RunnerExecutor) buildPrompt(node *Node, input json.RawMessage) string {
	p := "Complete the task below. Your final deliverable MUST be written to the file `output.json` "
	p += "in the workspace root, as a single JSON object. Do not put anything else in that file.\n\n"
	if node.Agent.SystemPrompt != "" {
		p += "Role: " + node.Agent.SystemPrompt + "\n\n"
	}
	if len(input) > 0 {
		p += "Context (JSON):\n" + string(input) + "\n"
	}
	return p
}
