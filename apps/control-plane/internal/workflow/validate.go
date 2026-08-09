package workflow

import (
	"fmt"
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
	case NodeJoin, NodeSubworkflow:
		// join is edge-defined; subworkflow is reserved for v1 (schema accepts,
		// scheduler rejects at run time).
	default:
		return fmt.Errorf("workflow %s: node %q unknown type %q", wfID, id, n.Type)
	}
	return nil
}
