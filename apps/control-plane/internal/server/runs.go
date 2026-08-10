// Package server is the control-plane's HTTP + WebSocket + run-orchestration
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
	"sync"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/store"
	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/workflow"
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
	Output json.RawMessage `json:"output,omitempty"`
	Error  string          `json:"error,omitempty"`
	Review *ReviewInfo     `json:"review,omitempty"`
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
}

func NewRunManager(nc *nats.Conn, link workflow.RunnerLink, st *store.Store, runnerID string) *RunManager {
	return &RunManager{nc: nc, link: link, st: st, runnerID: runnerID, runs: make(map[string]*Run)}
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
		if r != nil {
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
	if req.Workspace == "" {
		return nil, fmt.Errorf("workspace is required")
	}

	run := m.newRun(&def)
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
				return nil, fmt.Errorf("no runner registered; set runnerId or start a daemon")
			}
			runnerID = reg[0]
		}
		base = workflow.NewRunnerExecutor(m.link, runnerID, req.Workspace, run.ID)
	}
	exec := workflow.NewApprovalExecutor(base, broker)

	eng, err := workflow.NewEngine(&def, exec)
	if err != nil {
		cancel()
		return nil, err
	}
	m.emit(run, RunEvent{Status: workflow.StatusPending})
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
			})
		}
		_, err := eng.Run(runCtx, req.Context)
		final := m.finalStatus(run, err)
		run.setStatus(final)
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
	for _, ev := range run.Events() {
		if ev.Status == workflow.StatusFailed {
			return RunFailed
		}
	}
	return RunCompleted
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

// emit publishes an event to NATS (cluster fan-out), persists it to the
// StateStore when configured, and delivers locally to the run's subscribers.
func (m *RunManager) emit(run *Run, ev RunEvent) {
	ev.Seq = run.nextSeq()
	ev.RunID = run.ID
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
	run.publish(ev) // local delivery; the NATS round-trip also lands async
}

func storeTypeFor(ev RunEvent) string {
	switch {
	case ev.NodeID == "":
		if ev.Status == workflow.StatusFailed {
			return "RUN_FAILED"
		}
		return "RUN_EVENT"
	case ev.Review != nil:
		if ev.Review.Approved {
			return "REVIEW_APPROVED"
		}
		return "REVIEW_REJECTED"
	case ev.Status == workflow.StatusWaitingApproval:
		return "REVIEW_REQUESTED"
	default:
		return "NODE_" + string(ev.Status)
	}
}
