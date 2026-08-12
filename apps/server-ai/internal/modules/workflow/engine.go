package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrRunInterrupted is the lifecycle-control cancellation cause used by the
// server when pausing or cancelling a run. Ordinary context cancellation keeps
// its existing failure semantics.
var ErrRunInterrupted = errors.New("run interrupted by lifecycle control")

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
	StatusPausedForHuman  Status = "paused_for_human" // bounded autonomy exhausted or governance timeout
)

// Event is one node lifecycle event in run order (PRD event log §15.1).
type Event struct {
	Seq       int                `json:"seq"`
	NodeID    string             `json:"nodeId"`
	Status    Status             `json:"status"`
	Output    json.RawMessage    `json:"output,omitempty"`
	Error     string             `json:"error,omitempty"`
	Attempt   int                `json:"attempt,omitempty"`
	Artifacts []ProducedArtifact `json:"artifacts,omitempty"`
	Notify    bool               `json:"notify,omitempty"` // §13 escalation: retries crossed notifyThreshold
	Preview   *struct {
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
	branch    string // condition result
	active    []int  // indices of active out-edges (condition routing)
	approved  bool   // human_approval outcome
	attempts  int
	retryAt   int64 // unix ms when this node's retry backoff elapses; 0 = none
	err       string
	artifacts []ProducedArtifact
	// prevProduces is the previous attempt's produces (path -> sha256), used by
	// the F.10 similarity check to pause "原地打转" retries.
	prevProduces map[string]string
	notified     bool // §13: this node already crossed retry.notifyThreshold
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

// Restore rebuilds scheduler state from the authoritative node event stream.
// Completed/failed/skipped nodes stay terminal; a node interrupted while
// running, retrying, or waiting for approval becomes pending and is resumed.
func (e *Engine) Restore(events []Event) error {
	for _, ev := range events {
		st := e.states[ev.NodeID]
		if st == nil {
			continue
		}
		if ev.Attempt > st.attempts {
			st.attempts = ev.Attempt
		}
		switch ev.Status {
		case StatusCompleted:
			st.status = StatusCompleted
			st.output = append(json.RawMessage(nil), ev.Output...)
			st.artifacts = append([]ProducedArtifact(nil), ev.Artifacts...)
			if st.node.Type == NodeCondition && len(ev.Output) > 0 {
				var output struct {
					Branch string `json:"branch"`
				}
				if err := json.Unmarshal(ev.Output, &output); err != nil {
					return fmt.Errorf("restore condition %q: %w", ev.NodeID, err)
				}
				st.branch = output.Branch
				matched := false
				for i, edge := range e.out[ev.NodeID] {
					if edge.BranchKey != "" && edge.BranchKey == st.branch {
						st.active = append(st.active, i)
						matched = true
					}
				}
				if !matched {
					for i, edge := range e.out[ev.NodeID] {
						if edge.Condition == EdgeAlways && edge.BranchKey == "" {
							st.active = append(st.active, i)
						}
					}
				}
			}
			if st.node.Type == NodeHumanApproval && len(ev.Output) > 0 {
				var output struct {
					Approved bool `json:"approved"`
				}
				if json.Unmarshal(ev.Output, &output) == nil {
					st.approved = output.Approved
				}
			}
		case StatusFailed, StatusSkipped:
			st.status = ev.Status
			st.err = ev.Error
		case StatusRunning, StatusRetrying, StatusWaitingApproval, StatusPausedForHuman:
			st.status = StatusPending
			st.err = ev.Error
			if st.node.Type == NodeAgent && ev.Status != StatusWaitingApproval && ev.Attempt+1 > st.attempts {
				st.attempts = ev.Attempt + 1
			}
		}
	}
	return nil
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
			// Nothing could progress now. If a node is parked in retry backoff,
			// wait until the earliest retryAt (without blocking other branches —
			// they already had their chance this pass), then reschedule it.
			if wait := e.nextRetryAt(); wait > 0 {
				if !waitUntil(ctx, wait) {
					// Run cancelled while a node is parked in backoff: it never
					// got to retry, so surface it as failed and let the run reach
					// a terminal state (matches the prior inline-wait semantics).
					for _, st := range e.states {
						if st.status == StatusPending && st.retryAt > time.Now().UnixMilli() {
							st.retryAt = 0
							e.mark(st.node.ID, StatusFailed, nil, "run cancelled during retry backoff")
						}
					}
					continue
				}
				continue
			}
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
	if st.retryAt > time.Now().UnixMilli() {
		return false // in backoff; not ready until retryAt elapses
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
		// Pause/cancel/restart interrupts an in-flight node. Leave its latest event
		// as running so Restore schedules a fresh attempt; do not persist a false
		// business failure caused only by lifecycle control.
		if errors.Is(context.Cause(ctx), ErrRunInterrupted) {
			return context.Cause(ctx)
		}
		if errors.Is(err, ErrApprovalTimedOut) {
			st.err = err.Error()
			e.mark(id, StatusPausedForHuman, nil, err.Error())
			return nil
		}
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
		if st.node.Type == NodeAgent && st.node.Agent != nil && st.node.Agent.Retry != nil {
			e.mark(id, StatusPausedForHuman, nil, err.Error())
			return nil
		}
		// Terminal failure: no retries left (or the run was cancelled during
		// backoff). A single NODE_FAILED, not one per attempt.
		e.mark(id, StatusFailed, nil, err.Error())
	}
	return nil
}

// producesMap indexes artifacts by path so consecutive attempts can be compared
// by checksum (PRD F.10 similarity detection).
func producesMap(artifacts []ProducedArtifact) map[string]string {
	m := make(map[string]string, len(artifacts))
	for _, a := range artifacts {
		m[a.Path] = a.Checksum
	}
	return m
}

// recordAttemptProduces keeps the previous attempt's produces as the baseline
// and installs the current attempt's produces, so scheduleRetry can detect a
// node that keeps producing the same output across attempts ("原地打转").
func (e *Engine) recordAttemptProduces(st *nodeState, artifacts []ProducedArtifact) {
	if len(artifacts) == 0 {
		return
	}
	if len(st.artifacts) > 0 {
		st.prevProduces = producesMap(st.artifacts)
	}
	st.artifacts = artifacts
}

// similarityRatio returns the fraction of produces files whose checksum differs
// between consecutive attempts (PRD F.10): changed / union. Empty union -> 1
// (no baseline to compare), so callers only treat a low ratio as "原地打转".
func similarityRatio(current, previous map[string]string) float64 {
	if len(current) == 0 || len(previous) == 0 {
		return 1
	}
	union := make(map[string]struct{}, len(current)+len(previous))
	for p := range current {
		union[p] = struct{}{}
	}
	for p := range previous {
		union[p] = struct{}{}
	}
	changed := 0
	for p := range union {
		if current[p] != previous[p] {
			changed++
		}
	}
	return float64(changed) / float64(len(union))
}

// mark updates a node's status and appends a lifecycle event.
func (e *Engine) mark(id string, status Status, output json.RawMessage, errStr string) {
	st := e.states[id]
	st.status = status
	e.seq++
	// Attempt = 0-based index of the attempt this event belongs to. running/
	// completed happen before the failure counter advances; retrying/failed
	// marks fire after st.attempts++ (scheduleRetry/exhaustion), so back off one.
	attempt := st.attempts
	if status == StatusRetrying || status == StatusFailed {
		attempt = st.attempts - 1
	}
	if attempt < 0 {
		attempt = 0
	}
	ev := Event{Seq: e.seq, NodeID: id, Status: status, Error: errStr, Attempt: attempt, Notify: st.notified}
	if status == StatusCompleted && len(output) > 0 {
		ev.Output = output
		ev.Artifacts = append([]ProducedArtifact(nil), st.artifacts...)
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
	// PRD F.10: if this attempt produced almost the same files as the previous
	// one, the agent is going in circles — pause for a human instead of burning
	// more retries. attempt=1 has no baseline (similarityRatio returns 1).
	if st.attempts > 1 && similarityRatio(producesMap(st.artifacts), st.prevProduces) < 0.08 {
		return false // caller marks the node paused_for_human
	}
	// §13 escalation: once retries cross notifyThreshold, flag the retry event so
	// subscribers (IM/UI) can notify the human without blocking execution.
	if !st.notified && spec.NotifyThreshold > 0 && st.attempts >= spec.NotifyThreshold {
		st.notified = true
	}
	e.mark(st.node.ID, StatusRetrying, nil, err.Error())
	if i := st.attempts - 1; i < len(spec.BackoffSeconds) && spec.BackoffSeconds[i] > 0 {
		// Defer the wait (M6): the node is parked with a retryAt timestamp and the
		// scheduler keeps processing other ready branches; it sleeps only when no
		// node can progress, so backoff no longer stalls parallel fork branches.
		st.retryAt = time.Now().UnixMilli() + int64(spec.BackoffSeconds[i])*1000
	}
	st.status = StatusPending
	return true
}

// nextRetryAt returns the earliest future retryAt across pending nodes, or 0.
func (e *Engine) nextRetryAt() int64 {
	now := time.Now().UnixMilli()
	var earliest int64
	for _, st := range e.states {
		if st.status == StatusPending && st.retryAt > now {
			if earliest == 0 || st.retryAt < earliest {
				earliest = st.retryAt
			}
		}
	}
	return earliest
}

// waitUntil sleeps until the given unix-ms deadline, or ctx cancellation.
func waitUntil(ctx context.Context, deadlineMS int64) bool {
	d := time.Duration(deadlineMS-time.Now().UnixMilli()) * time.Millisecond
	if d < 0 {
		d = 0
	}
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
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
