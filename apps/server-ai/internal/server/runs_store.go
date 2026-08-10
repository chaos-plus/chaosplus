package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/workflow"
)

// LoadFromStore rehydrates non-terminal runs from the store on boot (PRD §15.1).
// Goroutines are NOT resumed (v1 limitation) — in-flight runs at shutdown show
// their last persisted state; the user can re-launch.
func (m *RunManager) LoadFromStore(ctx context.Context) {
	if m.st == nil {
		return
	}
	active, err := m.st.LoadActiveRunDefinitions(ctx)
	if err != nil {
		slog.Warn("load active runs", "err", err)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, rd := range active {
		if _, exists := m.runs[rd.ID]; exists {
			continue
		}
		var def workflow.WorkflowDef
		if err := json.Unmarshal([]byte(rd.DefJSON), &def); err != nil {
			slog.Warn("skip corrupt run def", "run", rd.ID, "err", err)
			continue
		}
		run := &Run{
			ID:      rd.ID,
			Def:     &def,
			status:  RunStatus(rd.Status),
			Broker:  workflow.NewApprovalBroker(),
			subs:    make(map[RunSubscriber]struct{}),
			created: time.UnixMilli(rd.CreatedAt),
		}
		// Preserve the original per-run event seqs (they feed idempotency keys):
		// rewriting them would collide with pre-restart events and silently drop
		// new ones (review: >1000-event runs truncated). Load well beyond any
		// realistic run size rather than a fixed 1000 cap.
		events, _ := m.st.ListEvents(ctx, rd.ID, 0, 100000)
		maxSeq := 0
		for _, evt := range events {
			var re RunEvent
			if json.Unmarshal([]byte(evt.PayloadJSON), &re) == nil {
				if re.Seq > maxSeq {
					maxSeq = re.Seq
				}
				run.events = append(run.events, re)
			}
		}
		run.seq = maxSeq
		m.runs[rd.ID] = run
		slog.Info("rehydrated run", "run", rd.ID, "status", rd.Status, "events", len(run.events))
	}
}
