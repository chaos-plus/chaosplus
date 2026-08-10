// Package server is the server-ai's HTTP + WebSocket + run-orchestration
// surface (PRD §3/C2/C3): REST commands, WS realtime, NATS fan-out, StateStore
// event log. Browser never touches the store (P1).
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/store"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/workflow"
)

const runSubjectPrefix = "chaos.run."

// RunStatus is a run's coarse lifecycle state for the UI.
type RunStatus string

const (
	RunRunning         RunStatus = "running"
	RunWaitingApproval RunStatus = "waiting_approval"
	RunCompleted       RunStatus = "completed"
	RunFailed          RunStatus = "failed"
	RunPaused          RunStatus = "paused"
)

// ReviewInfo carries a human-approval resolution in the event stream.
type ReviewInfo struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason,omitempty"`
	// Feedback carries the structured rejection payload (PRD §13).
	Feedback *workflow.Feedback `json:"feedback,omitempty"`
}

// RunEvent is the wire/UI event: node lifecycle + run-level + review metadata.
type RunEvent struct {
	Seq    int             `json:"seq"`
	RunID  string          `json:"runId"`
	NodeID string          `json:"nodeId,omitempty"`
	Status workflow.Status `json:"status"`
	// RunStatus is set only on run-level lifecycle events (RUN_STARTED/…).
	RunStatus RunStatus       `json:"runStatus,omitempty"`
	Output    json.RawMessage `json:"output,omitempty"`
	Error     string          `json:"error,omitempty"`
	Review    *ReviewInfo     `json:"review,omitempty"`
	Preview   *struct {
		Type    string `json:"type"`
		Content string `json:"content"`
	} `json:"preview,omitempty"`
}

// RunSubscriber receives live events for one run (a WS connection's channel).
type RunSubscriber chan RunEvent

// Run is one in-memory workflow run (PRD workflow_runs table deferred).
type Run struct {
	ID      string
	Def     *workflow.WorkflowDef
	Broker  *workflow.ApprovalBroker
	created time.Time

	mu     sync.Mutex
	status RunStatus
	events []RunEvent
	subs   map[RunSubscriber]struct{}
	seq    int
	cancel context.CancelFunc
}

func (r *Run) publish(ev RunEvent) {
	r.mu.Lock()
	r.events = append(r.events, ev)
	for sub := range r.subs {
		select {
		case sub <- ev: // buffered; drop if the client is too slow rather than block the engine
		default:
		}
	}
	r.mu.Unlock()
}

// subscribe registers a live channel and returns a snapshot of buffered
// events to replay before draining the live channel (so nothing is missed or
// double-sent between the snapshot and the drain).
func (r *Run) subscribe() (RunSubscriber, []RunEvent, func()) {
	ch := make(RunSubscriber, 64)
	r.mu.Lock()
	r.subs[ch] = struct{}{}
	hist := append([]RunEvent(nil), r.events...)
	r.mu.Unlock()
	return ch, hist, func() { r.mu.Lock(); delete(r.subs, ch); r.mu.Unlock() }
}

// Events returns a snapshot of the event log (safe for concurrent reads).
func (r *Run) Events() []RunEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]RunEvent(nil), r.events...)
}

// hasSeq reports whether the run already recorded the event with this seq. The
// NATS subscriber uses it to skip the round-trip re-delivery of an event that
// was emitted and published locally by this instance (each run event otherwise
// reaches WS subscribers twice: local delivery + the NATS fan-out echo).
func (r *Run) hasSeq(seq int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ev := range r.events {
		if ev.Seq == seq {
			return true
		}
	}
	return false
}

// Status returns the run's lifecycle state (safe for concurrent reads).
func (r *Run) Status() RunStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

func (r *Run) setStatus(s RunStatus) {
	r.mu.Lock()
	r.status = s
	r.mu.Unlock()
}

func (r *Run) nextSeq() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	return r.seq
}

// LaunchRequest is the POST /api/runs body. Exactly one of WorkflowFile /
// WorkflowJSON must be set.
type LaunchRequest struct {
	WorkflowFile string          `json:"workflowFile"`
	WorkflowJSON json.RawMessage `json:"workflowJSON"`
	Workspace    string          `json:"workspace"`
	Context      json.RawMessage `json:"context"`
	RunnerID     string          `json:"runnerId"`
}

// RunManager owns live runs and the NATS fan-out to their WS subscribers.
type RunManager struct {
	nc       *nats.Conn
	link     workflow.RunnerLink
	st       *store.Store
	runnerID string

	mu          sync.Mutex
	runs        map[string]*Run
	seq         int
	sub         *nats.Subscription
	baseFactory func(runID string) workflow.Executor // test seam; nil → RunnerExecutor
	picker      workflow.MachinePicker               // per-node machine selector (nil = use runnerID)
}

func NewRunManager(nc *nats.Conn, link workflow.RunnerLink, st *store.Store, runnerID string) *RunManager {
	return &RunManager{nc: nc, link: link, st: st, runnerID: runnerID, runs: make(map[string]*Run)}
}

// SetMachinePicker configures per-node machine dispatch. When set, each agent
// node in a workflow run is dispatched to the best machine for its executor type.
func (m *RunManager) SetMachinePicker(p workflow.MachinePicker) {
	m.picker = p
}

// Start subscribes chaos.run.*.evt and fans out each event to the matching
// run's subscribers. Safe to call once.
func (m *RunManager) Start(ctx context.Context) error {
	sub, err := m.nc.Subscribe(runSubjectPrefix+">", func(msg *nats.Msg) {
		var ev RunEvent
		if err := json.Unmarshal(msg.Data, &ev); err != nil {
			slog.Warn("bad run event", "err", err)
			return
		}
		m.mu.Lock()
		r := m.runs[ev.RunID]
		m.mu.Unlock()
		// Skip the NATS round-trip echo of an event this instance already
		// published locally; events from other instances (which were never in
		// the local history) still arrive via this subscriber.
		if r != nil && !r.hasSeq(ev.Seq) {
			r.publish(ev)
		}
	})
	if err != nil {
		return fmt.Errorf("subscribe run events: %w", err)
	}
	m.sub = sub
	go func() {
		<-ctx.Done()
		_ = sub.Unsubscribe()
	}()
	return nil
}

func (m *RunManager) List() []*Run {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Run, 0, len(m.runs))
	for _, r := range m.runs {
		out = append(out, r)
	}
	return out
}

func (m *RunManager) Get(id string) (*Run, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[id]
	return r, ok
}

// randRunSuffix returns a short random hex so run IDs stay unique across restarts.
func randRunSuffix() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "x"
	}
	return hex.EncodeToString(b)
}

func (m *RunManager) newRun(def *workflow.WorkflowDef) *Run {
	m.mu.Lock()
	m.seq++
	// 进程内计数器 + 随机后缀:重启后计数器归零,若只用序号就会与历史 run 的
	// 事件主键/幂等键碰撞,导致新事件被静默丢弃(§15.1 事件溯源被破坏)。
	id := fmt.Sprintf("run-%d-%s", m.seq, randRunSuffix())
	run := &Run{
		ID:      id,
		Def:     def,
		status:  RunRunning,
		Broker:  workflow.NewApprovalBroker(),
		subs:    make(map[RunSubscriber]struct{}),
		created: time.Now(),
	}
	m.runs[id] = run
	m.mu.Unlock()
	return run
}

// Launch loads + validates a workflow, then runs it on a goroutine against the
// per-run broker-backed executor. Returns immediately; progress arrives via
// Events / WS.
func (m *RunManager) Launch(ctx context.Context, req LaunchRequest) (*Run, error) {
	var raw []byte
	if req.WorkflowJSON != nil {
		raw = req.WorkflowJSON
	} else if req.WorkflowFile != "" {
		if !filepath.IsLocal(req.WorkflowFile) {
			return nil, fmt.Errorf("workflowFile must be a local path")
		}
		b, err := os.ReadFile(req.WorkflowFile)
		if err != nil {
			return nil, fmt.Errorf("read workflow: %w", err)
		}
		raw = b
	} else {
		return nil, fmt.Errorf("workflowFile or workflowJSON is required")
	}
	var def workflow.WorkflowDef
	if err := json.Unmarshal(raw, &def); err != nil {
		return nil, fmt.Errorf("parse workflow: %w", err)
	}
	if err := def.Validate(); err != nil {
		return nil, fmt.Errorf("validate workflow: %w", err)
	}
	run := m.newRun(&def)
	if req.Workspace == "" {
		req.Workspace = filepath.Join(os.TempDir(), "run-"+run.ID)
	}
	// Persist the run definition so it survives restarts (PRD §15.1).
	if m.st != nil {
		defJSON, _ := json.Marshal(&def)
		_ = m.st.SaveRunDefinition(context.Background(), store.RunDef{
			ID: run.ID, DefJSON: string(defJSON), Status: string(RunRunning),
		})
	}
	runCtx, cancel := context.WithCancel(ctx)
	run.mu.Lock()
	run.cancel = cancel
	run.mu.Unlock()
	broker := run.Broker
	broker.OnDecision = func(nodeID string, d workflow.Decision) {
		m.emit(run, RunEvent{
			NodeID: nodeID, Status: workflow.StatusCompleted,
			Review: &ReviewInfo{Approved: d.OK, Reason: d.Reason, Feedback: d.Feedback},
		})
		m.recordReviewProjection(run, nodeID, d)
	}

	var base workflow.Executor
	if m.baseFactory != nil {
		base = m.baseFactory(run.ID)
	} else {
		runnerID := req.RunnerID
		if runnerID == "" {
			runnerID = m.runnerID
		}
		if runnerID == "" {
			reg := m.link.RegisteredRunners()
			if len(reg) == 0 {
				cancel()
				m.failPersistedRun(run.ID)
				return nil, fmt.Errorf("no runner registered; set runnerId or start a daemon")
			}
			runnerID = reg[0]
		}
		base = workflow.NewRunnerExecutor(m.link, runnerID, req.Workspace, run.ID).
			WithMachinePicker(m.picker)
	}
	exec := workflow.NewApprovalExecutor(base, broker)

	eng, err := workflow.NewEngine(&def, exec)
	if err != nil {
		cancel()
		m.failPersistedRun(run.ID)
		return nil, err
	}
	m.emit(run, RunEvent{RunStatus: RunRunning})
	go func() {
		defer cancel()
		eng.OnEvent = func(ev workflow.Event) {
			if ev.Status == workflow.StatusWaitingApproval {
				run.setStatus(RunWaitingApproval)
				if m.st != nil {
					_ = m.st.UpdateRunStatus(context.Background(), run.ID, string(RunWaitingApproval))
				}
			}
			m.emit(run, RunEvent{
				Seq: ev.Seq, NodeID: ev.NodeID,
				Status: ev.Status, Output: ev.Output, Error: ev.Error,
				Preview: ev.Preview,
			})
		}
		_, err := eng.Run(runCtx, req.Context)
		final := m.finalStatus(run, err)
		run.setStatus(final)
		// Run-level terminal event (F.2 RUN_COMPLETED/FAILED/PAUSED).
		m.emit(run, RunEvent{RunStatus: run.Status()})
		// Persist terminal status so restart doesn't show stale "running".
		if m.st != nil {
			_ = m.st.UpdateRunStatus(context.Background(), run.ID, string(final))
		}
	}()
	return run, nil
}

// finalStatus derives the terminal run status from the engine result and the
// run's approval outcomes (PRD F.5: onReject=pause → paused, not silent end).
func (m *RunManager) finalStatus(run *Run, err error) RunStatus {
	if err != nil {
		return RunFailed
	}
	for _, ev := range run.Events() {
		if ev.NodeID == "" || ev.Review == nil {
			continue
		}
		if !ev.Review.Approved {
			if n := nodeByID(run.Def, ev.NodeID); n != nil && n.HumanApproval != nil && n.HumanApproval.OnReject == "pause" {
				return RunPaused
			}
		}
	}
	// A run fails only when some node's FINAL status is failed: a transient
	// failure that later retried and succeeded must not fail the run (§13).
	last := make(map[string]workflow.Status)
	for _, ev := range run.Events() {
		if ev.NodeID != "" {
			last[ev.NodeID] = ev.Status
		}
	}
	for _, s := range last {
		if s == workflow.StatusFailed {
			return RunFailed
		}
	}
	return RunCompleted
}

// nodeCountFromSnapshot counts the nodes in a persisted def snapshot (best
// effort; 0 on a malformed snapshot).
func nodeCountFromSnapshot(snap string) int {
	var def workflow.WorkflowDef
	if err := json.Unmarshal([]byte(snap), &def); err != nil {
		return 0
	}
	return len(def.Nodes)
}

func nodeByID(def *workflow.WorkflowDef, id string) *workflow.Node {
	for i := range def.Nodes {
		if def.Nodes[i].ID == id {
			return &def.Nodes[i]
		}
	}
	return nil
}

// Approve resolves a human-approval gate. 404/409 handled by the HTTP layer.
func (m *RunManager) Approve(runID, nodeID string, ok bool, reason string, fb *workflow.Feedback) error {
	run, found := m.Get(runID)
	if !found {
		return fmt.Errorf("run %s not found", runID)
	}
	return run.Broker.Resolve(nodeID, ok, reason, fb)
}

// emit delivers an event to the run's subscribers, publishes it to NATS
// (cluster fan-out), and persists it to the StateStore when configured.
// Local delivery happens FIRST so the NATS echo of this same instance finds the
// seq already in the run history and dedups (hasSeq) instead of double-posting.
func (m *RunManager) emit(run *Run, ev RunEvent) {
	ev.Seq = run.nextSeq()
	ev.RunID = run.ID
	run.publish(ev)
	m.persistNodeExecution(run, ev)
	data, _ := json.Marshal(ev)
	if err := m.nc.Publish(runSubjectPrefix+run.ID+".evt", data); err != nil {
		slog.Warn("publish run event", "err", err)
	}
	if m.st != nil {
		typ := storeTypeFor(ev)
		payload, _ := json.Marshal(ev)
		if err := m.st.Append(context.Background(), store.Event{
			ID:             fmt.Sprintf("%s-%d", run.ID, ev.Seq),
			InstanceID:     "desktop",
			RunID:          run.ID,
			Type:           typ,
			IdempotencyKey: fmt.Sprintf("%s:%s:%d", run.ID, typ, ev.Seq),
			PayloadJSON:    string(payload),
		}); err != nil {
			// 事件必须可重放:落库失败要看得见,不能吞。
			slog.Error("persist run event", "run", run.ID, "seq", ev.Seq, "type", typ, "err", err)
		}
	}
}

// failPersistedRun marks a run failed in the store after a launch error, so a
// run that never started does not linger as an active (running) row.
func (m *RunManager) failPersistedRun(runID string) {
	if m.st == nil {
		return
	}
	if err := m.st.UpdateRunStatus(context.Background(), runID, string(RunFailed)); err != nil {
		slog.Warn("mark failed launch run", "run", runID, "err", err)
	}
}

// persistNodeExecution mirrors a node lifecycle event into the node_executions
// projection (PRD §16). Best-effort; the events table remains authoritative.
func (m *RunManager) persistNodeExecution(run *Run, ev RunEvent) {
	if m.st == nil || ev.NodeID == "" {
		return
	}
	completedAt := int64(0)
	switch ev.Status {
	case workflow.StatusCompleted, workflow.StatusFailed, workflow.StatusSkipped:
		completedAt = time.Now().UnixMilli()
	}
	if err := m.st.UpsertNodeExecution(context.Background(), store.NodeExecution{
		RunID: run.ID, NodeID: ev.NodeID, Attempt: 1,
		Status: string(ev.Status), Error: ev.Error, CompletedAt: completedAt,
	}); err != nil {
		slog.Warn("persist node execution", "run", run.ID, "node", ev.NodeID, "err", err)
	}
}

// recordReviewProjection persists the human-approval verdict as audit
// projections (PRD F.2): validation_results for every review, feedback_log for
// rejections (the structured feedback). Best-effort: the events table (already
// written by emit) is the authoritative, rebuildable source of truth (§15.1),
// so a projection write failure never loses a decision. Errors are logged.
func (m *RunManager) recordReviewProjection(run *Run, nodeID string, d workflow.Decision) {
	if m.st == nil {
		return
	}
	ctx := context.Background()
	passed := 0
	if d.OK {
		passed = 1
	}
	evidence, _ := json.Marshal(map[string]any{"reason": d.Reason})
	v := store.ValidationResult{
		ID: "vr-" + randHex(8), ArtifactID: run.ID, ExecutionID: nodeID,
		ValidatorID: "human", ValidatorType: "human", Passed: passed,
		EvidenceJSON: string(evidence), ReviewedBy: "human",
	}
	if d.OK {
		if err := m.st.RecordValidationResult(ctx, v); err != nil {
			slog.Warn("record validation result", "run", run.ID, "node", nodeID, "err", err)
		}
		return
	}
	// Rejection: the failed verdict and its structured feedback land atomically.
	f := store.FeedbackLogEntry{
		ID: "fb-" + randHex(8), ArtifactID: run.ID, ExecutionID: nodeID,
		Reviewer: "human", Category: string(d.Feedback.Category),
		Location: d.Feedback.Location, Expected: d.Feedback.Expected,
		Detail: d.Feedback.Detail,
	}
	if err := m.st.RecordRejection(ctx, v, f); err != nil {
		slog.Warn("record rejection projection", "run", run.ID, "node", nodeID, "err", err)
	}
}

func storeTypeFor(ev RunEvent) string {
	// Run-level lifecycle events (F.2 RUN_STARTED/COMPLETED/FAILED/PAUSED).
	if ev.RunStatus != "" {
		switch ev.RunStatus {
		case RunRunning:
			return "RUN_STARTED"
		case RunCompleted:
			return "RUN_COMPLETED"
		case RunFailed:
			return "RUN_FAILED"
		case RunPaused, RunWaitingApproval:
			return "RUN_PAUSED"
		}
	}
	switch {
	case ev.NodeID == "":
		return "RUN_EVENT"
	case ev.Review != nil:
		if ev.Review.Approved {
			return "REVIEW_APPROVED"
		}
		return "REVIEW_REJECTED"
	case ev.Status == workflow.StatusWaitingApproval:
		return "REVIEW_REQUESTED"
	case ev.Status == workflow.StatusRetrying:
		// F.2: a failed attempt with a retry scheduled is NODE_RETRY_SCHEDULED,
		// not a terminal NODE_FAILED — consumers must not treat it as failure.
		return "NODE_RETRY_SCHEDULED"
	default:
		return "NODE_" + string(ev.Status)
	}
}
