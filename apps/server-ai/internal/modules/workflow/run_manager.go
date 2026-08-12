// Package workflow owns workflow execution, persistence, and event fan-out.
package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/nats-io/nats.go"
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
	RunCancelled       RunStatus = "cancelled"
)

// ReviewInfo carries a human-approval resolution in the event stream.
type ReviewInfo struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason,omitempty"`
	// Feedback carries the structured rejection payload (PRD §13).
	Feedback *Feedback `json:"feedback,omitempty"`
}

// RunEvent is the wire/UI event: node lifecycle + run-level + review metadata.
type RunEvent struct {
	Seq    int     `json:"seq"`
	RunID  guid.ID `json:"runId"`
	NodeID string  `json:"nodeId,omitempty"`
	Status Status  `json:"status"`
	// RunStatus is set only on run-level lifecycle events (RUN_STARTED/…).
	RunStatus RunStatus          `json:"runStatus,omitempty"`
	Output    json.RawMessage    `json:"output,omitempty"`
	Error     string             `json:"error,omitempty"`
	Attempt   int                `json:"attempt,omitempty"`
	Artifacts []ProducedArtifact `json:"artifacts,omitempty"`
	Consumes  []string           `json:"consumes,omitempty"`
	Review    *ReviewInfo        `json:"review,omitempty"`
	EventType string             `json:"eventType,omitempty"`
	Snapshot  *RunSnapshot       `json:"snapshot,omitempty"`
	Preview   *struct {
		Type    string `json:"type"`
		Content string `json:"content"`
	} `json:"preview,omitempty"`
}

// RunSnapshot makes RUN_STARTED self-contained so workflow_runs can be rebuilt
// from the append-only event log (PRD §15.1 World Reconstruction Test).
type RunSnapshot struct {
	Workflow     json.RawMessage `json:"workflow"`
	Context      json.RawMessage `json:"context"`
	Workspace    string          `json:"workspace"`
	RunnerHandle string          `json:"runnerHandle,omitempty"`
	CreatedAt    int64           `json:"createdAt"`
}

// RunSubscriber receives live events for one run (a WS connection's channel).
type RunSubscriber chan RunEvent

// Run is one in-memory workflow run (PRD workflow_runs table deferred).
type Run struct {
	ID        guid.ID
	TenantID  guid.ID
	EntityID  guid.ID
	Def       *WorkflowDef
	Broker    *ApprovalBroker
	ProjectID guid.ID
	OwnerID   guid.ID
	Workspace string
	created   time.Time

	mu         sync.Mutex
	status     RunStatus
	events     []RunEvent
	subs       map[RunSubscriber]struct{}
	seq        int
	cancel     context.CancelFunc
	done       chan struct{}
	req        LaunchRequest
	lease      RunLease
	persistErr error
}

func (r *Run) context(parent context.Context) context.Context {
	claims := &authn.Claims{TenantID: r.TenantID, EntityID: r.EntityID, PrincipalID: r.OwnerID}
	return authn.WithClaims(parent, claims)
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

func (r *Run) currentLease() RunLease {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lease
}

func (r *Run) setLease(lease RunLease) {
	r.mu.Lock()
	r.lease = lease
	r.persistErr = nil
	r.mu.Unlock()
}

func (r *Run) persistenceError() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.persistErr
}

func (r *Run) setPersistenceError(err error) context.CancelFunc {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.persistErr == nil {
		r.persistErr = err
	}
	return r.cancel
}

func (r *Run) nextSeq() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	return r.seq
}

// LaunchRequest is the POST /api/runs body. Workflow definitions cross the API
// boundary as validated JSON; the control plane never reads caller-selected
// local files.
type LaunchRequest struct {
	WorkflowJSON json.RawMessage `json:"workflowJSON"`
	Workspace    string          `json:"workspace"`
	Context      json.RawMessage `json:"context"`
	RunnerID     string          `json:"runnerId"`
	ProjectID    guid.ID         `json:"projectId"`
}

// RunManager owns live runs and the NATS fan-out to their WS subscribers.
type RunManager struct {
	nc       *nats.Conn
	link     RunnerLink
	st       *BunRepository
	runnerID string

	mu               sync.Mutex
	runs             map[guid.ID]*Run
	sub              *nats.Subscription
	baseFactory      func(runID guid.ID) Executor // test seam; nil -> RunnerExecutor
	picker           MachinePicker                // per-node machine selector (nil = use runnerID)
	// runnerScope, when set, reports the tenant/entity a runner id was onboarded
	// under so a user-supplied runnerId cannot dispatch a run onto another
	// tenant's machine (PRD P10).
	runnerScope func(runnerID string) (tenantID, entityID guid.ID, ok bool)
	processCtx  context.Context
	holderID         guid.ID
	nextID           func() (guid.ID, error)
	leaseTTL         time.Duration
	heartbeatTimeout time.Duration
}

func NewRunManager(nc *nats.Conn, link RunnerLink, st *BunRepository, runnerID string, holderID guid.ID, nextID func() (guid.ID, error)) *RunManager {
	if nc == nil || st == nil || nextID == nil {
		panic("workflow run manager requires NATS, repository, and id generator")
	}
	return &RunManager{nc: nc, link: link, st: st, runnerID: runnerID, runs: make(map[guid.ID]*Run),
		holderID: holderID, nextID: nextID, leaseTTL: 15 * time.Second,
		heartbeatTimeout: 45 * time.Second}
}

// SetMachinePicker configures per-node machine dispatch. When set, each agent
// node in a workflow run is dispatched to the best machine for its executor type.
func (m *RunManager) SetMachinePicker(p MachinePicker) {
	m.picker = p
}

// SetRunnerScope wires a tenant/entity resolver for runner ids so a
// user-supplied runnerId cannot target another tenant's machine.
func (m *RunManager) SetRunnerScope(fn func(runnerID string) (tenantID, entityID guid.ID, ok bool)) {
	m.runnerScope = fn
}

// Start subscribes chaos.run.*.evt and fans out each event to the matching
// run's subscribers. Safe to call once.
func (m *RunManager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.holderID.Zero() {
		holderID, err := m.nextID()
		if err != nil {
			m.mu.Unlock()
			return fmt.Errorf("generate workflow lease holder id: %w", err)
		}
		m.holderID = holderID
	}
	m.processCtx = ctx
	m.mu.Unlock()
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
	// Rehydrate non-terminal runs so in-flight approvals/pauses survive a
	// control-plane restart (crash recovery, PRD §15.1). Without this the live
	// run map is empty after boot and waiting approvals can never resolve.
	m.LoadFromStore(ctx)
	return nil
}

func (m *RunManager) Context() context.Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.processCtx != nil {
		return m.processCtx
	}
	return context.Background()
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

func (m *RunManager) ListFor(entityID guid.ID) []*Run {
	all := m.List()
	if entityID.Zero() {
		return all
	}
	out := make([]*Run, 0, len(all))
	for _, run := range all {
		if run.EntityID == entityID {
			out = append(out, run)
		}
	}
	return out
}

func (m *RunManager) Get(id guid.ID) (*Run, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[id]
	return r, ok
}

func (m *RunManager) GetFor(id, entityID guid.ID) (*Run, bool) {
	run, ok := m.Get(id)
	if !ok || (!entityID.Zero() && run.EntityID != entityID) {
		return nil, false
	}
	return run, true
}

func (m *RunManager) removeRun(id guid.ID) {
	m.mu.Lock()
	delete(m.runs, id)
	m.mu.Unlock()
}

func (m *RunManager) acquireLease(ctx context.Context, run *Run) error {
	if m.st == nil {
		return nil
	}
	lease, err := m.st.AcquireRunLease(run.context(ctx), run.ID, m.holderID, m.leaseTTL)
	if err != nil {
		return fmt.Errorf("acquire run %s single-writer lease: %w", run.ID, err)
	}
	run.setLease(lease)
	return nil
}

func (m *RunManager) renewLease(ctx context.Context, run *Run) {
	if m.st == nil {
		return
	}
	interval := m.leaseTTL / 3
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			lease, err := m.st.RenewRunLease(run.context(ctx), run.currentLease(), m.leaseTTL)
			if err != nil {
				m.stopOnPersistenceError(run, fmt.Errorf("renew run lease: %w", err))
				return
			}
			run.mu.Lock()
			if run.lease.FencingToken == lease.FencingToken && run.lease.HolderID == lease.HolderID {
				run.lease = lease
			}
			run.mu.Unlock()
		}
	}
}

func (m *RunManager) releaseLease(run *Run) {
	if m.st == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := m.st.ReleaseRunLease(run.context(ctx), run.currentLease()); err != nil {
		slog.Warn("release run lease", "run", run.ID, "err", err)
	}
}

func (m *RunManager) stopOnPersistenceError(run *Run, err error) {
	slog.Error("run persistence fenced or failed", "run", run.ID, "err", err)
	if cancel := run.setPersistenceError(err); cancel != nil {
		cancel()
	}
}

func (m *RunManager) newRun(ctx context.Context, def *WorkflowDef) (*Run, error) {
	claims, err := requireClaims(ctx)
	if err != nil {
		return nil, err
	}
	id, err := m.nextID()
	if err != nil {
		return nil, fmt.Errorf("generate workflow run id: %w", err)
	}
	m.mu.Lock()
	run := &Run{
		ID: id, TenantID: claims.TenantID, EntityID: claims.EntityID, OwnerID: claims.PrincipalID,
		Def:     def,
		status:  RunRunning,
		Broker:  NewApprovalBroker(),
		subs:    make(map[RunSubscriber]struct{}),
		created: time.Now().UTC(),
	}
	m.runs[id] = run
	m.mu.Unlock()
	return run, nil
}

// Launch loads + validates a workflow, then runs it on a goroutine against the
// per-run broker-backed executor. Returns immediately; progress arrives via
// Events / WS.
func (m *RunManager) Launch(ctx context.Context, req LaunchRequest) (*Run, error) {
	var raw []byte
	if len(req.WorkflowJSON) == 0 {
		return nil, fmt.Errorf("workflowJSON is required")
	}
	raw = req.WorkflowJSON
	var def WorkflowDef
	if err := json.Unmarshal(raw, &def); err != nil {
		return nil, fmt.Errorf("parse workflow: %w", err)
	}
	if err := def.Validate(); err != nil {
		return nil, fmt.Errorf("validate workflow: %w", err)
	}
	if len(req.Context) == 0 {
		req.Context = json.RawMessage(`{}`)
	}
	if err := def.ValidateContext(req.Context); err != nil {
		return nil, fmt.Errorf("validate workflow context: %w", err)
	}
	if req.ProjectID.Zero() {
		return nil, fmt.Errorf("projectId is required")
	}
	run, err := m.newRun(ctx, &def)
	if err != nil {
		return nil, err
	}
	if req.Workspace == "" {
		m.removeRun(run.ID)
		return nil, fmt.Errorf("workspace is required")
	}
	run.ProjectID = req.ProjectID
	run.Workspace = req.Workspace
	run.req = req
	defJSON, err := json.Marshal(run.Def)
	if err != nil {
		m.removeRun(run.ID)
		return nil, fmt.Errorf("marshal workflow run definition: %w", err)
	}
	runDef := RunDef{
		ID: run.ID, TenantID: run.TenantID, EntityID: run.EntityID, ProjectID: run.ProjectID, OwnerID: run.OwnerID,
		DefJSON: string(defJSON), Status: RunRunning, ContextJSON: string(req.Context), Workspace: run.Workspace,
		RunnerHandle: req.RunnerID, CreatedAt: run.created.UnixMilli(), CreatedBy: run.OwnerID,
	}
	if err := m.st.SaveRunDefinition(run.context(ctx), runDef); err != nil {
		m.removeRun(run.ID)
		return nil, fmt.Errorf("persist workflow run before activation: %w", err)
	}
	if err := m.acquireLease(ctx, run); err != nil {
		if statusErr := m.st.UpdateRunStatus(run.context(ctx), run.ID, RunFailed); statusErr != nil {
			err = errors.Join(err, fmt.Errorf("mark workflow run failed after lease error: %w", statusErr))
		}
		m.removeRun(run.ID)
		return nil, err
	}
	// The engine must outlive the HTTP request: binding it to the request
	// context cancels every node spawn the moment the launch response returns
	// ("context canceled"). The recovery path already uses m.Context(); launch
	// must do the same so runs survive after the request completes.
	if err := m.startEngine(m.Context(), run, req, nil); err != nil {
		if statusErr := m.st.UpdateRunStatus(run.context(ctx), run.ID, RunFailed); statusErr != nil {
			err = errors.Join(err, fmt.Errorf("mark workflow run failed after activation error: %w", statusErr))
		}
		m.releaseLease(run)
		m.removeRun(run.ID)
		return nil, err
	}
	return run, nil
}

func (m *RunManager) startEngine(ctx context.Context, run *Run, req LaunchRequest, restored []Event) error {
	runCtx, cancelCause := context.WithCancelCause(ctx)
	cancel := func() { cancelCause(ErrRunInterrupted) }
	done := make(chan struct{})
	run.mu.Lock()
	run.cancel = cancel
	run.done = done
	run.mu.Unlock()
	broker := run.Broker
	broker.OnDecision = func(nodeID string, d Decision) error {
		if err := m.emit(run, RunEvent{
			NodeID: nodeID, Status: StatusCompleted,
			Review: &ReviewInfo{Approved: d.OK, Reason: d.Reason, Feedback: d.Feedback},
		}); err != nil {
			return err
		}
		run.setStatus(RunRunning)
		return nil
	}

	var base Executor
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
				return fmt.Errorf("no runner registered; set runnerId or start a daemon")
			}
			runnerID = reg[0]
		}
		// Never dispatch onto a machine another tenant owns (PRD P10): a caller
		// may supply runnerId explicitly, but only the run's own tenant/entity
		// daemons are valid targets.
		if m.runnerScope != nil {
			if tenantID, entityID, ok := m.runnerScope(runnerID); ok && (tenantID != run.TenantID || entityID != run.EntityID) {
				cancel()
				return fmt.Errorf("runner %s is not owned by this run's tenant/entity", runnerID)
			}
		}
		base = NewRunnerExecutor(m.link, runnerID, req.Workspace, run.ID.String()).
			WithMachinePicker(m.picker).
			WithAttemptOffsets(attemptOffsets(run.Events())).
			WithHeartbeatTimeout(m.heartbeatTimeout)
	}
	exec := NewApprovalExecutor(base, broker)

	eng, err := NewEngine(run.Def, exec)
	if err != nil {
		cancel()
		return err
	}
	if len(restored) > 0 {
		if err := eng.Restore(restored); err != nil {
			cancel()
			return err
		}
	}
	defJSON, _ := json.Marshal(run.Def)
	eventType := "RUN_STARTED"
	var snapshot *RunSnapshot
	if len(restored) > 0 {
		eventType = "RUN_RESUMED"
	} else {
		snapshot = &RunSnapshot{Workflow: defJSON, Context: req.Context, Workspace: req.Workspace,
			RunnerHandle: req.RunnerID, CreatedAt: run.created.UnixMilli()}
	}
	if err := m.emit(run, RunEvent{RunStatus: RunRunning, EventType: eventType, Snapshot: snapshot}); err != nil {
		cancel()
		return err
	}
	go m.renewLease(runCtx, run)
	go func() {
		defer cancelCause(nil)
		defer close(done)
		eng.OnEvent = func(ev Event) {
			consumes := make([]string, 0)
			if node := nodeByID(run.Def, ev.NodeID); node != nil && node.Agent != nil && node.Agent.InputSpec != nil {
				for _, consume := range node.Agent.InputSpec.Consumes {
					consumes = append(consumes, consume.ID)
				}
			}
			if err := m.emit(run, RunEvent{
				Seq: ev.Seq, NodeID: ev.NodeID,
				Status: ev.Status, Output: ev.Output, Error: ev.Error,
				Attempt: ev.Attempt, Artifacts: ev.Artifacts, Consumes: consumes, Preview: ev.Preview,
			}); err != nil {
				m.stopOnPersistenceError(run, err)
				return
			}
			if ev.Status == StatusWaitingApproval {
				run.setStatus(RunWaitingApproval)
			}
			if ev.Status == StatusPausedForHuman {
				run.setStatus(RunPaused)
			}
		}
		_, err := eng.Run(runCtx, req.Context)
		persistErr := run.persistenceError()
		if persistErr != nil {
			err = persistErr
		}
		final := run.Status()
		if persistErr != nil {
			final = RunFailed
		} else if final != RunPaused && final != RunCancelled {
			final = m.finalStatus(run, err)
		}
		// Run-level terminal event (F.2 RUN_COMPLETED/FAILED/PAUSED).
		if emitErr := m.emit(run, RunEvent{RunStatus: final}); emitErr != nil {
			slog.Error("persist terminal run event", "run", run.ID, "status", final, "err", emitErr)
			if final != RunCancelled {
				final = RunFailed
			}
		}
		run.setStatus(final)
		cancelCause(nil)
		m.releaseLease(run)
	}()
	return nil
}

func attemptOffsets(events []RunEvent) map[string]int {
	offsets := make(map[string]int)
	for _, event := range events {
		if event.NodeID == "" {
			continue
		}
		attempt := event.Attempt + 1
		if attempt > offsets[event.NodeID] {
			offsets[event.NodeID] = attempt
		}
	}
	return offsets
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
	last := make(map[string]Status)
	for _, ev := range run.Events() {
		if ev.NodeID != "" {
			last[ev.NodeID] = ev.Status
		}
	}
	for _, s := range last {
		if s == StatusFailed {
			return RunFailed
		}
	}
	return RunCompleted
}

// nodeCountFromSnapshot counts the nodes in a persisted def snapshot (best
// effort; 0 on a malformed snapshot).
func nodeCountFromSnapshot(snap string) int {
	var def WorkflowDef
	if err := json.Unmarshal([]byte(snap), &def); err != nil {
		return 0
	}
	return len(def.Nodes)
}

func nodeByID(def *WorkflowDef, id string) *Node {
	for i := range def.Nodes {
		if def.Nodes[i].ID == id {
			return &def.Nodes[i]
		}
	}
	return nil
}

// Approve resolves a human-approval gate. 404/409 handled by the HTTP layer.
func (m *RunManager) Approve(runID guid.ID, nodeID string, ok bool, reason string, fb *Feedback) error {
	run, found := m.Get(runID)
	if !found {
		return fmt.Errorf("run %s not found", runID)
	}
	return run.Broker.Resolve(nodeID, ok, reason, fb)
}

func (m *RunManager) Pause(runID guid.ID) error {
	run, ok := m.Get(runID)
	if !ok {
		return fmt.Errorf("run %s not found", runID)
	}
	status := run.Status()
	if status != RunRunning && status != RunWaitingApproval {
		return fmt.Errorf("run %s cannot pause from %s", runID, status)
	}
	run.setStatus(RunPaused)
	run.mu.Lock()
	cancel := run.cancel
	run.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (m *RunManager) Cancel(runID guid.ID) error {
	run, ok := m.Get(runID)
	if !ok {
		return fmt.Errorf("run %s not found", runID)
	}
	status := run.Status()
	if status == RunCompleted || status == RunFailed || status == RunCancelled {
		return fmt.Errorf("run %s cannot cancel from %s", runID, status)
	}
	run.setStatus(RunCancelled)
	run.mu.Lock()
	cancel := run.cancel
	run.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (m *RunManager) Resume(runID guid.ID) error {
	run, ok := m.Get(runID)
	if !ok {
		return fmt.Errorf("run %s not found", runID)
	}
	if run.Status() != RunPaused {
		return fmt.Errorf("run %s cannot resume from %s", runID, run.Status())
	}
	run.mu.Lock()
	done := run.done
	run.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		default:
			return fmt.Errorf("run %s is still pausing", runID)
		}
	}
	restored := make([]Event, 0)
	for _, event := range run.Events() {
		if event.NodeID != "" && event.Review == nil {
			restored = append(restored, Event{NodeID: event.NodeID, Status: event.Status,
				Output: event.Output, Error: event.Error, Attempt: event.Attempt, Artifacts: event.Artifacts})
		}
	}
	run.Broker = NewApprovalBroker()
	if err := m.acquireLease(m.Context(), run); err != nil {
		return err
	}
	run.setStatus(RunRunning)
	if err := m.startEngine(m.Context(), run, run.req, restored); err != nil {
		m.releaseLease(run)
		return err
	}
	return nil
}

// emit commits the authoritative event and every rebuildable projection in one
// fenced transaction before it becomes visible in memory or over NATS.
func (m *RunManager) emit(run *Run, ev RunEvent) error {
	ev.Seq = run.nextSeq()
	ev.RunID = run.ID
	if m.st != nil {
		typ := storeTypeFor(ev)
		payload, err := json.Marshal(ev)
		if err != nil {
			return fmt.Errorf("marshal run event: %w", err)
		}
		eventID, err := m.nextID()
		if err != nil {
			return fmt.Errorf("generate workflow event id: %w", err)
		}
		events := []EventRecord{{
			ID:             eventID,
			TenantID:       run.TenantID,
			EntityID:       run.EntityID,
			ProjectID:      run.ProjectID,
			ActorID:        run.OwnerID,
			RunID:          run.ID,
			Type:           typ,
			IdempotencyKey: fmt.Sprintf("%s:%s:%d", run.ID, typ, ev.Seq),
			PayloadJSON:    string(payload),
		}}
		if err := m.st.CommitRunEvents(run.context(context.Background()), run.currentLease(), events); err != nil {
			return fmt.Errorf("commit run event %s/%d: %w", typ, ev.Seq, err)
		}
	}
	run.publish(ev)
	if m.nc != nil {
		data, err := json.Marshal(ev)
		if err != nil {
			return fmt.Errorf("marshal NATS run event: %w", err)
		}
		if err := m.nc.Publish(runSubjectPrefix+run.ID.String()+".evt", data); err != nil {
			slog.Warn("publish run event", "run", run.ID, "seq", ev.Seq, "err", err)
		}
	}
	return nil
}

func storeTypeFor(ev RunEvent) string {
	if ev.EventType != "" {
		return ev.EventType
	}
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
		case RunCancelled:
			return "RUN_CANCELLED"
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
	case ev.Status == StatusWaitingApproval:
		return "REVIEW_REQUESTED"
	case ev.Status == StatusRetrying:
		// F.2: a failed attempt with a retry scheduled is NODE_RETRY_SCHEDULED,
		// not a terminal NODE_FAILED — consumers must not treat it as failure.
		return "NODE_RETRY_SCHEDULED"
	case ev.Status == StatusPausedForHuman:
		return "NODE_PAUSED_FOR_HUMAN"
	default:
		switch ev.Status {
		case StatusRunning:
			return "NODE_STARTED"
		case StatusCompleted:
			return "NODE_COMPLETED"
		case StatusFailed:
			return "NODE_FAILED"
		case StatusSkipped:
			return "NODE_SKIPPED"
		default:
			return "NODE_EVENT"
		}
	}
}
