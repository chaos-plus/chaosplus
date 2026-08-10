package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

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
	link      RunnerLink
	runnerID  string
	workspace string // cwd every agent spawns in (workspace root, PRD artifact paths resolve here)
	runID     string
	idle      time.Duration // per-spawn idle timeout (reset on any matching event); 0 = none
	max       time.Duration // per-spawn absolute cap; 0 = none
	seq       int
	mu        sync.Mutex
}

// NewRunnerExecutor wires an Executor to one runner link + workspace for a run.
func NewRunnerExecutor(link RunnerLink, runnerID, workspace, runID string) *RunnerExecutor {
	return &RunnerExecutor{link: link, runnerID: runnerID, workspace: workspace, runID: runID}
}

// WithSpawnTimeout sets the per-spawn idle timeout (reset on live activity) and
// an absolute max cap. A real agent that stops producing output but never
// terminates (SDK session stuck after writing) then fails the node instead of
// hanging the whole run forever.
func (r *RunnerExecutor) WithSpawnTimeout(idle, max time.Duration) *RunnerExecutor {
	r.idle, r.max = idle, max
	return r
}

func (r *RunnerExecutor) RunAgent(ctx context.Context, node *Node, input json.RawMessage) (json.RawMessage, error) {
	r.mu.Lock()
	r.seq++
	spawnID := fmt.Sprintf("%s-%s-%d", r.runID, node.ID, r.seq)
	r.mu.Unlock()

	prompt := r.buildPrompt(node, input)
	res, err := r.link.SpawnAndWait(ctx, r.runnerID, gateway.Spawn{
		RunID:        r.runID,
		NodeID:       node.ID,
		Attempt:      1,
		SpawnID:      spawnID,
		ExecutorType: node.Agent.Executor,
		Prompt:       prompt,
		Cwd:          r.workspace,
		SystemPrompt: node.Agent.SystemPrompt,
	}, r.idle, r.max)
	if err != nil {
		// On timeout, tell the runner to stop the stray session so it doesn't
		// keep burning tokens/CPU after we've given up on it.
		_ = r.link.Kill(context.Background(), r.runnerID, spawnID)
		return nil, fmt.Errorf("node %s: spawn: %w", node.ID, err)
	}
	if !res.OK {
		return nil, fmt.Errorf("node %s: agent failed (exit %d): %s", node.ID, res.ExitCode, res.Error)
	}

	out, err := r.link.ReadArtifact(ctx, r.runnerID, spawnID, "output.json")
	if err != nil {
		return nil, fmt.Errorf("node %s: read output.json: %w", node.ID, err)
	}
	// Real agents often write a UTF-8 BOM (EF BB BF) ahead of the JSON — strip
	// it before validating so the object survives round-trips.
	out = bytes.TrimPrefix(out, []byte{0xEF, 0xBB, 0xBF})
	if !json.Valid(out) {
		return nil, fmt.Errorf("node %s: output.json is not valid JSON", node.ID)
	}

	// PRD §10/§11: validate output against outputSpec.produces BEFORE trusting
	// the agent's self-claim. Required artifacts must exist and match declared type.
	if err := r.validateProduces(ctx, node, spawnID, out); err != nil {
		return nil, fmt.Errorf("node %s: output validation: %w", node.ID, err)
	}

	// PRD F.5 outputValidator ('cmd:<template>'): the agent's self-claim is not
	// trusted — run the real validator in the workspace; non-zero exit fails the
	// node. Pass overrides the output with {"result":"passed"} so downstream
	// condition/loop nodes see the validator's verdict, not the agent's words.
	if v := validatorCmd(node); v != "" {
		res, err := r.link.RunCmd(ctx, r.runnerID, spawnID, v, 120000)
		if err != nil {
			return nil, fmt.Errorf("node %s: validator: %w", node.ID, err)
		}
		if res.ExitCode != 0 {
			detail := strings.TrimSpace(res.Stderr)
			if detail == "" {
				detail = strings.TrimSpace(res.Stdout)
			}
			if detail == "" {
				detail = fmt.Sprintf("exit %d", res.ExitCode)
			}
			return nil, fmt.Errorf("node %s: validator failed: %s", node.ID, detail)
		}
		out, _ = json.Marshal(map[string]any{"result": "passed"})
	}
	return out, nil
}

// validateProduces reads each required artifact declared in outputSpec.produces
// and validates it exists and matches its declared type. This is the built-in
// trust boundary — it runs unconditionally, before any optional cmd: validator.
func (r *RunnerExecutor) validateProduces(ctx context.Context, node *Node, spawnID string, main json.RawMessage) error {
	if node.Agent == nil || node.Agent.OutputSpec == nil || len(node.Agent.OutputSpec.Produces) == 0 {
		return nil
	}
	for _, p := range node.Agent.OutputSpec.Produces {
		if !p.Required {
			continue
		}
		path := p.Path
		if path == "" {
			path = p.ID
		}
		// output.json is already read, validated, and held in main — re-read
		// would be wasteful.
		var body []byte
		if path == "output.json" {
			body = main
		} else {
			b, err := r.link.ReadArtifact(ctx, r.runnerID, spawnID, path)
			if err != nil {
				return fmt.Errorf("required artifact %q (%s): %w", p.ID, path, err)
			}
			body = b
		}
		if len(body) == 0 {
			return fmt.Errorf("required artifact %q (%s) is empty", p.ID, path)
		}
		switch p.Type {
		case "json", "object", "array":
			if !json.Valid(body) {
				return fmt.Errorf("required artifact %q (%s) declared as %s but is not valid JSON", p.ID, path, p.Type)
			}
		}
	}
	return nil
}

func (r *RunnerExecutor) Approve(ctx context.Context, node *Node) (bool, error) {
	_ = ctx
	_ = node
	// RunnerExecutor must be wrapped by ApprovalExecutor for human-approval
	// support. If this is reached, the caller bypassed the approval broker,
	// which silently approves every gate — a dangerous accidental config.
	return false, fmt.Errorf("Approve() requires ApprovalExecutor wrapper: do not call RunnerExecutor.Approve() directly")
}

var _ Executor = (*RunnerExecutor)(nil)

// validatorCmd extracts the 'cmd:' output-validator template from an agent
// node, or "" when none is configured.
func validatorCmd(node *Node) string {
	if node.Agent == nil || node.Agent.OutputSpec == nil || node.Agent.OutputSpec.OutputValidator == nil {
		return ""
	}
	v := strings.TrimSpace(*node.Agent.OutputSpec.OutputValidator)
	if !strings.HasPrefix(v, "cmd:") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(v, "cmd:"))
}

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
