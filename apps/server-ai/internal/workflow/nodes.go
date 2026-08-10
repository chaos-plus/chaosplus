package workflow

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/diegoholiveira/jsonlogic/v3"
)

// buildScope constructs the JSON Logic variable scope: run context_json ∪ each
// completed node's output keyed by node id (PRD F.4, H-3).
func (e *Engine) buildScope(contextJSON json.RawMessage) (map[string]any, error) {
	scope := make(map[string]any)
	if len(contextJSON) > 0 {
		var ctxVal any
		if err := json.Unmarshal(contextJSON, &ctxVal); err != nil {
			return nil, fmt.Errorf("workflow %s: bad context_json: %w", e.def.ID, err)
		}
		if m, ok := ctxVal.(map[string]any); ok {
			for k, v := range m {
				scope[k] = v
			}
		} else {
			scope["context"] = ctxVal
		}
	}
	for id, st := range e.states {
		if st.status == StatusCompleted && len(st.output) > 0 {
			var out any
			if err := json.Unmarshal(st.output, &out); err == nil {
				scope[id] = out
			}
		}
	}
	return scope, nil
}

func (e *Engine) execTrigger(st *nodeState) error {
	out, _ := json.Marshal(map[string]any{"trigger": st.node.Trigger.Source})
	st.output = out
	e.updateScope(st)
	e.mark(st.node.ID, StatusCompleted, out, "")
	return nil
}

func (e *Engine) execAgent(ctx context.Context, st *nodeState) error {
	result, err := e.exec.RunAgent(ctx, st.node, e.agentInput(st))
	if err != nil {
		st.err = err.Error()
		return err
	}
	st.output = result.Output
	st.preview = result.Preview
	st.err = ""
	// The node consumed its rejection feedback successfully; drop it so a later
	// unrelated execution cannot pick up a stale directive (PRD §18.1 layer 4).
	delete(e.feedbackFor, st.node.ID)
	e.updateScope(st)
	e.mark(st.node.ID, StatusCompleted, result.Output, "")
	return nil
}

func (e *Engine) execApproval(ctx context.Context, st *nodeState) error {
	e.mark(st.node.ID, StatusWaitingApproval, nil, "")
	d, err := e.exec.Approve(ctx, st.node)
	if err != nil {
		return err
	}
	st.approved = d.OK
	// A rejection carries structured feedback (PRD §13); hand it to the next
	// execution of the affected node(s) so the retry sees actionable context.
	if !d.OK && d.Feedback != nil {
		e.recordRejectionFeedback(st, d.Feedback)
	}
	out, _ := json.Marshal(map[string]any{"approved": d.OK})
	st.output = out
	e.updateScope(st)
	e.mark(st.node.ID, StatusCompleted, out, "")
	return nil
}

// execCondition evaluates expr and activates the branchKey-matched out-edge; an
// `always` out-edge is the fallback; no match at all → node fails (PRD F.4).
func (e *Engine) execCondition(st *nodeState) error {
	res, err := e.evalExpr(st.node.Condition.Expr)
	if err != nil {
		return err
	}
	st.branch = stringify(res)
	out, _ := json.Marshal(map[string]any{"branch": st.branch})
	st.output = out
	e.updateScope(st)

	matched := false
	outEdges := e.out[st.node.ID]
	for i, ed := range outEdges {
		if ed.BranchKey != "" && ed.BranchKey == st.branch {
			st.active = append(st.active, i)
			matched = true
		}
	}
	if !matched {
		for i, ed := range outEdges {
			if ed.Condition == EdgeAlways && ed.BranchKey == "" {
				st.active = append(st.active, i)
				matched = true
			}
		}
	}
	if !matched {
		return fmt.Errorf("workflow %s: condition %q no matching branch %q", e.def.ID, st.node.ID, st.branch)
	}
	e.mark(st.node.ID, StatusCompleted, out, "")
	return nil
}

func (e *Engine) execTransform(st *nodeState) error {
	res, err := e.evalExpr(st.node.Transform.Expr)
	if err != nil {
		return err
	}
	out, _ := json.Marshal(res)
	st.output = out
	e.updateScope(st)
	e.mark(st.node.ID, StatusCompleted, out, "")
	return nil
}

// execFork evaluates itemsExpr and clones the template node per element
// (PRD H-3). Clones are added to the graph with fork→clone and clone→join
// edges, so the join naturally waits for the actual expansion count.
func (e *Engine) execFork(st *nodeState) error {
	res, err := e.evalExpr(st.node.FanOut.ItemsExpr)
	if err != nil {
		return err
	}
	items, ok := res.([]any)
	if !ok {
		return fmt.Errorf("workflow %s: fork %q itemsExpr did not yield an array", e.def.ID, st.node.ID)
	}

	tpl := e.states[st.node.FanOut.TemplateNodeID]
	if tpl == nil {
		return fmt.Errorf("workflow %s: fork %q templateNodeId %q not found", e.def.ID, st.node.ID, st.node.FanOut.TemplateNodeID)
	}

	// The fork's downstream join is the first join target of its out-edges.
	joinID := ""
	for _, ed := range e.out[st.node.ID] {
		if e.states[ed.To].node.Type == NodeJoin {
			joinID = ed.To
			break
		}
	}

	for i, item := range items {
		cloneID := fmt.Sprintf("%s#%d", st.node.FanOut.TemplateNodeID, i)
		clone := *tpl.node
		clone.ID = cloneID // clones are distinct nodes with their own identity
		e.states[cloneID] = &nodeState{node: &clone, status: StatusPending}
		e.out[st.node.ID] = append(e.out[st.node.ID], Edge{From: st.node.ID, To: cloneID, Condition: EdgeAlways})
		e.in[cloneID] = append(e.in[cloneID], Edge{From: st.node.ID, To: cloneID, Condition: EdgeAlways})
		e.scope[cloneID] = item // element injected into the clone's context (H-3)
		if joinID != "" {
			e.in[joinID] = append(e.in[joinID], Edge{From: cloneID, To: joinID, Condition: EdgeAlways})
			e.out[cloneID] = append(e.out[cloneID], Edge{From: cloneID, To: joinID, Condition: EdgeAlways})
		}
	}
	out, _ := json.Marshal(map[string]any{"count": len(items)})
	st.output = out
	e.updateScope(st)
	e.mark(st.node.ID, StatusCompleted, out, "")
	return nil
}

// execGroup runs an inline subgraph (GroupSpec) as a nested DAG on the same
// executor. The group entry gets the current scope; the last completed node's
// output becomes the group output. Groups nest arbitrarily.
func (e *Engine) execGroup(ctx context.Context, st *nodeState) error {
	g := st.node.Group
	if g == nil || len(g.Nodes) == 0 {
		return fmt.Errorf("workflow %s: group %q has no nodes", e.def.ID, st.node.ID)
	}
	subDef := &WorkflowDef{
		ID:    fmt.Sprintf("%s.%s", e.def.ID, st.node.ID),
		Nodes: g.Nodes, Edges: g.Edges,
	}
	// Map external scope into subgraph context.
	subCtx := make(map[string]any)
	for k, v := range e.scope {
		subCtx[k] = v
	}
	for extKey, intKey := range g.InputMapping {
		if v, ok := e.scope[extKey]; ok {
			subCtx[intKey] = v
		}
	}
	ctxJSON, _ := json.Marshal(subCtx)

	subEngine, err := NewEngine(subDef, e.exec)
	if err != nil {
		return fmt.Errorf("workflow %s: group %q: %w", e.def.ID, st.node.ID, err)
	}
	subEngine.OnEvent = e.OnEvent
	result, err := subEngine.Run(ctx, ctxJSON)
	if err != nil {
		return err
	}
	// Propagate inner failures: if any subgraph node failed, the group fails.
	for _, ev := range result {
		if ev.Status == StatusFailed {
			return fmt.Errorf("workflow %s: group %q: node %q failed: %s",
				e.def.ID, st.node.ID, ev.NodeID, ev.Error)
		}
	}
	// Last completed node's output = group output.
	var lastOutput json.RawMessage
	for i := len(result) - 1; i >= 0; i-- {
		if result[i].Status == StatusCompleted && len(result[i].Output) > 0 {
			lastOutput = result[i].Output
			break
		}
	}
	if lastOutput == nil {
		lastOutput, _ = json.Marshal(map[string]any{"ok": true})
	}
	// outputMapping: internalKey → externalScopeKey
	if len(lastOutput) > 0 {
		var outMap map[string]any
		if json.Unmarshal(lastOutput, &outMap) == nil {
			for intKey, extKey := range g.OutputMapping {
				if v, ok := outMap[intKey]; ok {
					e.scope[extKey] = v
				}
			}
		}
	}
	st.output = lastOutput
	e.updateScope(st)
	e.mark(st.node.ID, StatusCompleted, lastOutput, "")
	return nil
}

// execJoin implements AND semantics: any failed upstream (after retry) fails
// the join (PRD §7.3).
func (e *Engine) execJoin(id string) error {
	st := e.states[id]
	for _, ed := range e.in[id] {
		if e.states[ed.From].status == StatusFailed {
			return fmt.Errorf("workflow %s: join %q upstream %q failed", e.def.ID, id, ed.From)
		}
	}
	out, _ := json.Marshal(map[string]any{"join": "ok"})
	st.output = out
	e.updateScope(st)
	e.mark(id, StatusCompleted, out, "")
	return nil
}

// execLoop re-runs the body subgraph until loop.condition holds or
// maxIterations is exhausted (PRD §7.2, §23.A).
func (e *Engine) execLoop(ctx context.Context, id string) error {
	st := e.states[id]
	loop := st.node.Loop
	body := e.bodyOf[id]

	for iter := 1; iter <= loop.MaxIterations; iter++ {
		// Reset body states, then run the body subgraph to a fixed point.
		// rejection_feedback is NOT cleared here: a rejection at the end of one
		// iteration must reach the fixer when it re-runs at the start of the
		// next (producer-as-fixer loops). execAgent deletes the entry once the
		// node consumes it successfully, so stale entries cannot leak.
		for _, bid := range body {
			bs := e.states[bid]
			bs.status = StatusPending
			bs.output = nil
			bs.branch = ""
			bs.approved = false
			bs.attempts = 0
			bs.err = ""
			bs.retryAt = 0
		}
		if err := e.runBody(ctx, body); err != nil {
			return err
		}
		// loop.condition is evaluated over the (fresh) scope.
		ok, err := e.evalBool(loop.Condition)
		if err != nil {
			return err
		}
		if ok {
			out, _ := json.Marshal(map[string]any{"iterations": iter})
			st.output = out
			e.updateScope(st)
			e.mark(id, StatusCompleted, out, "")
			return nil
		}
	}
	return fmt.Errorf("workflow %s: loop %q did not converge in %d iterations", e.def.ID, id, loop.MaxIterations)
}

// runBody executes only the body subgraph (which may itself fork/join) until
// every body node is terminal.
func (e *Engine) runBody(ctx context.Context, body []string) error {
	bodySet := make(map[string]bool, len(body))
	for _, b := range body {
		bodySet[b] = true
	}
	for {
		progressed := false
		for _, id := range body {
			st := e.states[id]
			if !bodySet[id] || st.status != StatusPending {
				continue
			}
			if !e.ready(id) {
				continue
			}
			if err := e.execute(ctx, id); err != nil {
				return err
			}
			progressed = true
		}
		if !progressed {
			break
		}
	}
	for _, id := range body {
		if !terminal(e.states[id].status) {
			return fmt.Errorf("workflow %s: loop body stuck at node %q", e.def.ID, id)
		}
	}
	return nil
}

// evalExpr evaluates a JSON Logic expression against the run scope.
func (e *Engine) evalExpr(expr json.RawMessage) (any, error) {
	var rule any
	if err := json.Unmarshal(expr, &rule); err != nil {
		return nil, fmt.Errorf("workflow %s: bad expr: %w", e.def.ID, err)
	}
	return jsonlogic.ApplyInterface(rule, e.scope)
}

func (e *Engine) evalBool(expr json.RawMessage) (bool, error) {
	res, err := e.evalExpr(expr)
	if err != nil {
		return false, err
	}
	return truthy(res), nil
}

func (e *Engine) updateScope(st *nodeState) {
	if len(st.output) == 0 {
		return
	}
	var out any
	if err := json.Unmarshal(st.output, &out); err == nil {
		e.scope[st.node.ID] = out
	}
}

func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		return fmt.Sprintf("%v", t)
	case json.Number:
		return t.String()
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

func truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	case float64:
		return t != 0
	case []any:
		return len(t) > 0
	case map[string]any:
		return len(t) > 0
	}
	return true
}
