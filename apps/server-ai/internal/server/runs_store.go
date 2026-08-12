package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/workflow"
)

// LoadFromStore rehydrates and resumes non-terminal runs from the authoritative
// event stream. Completed nodes are not repeated; interrupted nodes continue
// with a new attempt and waiting approvals become live broker waits again.
func (m *RunManager) LoadFromStore(ctx context.Context) {
	if m.st == nil {
		return
	}
	active, err := m.st.LoadActiveRunDefinitions(ctx)
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
		var def workflow.WorkflowDef
		if err := json.Unmarshal([]byte(rd.DefJSON), &def); err != nil {
			slog.Warn("skip corrupt run def", "run", rd.ID, "err", err)
			continue
		}
		run := &Run{
			ID: rd.ID, Def: &def, status: RunStatus(rd.Status),
			Broker: workflow.NewApprovalBroker(), subs: make(map[RunSubscriber]struct{}),
			InstanceID: rd.InstanceID, ProjectID: rd.ProjectID, Workspace: rd.Workspace,
			created: time.UnixMilli(rd.CreatedAt),
		}
		run.req = LaunchRequest{WorkflowJSON: json.RawMessage(rd.DefJSON), Context: json.RawMessage(rd.ContextJSON),
			Workspace: rd.Workspace, RunnerID: rd.RunnerID, InstanceID: rd.InstanceID, ProjectID: rd.ProjectID}
		// Preserve the original per-run event seqs (they feed idempotency keys):
		// rewriting them would collide with pre-restart events and silently drop
		// new ones (review: >1000-event runs truncated). Load well beyond any
		// realistic run size rather than a fixed 1000 cap.
		events, _ := m.st.ListEvents(ctx, rd.ID, 0, 100000)
		maxSeq := 0
		restored := make([]workflow.Event, 0, len(events))
		for _, evt := range events {
			var re RunEvent
			if json.Unmarshal([]byte(evt.PayloadJSON), &re) == nil {
				if re.Seq > maxSeq {
					maxSeq = re.Seq
				}
				run.events = append(run.events, re)
				if re.NodeID != "" {
					output := re.Output
					if re.Review != nil {
						output, _ = json.Marshal(map[string]bool{"approved": re.Review.Approved})
					}
					restored = append(restored, workflow.Event{
						NodeID: re.NodeID, Status: re.Status, Output: output, Error: re.Error,
						Attempt: re.Attempt, Artifacts: re.Artifacts,
					})
				}
			}
		}
		run.seq = maxSeq
		m.mu.Lock()
		m.runs[rd.ID] = run
		m.mu.Unlock()
		slog.Info("rehydrated run", "run", rd.ID, "status", rd.Status, "events", len(run.events))
		if rd.Status == string(RunPaused) {
			continue
		}
		go m.resumeStoredRun(ctx, run, run.req, restored)
	}
}

func (m *RunManager) resumeStoredRun(ctx context.Context, run *Run, req LaunchRequest, restored []workflow.Event) {
	for {
		err := m.startEngine(ctx, run, req, restored)
		if err == nil {
			return
		}
		if err.Error() != "no runner registered; set runnerId or start a daemon" {
			slog.Error("resume stored run", "run", run.ID, "err", err)
			run.setStatus(RunFailed)
			_ = m.st.UpdateRunStatus(context.Background(), run.ID, string(RunFailed))
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}
