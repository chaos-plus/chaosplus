package workflow

import (
	"context"
	"encoding/json"
	"fmt"
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
	node     *Node
	status   Status
	output   json.RawMessage
	preview  *struct {
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
	seq       int
	events    []Event
	OnEvent   func(Event) // live lifecycle hook (nil-safe); fires on every mark()
}

// NewEngine validates the def and indexes the graph. Loop bodies are computed
// from bodyEntry and excluded from main-graph scheduling; fork template nodes
// are likewise excluded (they only materialize as clones).
func NewEngine(def *WorkflowDef, exec Executor) (*Engine, error) {
	if err := def.Validate(); err != nil {
		return nil, err
	}
	e := &Engine{
		def:       def,
		exec:      exec,
		states:    make(map[string]*nodeState, len(def.Nodes)),
		out:       make(map[string][]Edge),
		in:        make(map[string][]Edge),
		bodyOf:    make(map[string][]string),
		templates: make(map[string]bool),
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
		st.status = StatusFailed
		st.err = err.Error()
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
