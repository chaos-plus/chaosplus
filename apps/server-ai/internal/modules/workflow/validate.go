package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Graph bounds (M5): a workflow beyond these is rejected at validation so a
// hostile/large def cannot stall the scheduler or bloat agent prompts.
const (
	maxWorkflowNodes = 500
	maxWorkflowEdges = 2000
)

// maxBackoffSeconds caps a single retry backoff so a typo cannot stall the
// synchronous scheduler for an unreasonable time (it remains cancellable).
const maxBackoffSeconds = 3600

// maxRetryAttempts caps total attempts so a fast-failing agent cannot drive an
// unbounded number of spawns (cost explosion, M2).
const maxRetryAttempts = 10

// Validate checks structural invariants of a WorkflowDef before any run:
// unique node ids, edges reference existing nodes, edge conditions are
// predefined (PRD §7.3 forbids eval), and the graph is acyclic.
func (d *WorkflowDef) Validate() error {
	if d.ID == "" {
		return fmt.Errorf("workflow: missing id")
	}
	if d.Version == "" {
		return fmt.Errorf("workflow: missing version")
	}
	if len(d.ContextSchema) > 0 {
		if _, err := compileContextSchema(d.ContextSchema); err != nil {
			return fmt.Errorf("workflow %s: invalid contextSchema: %w", d.ID, err)
		}
	}

	nodes := make(map[string]*Node, len(d.Nodes))
	for i := range d.Nodes {
		n := &d.Nodes[i]
		if n.ID == "" {
			return fmt.Errorf("workflow %s: node %d missing id", d.ID, i)
		}
		if _, dup := nodes[n.ID]; dup {
			return fmt.Errorf("workflow %s: duplicate node id %q", d.ID, n.ID)
		}
		nodes[n.ID] = n
	}
	if len(nodes) == 0 {
		return fmt.Errorf("workflow %s: no nodes", d.ID)
	}
	// M5 (round-3 review): bound the graph so a huge def cannot drive the
	// O(n²)-ish scheduler or giant agent prompts.
	if len(nodes) > maxWorkflowNodes || len(d.Edges) > maxWorkflowEdges {
		return fmt.Errorf("workflow %s: too many nodes/edges (max %d nodes, %d edges)", d.ID, maxWorkflowNodes, maxWorkflowEdges)
	}

	for _, e := range d.Edges {
		if nodes[e.From] == nil {
			return fmt.Errorf("workflow %s: edge from %q references unknown node", d.ID, e.From)
		}
		if nodes[e.To] == nil {
			return fmt.Errorf("workflow %s: edge to %q references unknown node", d.ID, e.To)
		}
		if e.Condition != "" {
			switch e.Condition {
			case EdgeSuccess, EdgeFailed, EdgeApproved, EdgeRejected, EdgeAlways:
			default:
				return fmt.Errorf("workflow %s: edge %q→%q invalid condition %q", d.ID, e.From, e.To, e.Condition)
			}
		}
		// branchKey is only meaningful on condition-node out-edges; enforcing
		// the exact rule here would require per-node checks, so validate the
		// cheap part (non-empty condition/branchKey combos) and leave the rest
		// to the scheduler.
	}

	// Acyclic: Kahn's algorithm on the edge graph.
	indeg := make(map[string]int, len(nodes))
	for id := range nodes {
		indeg[id] = 0
	}
	for _, e := range d.Edges {
		indeg[e.To]++
	}
	queue := []string{}
	for id, n := range indeg {
		if n == 0 {
			queue = append(queue, id)
		}
	}
	seen := 0
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		seen++
		for _, e := range d.Edges {
			if e.From == id {
				indeg[e.To]--
				if indeg[e.To] == 0 {
					queue = append(queue, e.To)
				}
			}
		}
	}
	if seen != len(nodes) {
		return fmt.Errorf("workflow %s: graph contains a cycle (loop nodes must use a loop body, not back-edges)", d.ID)
	}

	// human_approval nodes must route only via approved/rejected out-edges
	// (F.5): a default success edge would let rejection flow downstream.
	// A terminal approval node (no out-edges, e.g. sprint-delivery) is valid.
	for id, n := range nodes {
		if n.Type != NodeHumanApproval {
			continue
		}
		for _, e := range d.Edges {
			if e.From != id {
				continue
			}
			if e.Condition != EdgeApproved && e.Condition != EdgeRejected {
				return fmt.Errorf("workflow %s: approval node %q out-edge to %q must be 'approved' or 'rejected' (got %q)", d.ID, id, e.To, e.Condition)
			}
		}
	}

	// Per-type required fields.
	for id, n := range nodes {
		if err := validateNodeFields(d.ID, id, n); err != nil {
			return err
		}
	}
	return nil
}

func validateNodeFields(wfID, id string, n *Node) error {
	switch n.Type {
	case NodeAgent:
		if n.Agent == nil {
			return fmt.Errorf("workflow %s: node %q (agent) missing agent spec", wfID, id)
		}
		if n.Agent.Executor == "" {
			return fmt.Errorf("workflow %s: node %q (agent) missing executor", wfID, id)
		}
		if n.Agent.Executor == "script" {
			if strings.TrimSpace(n.Agent.Script) == "" {
				return fmt.Errorf("workflow %s: node %q script executor requires a non-empty script", wfID, id)
			}
		} else if n.Agent.Script != "" {
			return fmt.Errorf("workflow %s: node %q script is only valid for the script executor", wfID, id)
		}
		// These fields are part of the public contract but the current runner
		// protocol cannot enforce them yet. Reject them at authoring time instead
		// of silently weakening an approved agent spec.
		if len(n.Agent.ForbiddenActions) > 0 {
			return fmt.Errorf("workflow %s: node %q forbiddenActions is not supported by this runner; refusing to run without enforcement", wfID, id)
		}
		if len(n.Agent.AllowedMCPTools) > 0 {
			return fmt.Errorf("workflow %s: node %q allowedMCPTools is not supported by this runner; refusing to run without enforcement", wfID, id)
		}
		if len(n.Agent.RequiredSkills) > 0 {
			return fmt.Errorf("workflow %s: node %q requiredSkills is not supported by this runner; refusing to run without injection", wfID, id)
		}
		if len(n.Agent.AllowedTools) > 0 && n.Agent.Executor != "claude" && n.Agent.Executor != "codex" {
			return fmt.Errorf("workflow %s: node %q allowedTools cannot be enforced by executor %q", wfID, id, n.Agent.Executor)
		}
		if n.Agent.Hooks != nil {
			return fmt.Errorf("workflow %s: node %q hooks are not supported by this runner; refusing to skip them", wfID, id)
		}
		if n.Agent.InputSpec != nil && n.Agent.InputSpec.InputValidator != nil {
			return fmt.Errorf("workflow %s: node %q inputValidator is not supported before spawn; refusing to skip it", wfID, id)
		}
		if n.Agent.OutputSpec != nil && n.Agent.OutputSpec.OutputValidator != nil {
			if err := validateCommandRef(*n.Agent.OutputSpec.OutputValidator); err != nil {
				return fmt.Errorf("workflow %s: node %q outputValidator: %w", wfID, id, err)
			}
		}
		for i, validator := range n.Agent.ValidatorSpecs {
			if validator.Layer != "automated" {
				return fmt.Errorf("workflow %s: node %q validatorSpecs[%d] layer %q is not supported inline; use a human_approval or agent node", wfID, id, i, validator.Layer)
			}
			if err := validateCommandRef(validator.Ref); err != nil {
				return fmt.Errorf("workflow %s: node %q validatorSpecs[%d]: %w", wfID, id, i, err)
			}
		}
		if r := n.Agent.Retry; r != nil {
			if r.MaxAttempts < 1 || r.MaxAttempts > maxRetryAttempts {
				return fmt.Errorf("workflow %s: node %q retry.maxAttempts must be in [1,%d] (got %d)", wfID, id, maxRetryAttempts, r.MaxAttempts)
			}
			for i, b := range r.BackoffSeconds {
				if b < 0 || b > maxBackoffSeconds {
					return fmt.Errorf("workflow %s: node %q retry.backoffSeconds[%d] must be in [0,%d] (got %d)", wfID, id, i, maxBackoffSeconds, b)
				}
			}
		}
	case NodeHumanApproval:
		if n.HumanApproval == nil {
			return fmt.Errorf("workflow %s: node %q (human_approval) missing humanApproval spec", wfID, id)
		}
		if n.HumanApproval.TimeoutMs < 0 {
			return fmt.Errorf("workflow %s: node %q humanApproval.timeoutMs must be non-negative", wfID, id)
		}
		if n.HumanApproval.OnTimeout != "pause" && n.HumanApproval.OnTimeout != "auto_reject" {
			return fmt.Errorf("workflow %s: node %q humanApproval.onTimeout must be pause or auto_reject", wfID, id)
		}
		if n.HumanApproval.OnReject != "pause" && n.HumanApproval.OnReject != "retry" {
			return fmt.Errorf("workflow %s: node %q humanApproval.onReject must be pause or retry", wfID, id)
		}
	case NodeCondition:
		if n.Condition == nil || len(n.Condition.Expr) == 0 {
			return fmt.Errorf("workflow %s: node %q (condition) missing condition.expr", wfID, id)
		}
	case NodeTransform:
		if n.Transform == nil || len(n.Transform.Expr) == 0 {
			return fmt.Errorf("workflow %s: node %q (transform) missing transform.expr", wfID, id)
		}
	case NodeTrigger:
		if n.Trigger == nil {
			return fmt.Errorf("workflow %s: node %q (trigger) missing trigger spec", wfID, id)
		}
	case NodeParallelFork:
		if n.FanOut == nil || len(n.FanOut.ItemsExpr) == 0 || n.FanOut.TemplateNodeID == "" {
			return fmt.Errorf("workflow %s: node %q (parallel_fork) missing fanOut (itemsExpr + templateNodeId)", wfID, id)
		}
	case NodeLoop:
		if n.Loop == nil || n.Loop.BodyEntry == "" || len(n.Loop.Condition) == 0 || n.Loop.MaxIterations <= 0 {
			return fmt.Errorf("workflow %s: node %q (loop) missing loop spec", wfID, id)
		}
	case NodeGroup:
		if n.Group == nil || len(n.Group.Nodes) == 0 {
			return fmt.Errorf("workflow %s: node %q (group) missing group spec (nodes)", wfID, id)
		}
		// Guard against stack overflow on deeply nested groups.
		if n.Group.Depth > 8 {
			return fmt.Errorf("workflow %s: group %q exceeds max nesting depth (8)", wfID, id)
		}
		// Validate the subgraph recursively, incrementing depth.
		for i := range n.Group.Nodes {
			if n.Group.Nodes[i].Group != nil {
				n.Group.Nodes[i].Group.Depth = n.Group.Depth + 1
			}
		}
		sub := &WorkflowDef{ID: fmt.Sprintf("%s.%s", wfID, id), Nodes: n.Group.Nodes, Edges: n.Group.Edges}
		if err := sub.Validate(); err != nil {
			return fmt.Errorf("workflow %s: group %q: %w", wfID, id, err)
		}
	case NodeSubworkflow:
		return fmt.Errorf("workflow %s: node %q uses unsupported subworkflow; resolve it before submission", wfID, id)
	case NodeJoin:
		// join is edge-defined.
	default:
		return fmt.Errorf("workflow %s: node %q unknown type %q", wfID, id, n.Type)
	}
	return nil
}

func validateCommandRef(ref string) error {
	if !strings.HasPrefix(strings.TrimSpace(ref), "cmd:") || strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(ref), "cmd:")) == "" {
		return fmt.Errorf("only non-empty cmd: validators are supported")
	}
	return nil
}

func compileContextSchema(raw json.RawMessage) (*jsonschema.Schema, error) {
	var document any
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("context-schema.json", document); err != nil {
		return nil, err
	}
	return compiler.Compile("context-schema.json")
}

// ValidateContext checks StartRun context_json against WorkflowDef.contextSchema.
// An absent schema accepts any valid JSON value; an absent context is `{}`.
func (d *WorkflowDef) ValidateContext(raw json.RawMessage) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("context is not valid JSON: %w", err)
	}
	if len(d.ContextSchema) == 0 {
		return nil
	}
	schema, err := compileContextSchema(d.ContextSchema)
	if err != nil {
		return fmt.Errorf("compile contextSchema: %w", err)
	}
	if err := schema.Validate(value); err != nil {
		return fmt.Errorf("context does not match contextSchema: %w", err)
	}
	return nil
}
