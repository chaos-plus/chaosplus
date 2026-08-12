package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

// RebuildCoreProjections rebuilds only workflow-owned run and node projections.
// Other bounded contexts rebuild their projections through their own modules.
func (s *BunRepository) RebuildCoreProjections(ctx context.Context) error {
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		events := []EventRecord{}
		if err := tx.NewSelect().Model(&events).Order("seq ASC").Scan(ctx); err != nil {
			return fmt.Errorf("load events for projection rebuild: %w", err)
		}
		for _, table := range []string{"node_executions", "workflow_runs"} {
			if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
				return fmt.Errorf("clear projection %s: %w", table, err)
			}
		}
		for _, event := range events {
			if err := replayCoreEvent(ctx, tx, event); err != nil {
				return fmt.Errorf("replay event %s (%s): %w", event.ID, event.Type, err)
			}
		}
		return nil
	})
}

type replayRunEvent struct {
	RunID     guid.ID         `json:"runId"`
	NodeID    string          `json:"nodeId"`
	Status    string          `json:"status"`
	RunStatus string          `json:"runStatus"`
	Error     string          `json:"error"`
	Attempt   int             `json:"attempt"`
	Snapshot  *replaySnapshot `json:"snapshot"`
}

type replaySnapshot struct {
	Workflow     json.RawMessage `json:"workflow"`
	Context      json.RawMessage `json:"context"`
	Workspace    string          `json:"workspace"`
	RunnerHandle string          `json:"runnerHandle"`
	CreatedAt    int64           `json:"createdAt"`
}

func replayCoreEvent(ctx context.Context, tx bun.Tx, event EventRecord) error {
	var runEvent replayRunEvent
	_ = json.Unmarshal([]byte(event.PayloadJSON), &runEvent)
	switch event.Type {
	case "RUN_STARTED":
		if runEvent.Snapshot == nil || len(runEvent.Snapshot.Workflow) == 0 {
			return fmt.Errorf("RUN_STARTED is missing snapshot")
		}
		createdAt := runEvent.Snapshot.CreatedAt
		if createdAt == 0 {
			createdAt = event.TS
		}
		run := RunDef{ID: event.RunID, TenantID: event.TenantID, EntityID: event.EntityID, ProjectID: event.ProjectID, OwnerID: event.ActorID,
			DefJSON: string(runEvent.Snapshot.Workflow), Status: RunRunning,
			CreatedAt: createdAt, UpdatedAt: event.TS, ContextJSON: string(runEvent.Snapshot.Context),
			Workspace: runEvent.Snapshot.Workspace, RunnerHandle: runEvent.Snapshot.RunnerHandle,
			CreatedBy: event.ActorID, UpdatedBy: event.ActorID, Version: 1}
		if run.ContextJSON == "" {
			run.ContextJSON = "{}"
		}
		_, err := tx.NewInsert().Model(&run).On("CONFLICT (id) DO UPDATE").
			Set("def_json = EXCLUDED.def_json").Set("status = EXCLUDED.status").Set("context_json = EXCLUDED.context_json").
			Set("workspace = EXCLUDED.workspace").Set("runner_handle = EXCLUDED.runner_handle").
			Set("entity_id = EXCLUDED.entity_id").Set("project_id = EXCLUDED.project_id").Set("updated_at = EXCLUDED.updated_at").Exec(ctx)
		return err
	case "RUN_RESUMED":
		return updateRunProjection(ctx, tx, event, RunRunning)
	case "RUN_COMPLETED":
		return updateRunProjection(ctx, tx, event, RunCompleted)
	case "RUN_FAILED":
		return updateRunProjection(ctx, tx, event, RunFailed)
	case "RUN_PAUSED":
		status := RunPaused
		if runEvent.RunStatus == "waiting_approval" {
			status = RunWaitingApproval
		}
		return updateRunProjection(ctx, tx, event, status)
	case "RUN_CANCELLED":
		return updateRunProjection(ctx, tx, event, RunCancelled)
	case "REVIEW_APPROVED", "REVIEW_REJECTED":
		return updateRunProjection(ctx, tx, event, RunRunning)
	}
	if runEvent.NodeID == "" || runEvent.Status == "" {
		return nil
	}
	completedAt := int64(0)
	if runEvent.Status == "completed" || runEvent.Status == "failed" || runEvent.Status == "skipped" {
		completedAt = event.TS
	}
	node := NodeExecution{TenantID: event.TenantID, EntityID: event.EntityID, RunID: event.RunID, NodeKey: runEvent.NodeID, Attempt: runEvent.Attempt + 1,
		Status: runEvent.Status, StartedAt: event.TS, CompletedAt: completedAt, Error: runEvent.Error}
	_, err := tx.NewInsert().Model(&node).On("CONFLICT (tenant_id, entity_id, run_id, node_key, attempt) DO UPDATE SET status = excluded.status, completed_at = excluded.completed_at, error = excluded.error").Exec(ctx)
	if err != nil {
		return err
	}
	if event.Type == "REVIEW_REQUESTED" {
		return updateRunProjection(ctx, tx, event, RunWaitingApproval)
	}
	return nil
}

func updateRunProjection(ctx context.Context, tx bun.Tx, event EventRecord, status RunStatus) error {
	_, err := tx.NewUpdate().Model((*RunDef)(nil)).Set("status = ?", status).Set("updated_at = ?", event.TS).
		Set("updated_by = ?", event.ActorID).Set("version = version + 1").
		Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0", event.RunID, event.TenantID, event.EntityID).Exec(ctx)
	return err
}

// RebuildCoreProjectionsIfEmpty recovers after projection rows were removed.
// Normal startups with an existing workflow_runs projection remain untouched.
func (s *BunRepository) RebuildCoreProjectionsIfEmpty(ctx context.Context) error {
	count, err := s.db.NewSelect().Model((*RunDef)(nil)).Count(ctx)
	if err != nil || count > 0 {
		return err
	}
	eventCount, err := s.db.NewSelect().Model((*EventRecord)(nil)).Where("type = ?", "RUN_STARTED").Count(ctx)
	if err != nil || eventCount == 0 {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return s.RebuildCoreProjections(ctx)
}
