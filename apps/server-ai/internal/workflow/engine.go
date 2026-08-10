package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Status is a node's terminal/live state within a run.
type Status string

const (
	StatusPending         Status = "pending"
	StatusRunning         Status = "running"
	StatusCompleted       Status = "completed"
	StatusFailed          Status = "failed"
	StatusSkipped         Status = "skipped"
	StatusWaitingApproval Status = "waiting_approval" // human_approval gate is blocked on a human
	StatusRetrying        Status = "retrying"         // an attempt failed but a retry is scheduled (F.2 NODE_RETRY_SCHEDULED); not terminal
)

// Event is one node lifecycle event in run order (PRD event log §15.1).
type Event struct {
	Seq     int             `json:"seq"`
	NodeID  string          `json:"nodeId"`
	Status  Status          `json:"status"`
	Output  json.RawMessage `json:"output,omitempty"`
	Error   string          `json:"error,omitempty"`
	Preview *struct {
		Type    string `json:"type"`
		Content string `json:"content"`
	} `json:"preview,omitempty"`
}

type nodeState struct {
	node    *Node
	status  Status
	output  json.RawMessage
	preview *struct {
		Type    string `json:"type"`
		Content string `json:"content"`
	}
	branch   string // condition result
	active   []int  // indices of active out-edges (condition routing)
	approved bool   // human_approval outcome
	attempts int
	err      string
}

// Engine is a static-DAG scheduler (PRD §7.3). It is executor-agnostic: agent
// nodes and approval gates are delegated to Executor; all graph logic
// (readiness, condition, join, fork, loop) lives here.
type Engine struct {
	def       *WorkflowDef
	exec      Executor
	states    map[string]*nodeState
	out       map[string][]Edge   // nodeID -> outgoing edges
	in        map[string][]Edge   // nodeID -> incoming edges
	bodyOf    map[string][]string // loop nodeID -> its body node IDs
	templates map[string]bool     // fork template node IDs (not scheduled directly)
	scope     map[string]any      // JSON Logic variable scope (context ∪ outputs)
	// feedbackFor maps a node ID to the last structured rejection feedback it
	// must consume on its next execution (PRD §13 / F.8 layer 4). Written when a
	// human_approval gate rejects; read by execAgent for the affected node.
	feedbackFor map[string]*Feedback
	seq         int
	events      []Event
	OnEvent     func(Event) // live lifecycle hook (nil-safe); fires on every mark()
}

// NewEngine validates the def and indexes the graph. Loop bodies are computed
// from bodyEntry and excluded from main-graph scheduling; fork template nodes
// are likewise excluded (they only materialize as clones).
func NewEngine(def *WorkflowDef, exec Executor) (*Engine, error) {
	if err := def.Validate(); err != nil {
		return nil, err
	}
	e := &Engine{
		def:         def,
		exec:        exec,
		states:      make(map[string]*nodeState, len(def.Nodes)),
		out:         make(map[string][]Edge),
		in:          make(map[string][]Edge),
		bodyOf:      make(map[string][]string),
		templates:   make(map[string]bool),
		feedbackFor: make(map[string]*Feedback),
	}
	for i := range def.Nodes {
		n := &def.Nodes[i]
		e.states[n.ID] = &nodeState{node: n, status: StatusPending}
		if n.Type == NodeParallelFork {
			e.templates[n.FanOut.TemplateNodeID] = true
		}
	}
	for _, ed := range def.Edges {
		e.out[ed.From] = append(e.out[ed.From], ed)
		e.in[ed.To] = append(e.in[ed.To], ed)
	}
	for _, n := range def.Nodes {
		if n.Type == NodeLoop {
			e.bodyOf[n.ID] = e.reachable(n.Loop.BodyEntry, map[string]bool{})
		}
	}
	return e, nil
}

// reachable returns node IDs reachable from start following out-edges.
func (e *Engine) reachable(start string, seen map[string]bool) []string {
	if seen[start] {
		return nil
	}
	seen[start] = true
	out := []string{start}
	for _, ed := range e.out[start] {
		out = append(out, e.reachable(ed.To, seen)...)
	}
	return out
}

// Run executes the DAG from the given run context (context_json). Returns the
// ordered event stream. An error is returned only for scheduling failures
// (deadlock); node failures surface as failed statuses in the event stream.
func (e *Engine) Run(ctx context.Context, contextJSON json.RawMessage) ([]Event, error) {
	scope, err := e.buildScope(contextJSON)
	if err != nil {
		return nil, err
	}
	e.scope = scope

	for {
		progressed := false
		for id := range e.states {
			st := e.states[id]
			if st.status != StatusPending || e.isLoopBody(id) || e.templates[id] {
				continue
			}
			if !e.ready(id) {
				continue
			}
			if err := e.execute(ctx, id); err != nil {
				return e.events, err
			}
			progressed = true
		}
		if !progressed {
			break
		}
	}

	// Untaken branches (condition routed elsewhere) become skipped. A pending
	// node whose in-edges are all satisfied-but-inactive is a routing deadlock.
	for id, st := range e.states {
		if st.status != StatusPending || e.isLoopBody(id) || e.templates[id] {
			continue
		}
		if e.anyActiveIn(id) {
			return e.events, fmt.Errorf("workflow %s: deadlock at node %q", e.def.ID, id)
		}
		e.mark(id, StatusSkipped, nil, "")
	}
	return e.events, nil
}

func (e *Engine) isLoopBody(id string) bool {
	for _, body := range e.bodyOf {
		for _, bid := range body {
			if bid == id {
				return true
			}
		}
	}
	return false
}

// ready implements PRD §7.3 node readiness: every incoming edge condition must
// hold. Join in-edges are effectively "always" (wait for terminal).
func (e *Engine) ready(id string) bool {
	st := e.states[id]
	if st.status != StatusPending {
		return false
	}
	if st.node.Type == NodeJoin {
		return e.allInTerminal(id)
	}
	for _, ed := range e.in[id] {
		src := e.states[ed.From]
		if !terminal(src.status) {
			return false
		}
		if src.node.Type == NodeCondition {
			if !edgeActive(src, ed, e.out[ed.From]) {
				return false
			}
			continue
		}
		if !edgeConditionHolds(src, ed.Condition) {
			return false
		}
	}
	return true
}

// allInTerminal is the join readiness rule (AND wait).
func (e *Engine) allInTerminal(id string) bool {
	for _, ed := range e.in[id] {
		if !terminal(e.states[ed.From].status) {
			return false
		}
	}
	return true
}

// anyActiveIn reports whether at least one incoming edge is active (used to
// distinguish "branch not taken" from a deadlock).
func (e *Engine) anyActiveIn(id string) bool {
	for _, ed := range e.in[id] {
		src := e.states[ed.From]
		if src.node.Type == NodeCondition {
			if edgeActive(src, ed, e.out[ed.From]) {
				return true
			}
		} else if edgeConditionHolds(src, ed.Condition) {
			return true
		}
	}
	return false
}

// execute runs a ready node and transitions its state.
func (e *Engine) execute(ctx context.Context, id string) error {
	st := e.states[id]
	e.mark(id, StatusRunning, nil, "")
	var err error
	switch st.node.Type {
	case NodeTrigger:
		err = e.execTrigger(st)
	case NodeAgent:
		err = e.execAgent(ctx, st)
	case NodeHumanApproval:
		err = e.execApproval(ctx, st)
	case NodeCondition:
		err = e.execCondition(st)
	case NodeTransform:
		err = e.execTransform(st)
	case NodeParallelFork:
		err = e.execFork(st)
	case NodeJoin:
		err = e.execJoin(id)
	case NodeLoop:
		err = e.execLoop(ctx, id)
	case NodeGroup:
		err = e.execGroup(ctx, st)
	case NodeSubworkflow:
		err = fmt.Errorf("workflow %s: subworkflow node %q not implemented in v1", e.def.ID, id)
	default:
		err = fmt.Errorf("workflow %s: node %q unknown type %q", e.def.ID, id, st.node.Type)
	}
	if err != nil {
		// n8n-style onError: "continue" passes a stub output downstream
		// instead of failing the run. Node output = {"error":..., "nodeId":..., "continued":true}.
		if st.node.OnError == "continue" {
			out, _ := json.Marshal(map[string]any{"error": err.Error(), "nodeId": id, "continued": true})
			st.output = out
			e.updateScope(st)
			e.mark(id, StatusCompleted, out, "")
			return nil
		}
		st.err = err.Error()
		if e.scheduleRetry(ctx, st, err) {
			return nil // transient failure; node reset to pending for re-run
		}
		// Terminal failure: no retries left (or the run was cancelled during
		// backoff). A single NODE_FAILED, not one per attempt.
		e.mark(id, StatusFailed, nil, err.Error())
	}
	return nil
}

// mark updates a node's status and appends a lifecycle event.
func (e *Engine) mark(id string, status Status, output json.RawMessage, errStr string) {
	st := e.states[id]
	st.status = status
	e.seq++
	ev := Event{Seq: e.seq, NodeID: id, Status: status, Error: errStr}
	if status == StatusCompleted && len(output) > 0 {
		ev.Output = output
	}
	if st.preview != nil {
		ev.Preview = st.preview
	}
	e.events = append(e.events, ev)
	if e.OnEvent != nil {
		e.OnEvent(ev)
	}
}

// recordRejectionFeedback notes that a human_approval rejection must reach the
// fixer's next execution (PRD §13 / F.8 layer 4). Only the `rejected` out-edge
// target re-runs as a consequence of the rejection, so it is the only node
// that must consume the feedback (join-fed approvals would otherwise leak the
// entry onto a non-agent node that never runs).
func (e *Engine) recordRejectionFeedback(st *nodeState, fb *Feedback) {
	for _, ed := range e.out[st.node.ID] {
		if ed.Condition == EdgeRejected {
			e.feedbackFor[ed.To] = fb
		}
	}
}

// agentInput builds the JSON Logic input for an agent node: the run scope plus
// a `rejection_feedback` key (last structured rejection, PRD §13) and a
// `last_error` key (previous attempt's failure) so a retry or fixer sees
// actionable context. Keys are namespaced to avoid colliding with a user
// context that happens to carry its own feedback/lastError fields. The global
// scope is untouched.
func (e *Engine) agentInput(st *nodeState) json.RawMessage {
	scope := make(map[string]any, len(e.scope)+2)
	for k, v := range e.scope {
		scope[k] = v
	}
	// Reserved keys are engine-owned: strip any user context value that happens
	// to carry the same name, then set them only from authoritative state — a
	// user-supplied `rejection_feedback` in the run context must never be
	// promoted into the agent's prompt as a directive (H1 / prompt injection).
	delete(scope, "rejection_feedback")
	delete(scope, "last_error")
	if fb := e.feedbackFor[st.node.ID]; fb != nil {
		scope["rejection_feedback"] = fb
	}
	if st.err != "" {
		scope["last_error"] = st.err
	}
	b, _ := json.Marshal(scope)
	return b
}

// scheduleRetry implements PRD F.5 bounded retry for agent nodes. On a failure
// with attempts remaining it emits a transient retrying status (F.2
// NODE_RETRY_SCHEDULED), waits the configured backoff, and resets the node to
// pending so the engine re-runs it — up to retry.maxAttempts total attempts.
// Exhaustion or ctx cancellation leaves the node failed (the caller emits the
// terminal failure). notifyThreshold (escalation tiers §13) is not consumed in
// v1; maxAttempts and backoffSeconds are the operative parts. ponytail: the
// backoff blocks the synchronous scheduler — acceptable while runs are serial,
// parallel branches would need to defer the wait.
func (e *Engine) scheduleRetry(ctx context.Context, st *nodeState, err error) bool {
	if st.node.Type != NodeAgent || st.node.Agent == nil || st.node.Agent.Retry == nil {
		return false
	}
	spec := st.node.Agent.Retry
	if spec.MaxAttempts <= 1 {
		return false
	}
	st.attempts++
	if st.attempts >= spec.MaxAttempts {
		return false // exhausted; caller emits the terminal failure
	}
	e.mark(st.node.ID, StatusRetrying, nil, err.Error())
	if i := st.attempts - 1; i < len(spec.BackoffSeconds) && spec.BackoffSeconds[i] > 0 {
		timer := time.NewTimer(time.Duration(spec.BackoffSeconds[i]) * time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return false // run cancelled during backoff → leave node failed
		}
	}
	st.status = StatusPending
	return true
}

func terminal(s Status) bool {
	return s == StatusCompleted || s == StatusFailed || s == StatusSkipped
}

// edgeConditionHolds checks a non-condition edge selector against the source's
// terminal state.
func edgeConditionHolds(src *nodeState, cond EdgeCondition) bool {
	if cond == "" {
		cond = EdgeSuccess
	}
	switch cond {
	case EdgeSuccess:
		return src.status == StatusCompleted
	case EdgeFailed:
		return src.status == StatusFailed
	case EdgeApproved:
		return src.status == StatusCompleted && src.approved
	case EdgeRejected:
		return src.status == StatusCompleted && !src.approved
	case EdgeAlways:
		return terminal(src.status)
	}
	return false
}

// edgeActive checks whether a condition node's out-edge is among the branch it
// selected. Fallback: an `always` edge activates only when no branchKey edge
// was taken.
func edgeActive(src *nodeState, ed Edge, outEdges []Edge) bool {
	if ed.BranchKey != "" {
		return ed.BranchKey == src.branch
	}
	if ed.Condition == EdgeAlways {
		for _, o := range outEdges {
			if o.BranchKey != "" && o.BranchKey == src.branch {
				return false // a branch edge was taken; always is not the fallback
			}
		}
		return true
	}
	return false
}
