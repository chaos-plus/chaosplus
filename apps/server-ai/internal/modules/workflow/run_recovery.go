package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"
)

// LoadFromStore rehydrates and resumes non-terminal runs from the authoritative
// event stream. Completed nodes are not repeated; interrupted nodes continue
// with a new attempt and waiting approvals become live broker waits again.
func (m *RunManager) LoadFromStore(ctx context.Context) {
	if m.st == nil {
		return
	}
	active, err := m.st.LoadAllActiveRunDefinitions(ctx)
	if err != nil {
		slog.Warn("load active runs", "err", err)
		return
	}
	for _, rd := range active {
		m.mu.Lock()
		if _, exists := m.runs[rd.ID]; exists {
			m.mu.Unlock()
			continue
		}
		m.mu.Unlock()
		var def WorkflowDef
		if err := json.Unmarshal([]byte(rd.DefJSON), &def); err != nil {
			slog.Warn("skip corrupt run def", "run", rd.ID, "err", err)
			continue
		}
		run := &Run{
			ID: rd.ID, TenantID: rd.TenantID, EntityID: rd.EntityID, OwnerID: rd.OwnerID, Def: &def, status: rd.Status,
			Broker: NewApprovalBroker(), subs: make(map[RunSubscriber]struct{}),
			ProjectID: rd.ProjectID, Workspace: rd.Workspace,
			created: time.UnixMilli(rd.CreatedAt).UTC(),
		}
		run.req = LaunchRequest{WorkflowJSON: json.RawMessage(rd.DefJSON), Context: json.RawMessage(rd.ContextJSON),
			Workspace: rd.Workspace, RunnerID: rd.RunnerHandle, ProjectID: rd.ProjectID}
		// Preserve the original per-run event seqs (they feed idempotency keys):
		// rewriting them would collide with pre-restart events and silently drop
		// new ones (review: >1000-event runs truncated). Load well beyond any
		// realistic run size rather than a fixed 1000 cap.
		events, _ := m.st.ListAllEvents(ctx, rd.ID, 0, 100000)
		maxSeq := 0
		restored := make([]Event, 0, len(events))
		for _, evt := range events {
			var re RunEvent
			if json.Unmarshal([]byte(evt.PayloadJSON), &re) == nil && re.RunID == rd.ID && re.Seq > 0 {
				if re.Seq > maxSeq {
					maxSeq = re.Seq
				}
				run.events = append(run.events, re)
				if re.NodeID != "" {
					output := re.Output
					if re.Review != nil {
						output, _ = json.Marshal(map[string]bool{"approved": re.Review.Approved})
					}
					restored = append(restored, Event{
						NodeID: re.NodeID, Status: re.Status, Output: output, Error: re.Error,
						Attempt: re.Attempt, Artifacts: re.Artifacts,
					})
				}
			}
		}
		run.seq = maxSeq
		if err := m.acquireLease(ctx, run); err != nil {
			if errors.Is(err, ErrLeaseHeld) {
				slog.Info("active run is owned by another control-plane instance", "run", rd.ID)
				m.startBackground(func() { m.waitForStoredLease(ctx, run, rd.Status, restored) })
				continue
			}
			slog.Warn("skip active run without lease", "run", rd.ID, "err", err)
			continue
		}
		m.activateStoredRun(ctx, run, rd.Status, restored)
	}
}

func (m *RunManager) waitForStoredLease(ctx context.Context, run *Run, status RunStatus, restored []Event) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.acquireLease(ctx, run); err != nil {
				if !errors.Is(err, ErrLeaseHeld) {
					slog.Warn("retry active run lease", "run", run.ID, "err", err)
				}
				continue
			}
			freshStatus, freshRestored, terminal, err := m.refreshStoredRun(ctx, run)
			if err != nil {
				slog.Warn("refresh run after lease takeover", "run", run.ID, "err", err)
				m.releaseLease(run)
				continue
			}
			if terminal {
				m.releaseLease(run)
				return
			}
			m.activateStoredRun(ctx, run, freshStatus, freshRestored)
			return
		}
	}
}

func (m *RunManager) refreshStoredRun(ctx context.Context, run *Run) (RunStatus, []Event, bool, error) {
	// The lease-takeover path runs with the process context (no request
	// principal); scope the read to the run's own tenant/entity so the
	// repository's claim guard passes without leaking cross-tenant data.
	runCtx := run.context(ctx)
	rd, err := m.st.GetRunDef(runCtx, run.ID)
	if err != nil {
		return "", nil, false, err
	}
	switch rd.Status {
	case RunCompleted, RunFailed, RunCancelled:
		return rd.Status, nil, true, nil
	}
	events, err := m.st.ListEvents(runCtx, rd.ID, 0, 100000)
	if err != nil {
		return "", nil, false, err
	}
	run.mu.Lock()
	run.events = run.events[:0]
	run.seq = 0
	restored := make([]Event, 0, len(events))
	for _, evt := range events {
		var re RunEvent
		if json.Unmarshal([]byte(evt.PayloadJSON), &re) != nil || re.RunID != rd.ID || re.Seq <= 0 {
			continue
		}
		if re.Seq > run.seq {
			run.seq = re.Seq
		}
		run.events = append(run.events, re)
		if re.NodeID != "" {
			output := re.Output
			if re.Review != nil {
				output, _ = json.Marshal(map[string]bool{"approved": re.Review.Approved})
			}
			restored = append(restored, Event{NodeID: re.NodeID, Status: re.Status,
				Output: output, Error: re.Error, Attempt: re.Attempt, Artifacts: re.Artifacts})
		}
	}
	run.status = rd.Status
	run.mu.Unlock()
	return rd.Status, restored, false, nil
}

func (m *RunManager) activateStoredRun(ctx context.Context, run *Run, status RunStatus, restored []Event) {
	m.mu.Lock()
	if _, exists := m.runs[run.ID]; exists {
		m.mu.Unlock()
		m.releaseLease(run)
		return
	}
	m.runs[run.ID] = run
	m.mu.Unlock()
	slog.Info("rehydrated run", "run", run.ID, "status", status, "events", len(run.events))
	if status == RunPaused {
		m.releaseLease(run)
		return
	}
	if !m.startBackground(func() { m.resumeStoredRun(ctx, run, run.req, restored) }) {
		m.releaseLease(run)
	}
}

func (m *RunManager) resumeStoredRun(ctx context.Context, run *Run, req LaunchRequest, restored []Event) {
	for {
		err := m.startEngine(ctx, run, req, restored)
		if err == nil {
			return
		}
		if err.Error() != "no runner registered; set runnerId or start a daemon" {
			slog.Error("resume stored run", "run", run.ID, "err", err)
			run.setStatus(RunFailed)
			if emitErr := m.emit(run, RunEvent{RunStatus: RunFailed}); emitErr != nil {
				slog.Error("persist failed resumed run", "run", run.ID, "err", emitErr)
			}
			m.releaseLease(run)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}
