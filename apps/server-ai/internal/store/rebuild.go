package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

// RebuildCoreProjections is the World Reconstruction Test implementation for
// the execution core. It discards only rebuildable run/node/artifact/review
// projections and replays the append-only events table in one transaction.
func (s *Store) RebuildCoreProjections(ctx context.Context) error {
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		events := []Event{}
		if err := tx.NewSelect().Model(&events).Order("seq ASC").Scan(ctx); err != nil {
			return fmt.Errorf("load events for projection rebuild: %w", err)
		}
		for _, table := range []string{"feedback_log", "validation_results", "artifact_deps", "artifacts", "node_executions", "workflow_runs"} {
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
	RunID     string          `json:"runId"`
	NodeID    string          `json:"nodeId"`
	Status    string          `json:"status"`
	RunStatus string          `json:"runStatus"`
	Error     string          `json:"error"`
	Attempt   int             `json:"attempt"`
	Review    *replayReview   `json:"review"`
	Snapshot  *replaySnapshot `json:"snapshot"`
}

type replaySnapshot struct {
	Workflow   json.RawMessage `json:"workflow"`
	Context    json.RawMessage `json:"context"`
	Workspace  string          `json:"workspace"`
	RunnerID   string          `json:"runnerId"`
	InstanceID string          `json:"instanceId"`
	ProjectID  string          `json:"projectId"`
	CreatedAt  int64           `json:"createdAt"`
}

type replayReview struct {
	Approved bool            `json:"approved"`
	Reason   string          `json:"reason"`
	Feedback *replayFeedback `json:"feedback"`
}

type replayFeedback struct {
	Category string `json:"category"`
	Location string `json:"location"`
	Expected string `json:"expected"`
	Detail   string `json:"detail"`
}

type replayArtifactEvent struct {
	Artifact     Artifact      `json:"artifact"`
	Dependencies []ArtifactDep `json:"dependencies"`
	Reviewer     string        `json:"reviewer,omitempty"`
}

func replayCoreEvent(ctx context.Context, tx bun.Tx, event Event) error {
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
		run := RunDef{ID: event.RunID, DefJSON: string(runEvent.Snapshot.Workflow), Status: "running",
			CreatedAt: createdAt, UpdatedAt: event.TS, ContextJSON: string(runEvent.Snapshot.Context),
			Workspace: runEvent.Snapshot.Workspace, RunnerID: runEvent.Snapshot.RunnerID,
			InstanceID: runEvent.Snapshot.InstanceID, ProjectID: runEvent.Snapshot.ProjectID}
		if run.ContextJSON == "" {
			run.ContextJSON = "{}"
		}
		_, err := tx.NewInsert().Model(&run).On("CONFLICT (id) DO UPDATE").
			Set("def_json = EXCLUDED.def_json").Set("status = EXCLUDED.status").Set("context_json = EXCLUDED.context_json").
			Set("workspace = EXCLUDED.workspace").Set("runner_id = EXCLUDED.runner_id").
			Set("instance_id = EXCLUDED.instance_id").Set("project_id = EXCLUDED.project_id").Set("updated_at = EXCLUDED.updated_at").Exec(ctx)
		return err
	case "RUN_RESUMED":
		return updateRunProjection(ctx, tx, event.RunID, "running", event.TS)
	case "RUN_COMPLETED":
		return updateRunProjection(ctx, tx, event.RunID, "completed", event.TS)
	case "RUN_FAILED":
		return updateRunProjection(ctx, tx, event.RunID, "failed", event.TS)
	case "RUN_PAUSED":
		status := "paused"
		if runEvent.RunStatus == "waiting_approval" {
			status = runEvent.RunStatus
		}
		return updateRunProjection(ctx, tx, event.RunID, status, event.TS)
	case "RUN_CANCELLED":
		return updateRunProjection(ctx, tx, event.RunID, "cancelled", event.TS)
	case "ARTIFACT_PRODUCED":
		return replayArtifactProduced(ctx, tx, event)
	case "ARTIFACT_INVALIDATED", "ARTIFACT_STALE_MARKED", "ARTIFACT_ORPHANED", "ARTIFACT_FORCE_VALIDATED":
		return replayArtifactStatus(ctx, tx, event)
	case "REVIEW_APPROVED", "REVIEW_REJECTED":
		return replayReviewProjection(ctx, tx, event, runEvent)
	}
	if runEvent.NodeID == "" || runEvent.Status == "" {
		return nil
	}
	completedAt := int64(0)
	if runEvent.Status == "completed" || runEvent.Status == "failed" || runEvent.Status == "skipped" {
		completedAt = event.TS
	}
	node := NodeExecution{RunID: event.RunID, NodeID: runEvent.NodeID, Attempt: runEvent.Attempt + 1,
		Status: runEvent.Status, StartedAt: event.TS, CompletedAt: completedAt, Error: runEvent.Error}
	_, err := tx.NewInsert().Model(&node).On("CONFLICT (run_id, node_id, attempt) DO UPDATE SET status = excluded.status, completed_at = excluded.completed_at, error = excluded.error").Exec(ctx)
	if err != nil {
		return err
	}
	if event.Type == "REVIEW_REQUESTED" {
		return updateRunProjection(ctx, tx, event.RunID, "waiting_approval", event.TS)
	}
	return nil
}

func updateRunProjection(ctx context.Context, tx bun.Tx, runID, status string, ts int64) error {
	_, err := tx.NewUpdate().Model((*RunDef)(nil)).Set("status = ?", status).Set("updated_at = ?", ts).Where("id = ?", runID).Exec(ctx)
	return err
}

func replayArtifactProduced(ctx context.Context, tx bun.Tx, event Event) error {
	var payload replayArtifactEvent
	if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
		return err
	}
	if payload.Artifact.ID == "" {
		if err := json.Unmarshal([]byte(event.PayloadJSON), &payload.Artifact); err != nil {
			return err
		}
	}
	artifact := payload.Artifact
	if artifact.CreatedAt == 0 {
		artifact.CreatedAt = event.TS
	}
	artifact.UpdatedAt = event.TS
	var previous Artifact
	err := tx.NewSelect().Model(&previous).Where("id = ?", artifact.ID).Scan(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && previous.Checksum != artifact.Checksum {
		if err := markArtifactDependentsStale(ctx, tx, artifact.ID); err != nil {
			return err
		}
		artifact.CreatedAt = previous.CreatedAt
	}
	_, err = tx.NewInsert().Model(&artifact).On("CONFLICT (id) DO UPDATE").
		Set("logical_id = EXCLUDED.logical_id").Set("logical_path = EXCLUDED.logical_path").Set("type = EXCLUDED.type").
		Set("checksum = EXCLUDED.checksum").Set("size_bytes = EXCLUDED.size_bytes").Set("status = EXCLUDED.status").
		Set("force_valid = EXCLUDED.force_valid").Set("producer_run_id = EXCLUDED.producer_run_id").
		Set("producer_node_id = EXCLUDED.producer_node_id").Set("attempt = EXCLUDED.attempt").
		Set("runner_id = EXCLUDED.runner_id").Set("spawn_id = EXCLUDED.spawn_id").Set("updated_at = EXCLUDED.updated_at").Exec(ctx)
	if err != nil {
		return err
	}
	if _, err := tx.NewDelete().Model((*ArtifactDep)(nil)).Where("artifact_id = ?", artifact.ID).Exec(ctx); err != nil {
		return err
	}
	for _, dependency := range payload.Dependencies {
		dependency.ArtifactID = artifact.ID
		if dependency.CreatedAt == 0 {
			dependency.CreatedAt = event.TS
		}
		if _, err := tx.NewInsert().Model(&dependency).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

func replayArtifactStatus(ctx context.Context, tx bun.Tx, event Event) error {
	var payload Artifact
	if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
		return err
	}
	var envelope replayArtifactEvent
	if payload.ID == "" {
		if err := json.Unmarshal([]byte(event.PayloadJSON), &envelope); err != nil {
			return err
		}
		payload = envelope.Artifact
	}
	status := payload.Status
	force := payload.ForceValid
	switch event.Type {
	case "ARTIFACT_ORPHANED":
		status, force = ArtifactOrphaned, false
	case "ARTIFACT_STALE_MARKED":
		status, force = ArtifactStale, false
	case "ARTIFACT_INVALIDATED":
		status, force = ArtifactInvalid, false
	case "ARTIFACT_FORCE_VALIDATED":
		status, force = ArtifactValid, true
	}
	q := tx.NewUpdate().Model((*Artifact)(nil)).Set("status = ?", status).Set("force_valid = ?", force).
		Set("updated_at = ?", event.TS).Where("id = ?", payload.ID)
	if event.Type == "ARTIFACT_INVALIDATED" {
		q = q.Set("checksum = ?", payload.Checksum).Set("size_bytes = ?", payload.SizeBytes)
	}
	_, err := q.Exec(ctx)
	if err != nil {
		return err
	}
	if event.Type == "ARTIFACT_INVALIDATED" {
		return markArtifactDependentsStale(ctx, tx, payload.ID)
	}
	if event.Type == "ARTIFACT_FORCE_VALIDATED" {
		reviewer := envelope.Reviewer
		if reviewer == "" {
			reviewer = "human"
		}
		validation := ValidationResult{ID: "wrt-" + event.ID, ArtifactID: payload.ID, ValidatorID: "force_valid",
			ValidatorType: "human", Passed: 1, EvidenceJSON: `{"forceValid":true}`, ReviewedBy: reviewer, TS: event.TS}
		_, err = tx.NewInsert().Model(&validation).On("CONFLICT (id) DO NOTHING").Exec(ctx)
	}
	return err
}

func replayReviewProjection(ctx context.Context, tx bun.Tx, event Event, payload replayRunEvent) error {
	if payload.Review == nil {
		return fmt.Errorf("review event has no review payload")
	}
	passed := 0
	if payload.Review.Approved {
		passed = 1
	}
	evidence, _ := json.Marshal(map[string]string{"reason": payload.Review.Reason})
	validation := ValidationResult{ID: "wrt-v-" + event.ID, ArtifactID: event.RunID, ExecutionID: payload.NodeID,
		ValidatorID: "human", ValidatorType: "human", Passed: passed, EvidenceJSON: string(evidence), ReviewedBy: "human", TS: event.TS}
	if _, err := tx.NewInsert().Model(&validation).On("CONFLICT (id) DO NOTHING").Exec(ctx); err != nil {
		return err
	}
	if err := updateRunProjection(ctx, tx, event.RunID, "running", event.TS); err != nil {
		return err
	}
	if payload.Review.Approved || payload.Review.Feedback == nil {
		return nil
	}
	feedback := FeedbackLogEntry{ID: "wrt-f-" + event.ID, ArtifactID: event.RunID, ExecutionID: payload.NodeID,
		Reviewer: "human", Category: payload.Review.Feedback.Category, Location: payload.Review.Feedback.Location,
		Expected: payload.Review.Feedback.Expected, Detail: payload.Review.Feedback.Detail, TS: event.TS}
	_, err := tx.NewInsert().Model(&feedback).On("CONFLICT (id) DO NOTHING").Exec(ctx)
	return err
}

// RebuildCoreProjectionsIfEmpty recovers after projection rows were removed.
// Normal startups with an existing workflow_runs projection remain untouched.
func (s *Store) RebuildCoreProjectionsIfEmpty(ctx context.Context) error {
	count, err := s.db.NewSelect().Model((*RunDef)(nil)).Count(ctx)
	if err != nil || count > 0 {
		return err
	}
	eventCount, err := s.db.NewSelect().Model((*Event)(nil)).Where("type = ?", "RUN_STARTED").Count(ctx)
	if err != nil || eventCount == 0 {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return s.RebuildCoreProjections(ctx)
}
