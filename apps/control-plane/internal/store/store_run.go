package store

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

// RunRecord is one persisted workflow run (PRD §16 workflow_runs). A run's
// authoritative trace lives in the events table; this row is the coarse
// lifecycle projection that survives restarts (as history) and enables crash
// recovery of in-flight runs.
type RunRecord struct {
	bun.BaseModel `bun:"table:workflow_runs"`
	ID            string `bun:"id,pk"`
	WorkflowID    string `bun:"workflow_id,notnull,default:''"`
	WorkflowVer   string `bun:"workflow_version,notnull,default:''"`
	DefSnapshot   string `bun:"workflow_def_snapshot,notnull,default:'{}'"`
	Status        string `bun:"status,notnull,default:'running'"`
	Workspace     string `bun:"workspace,notnull,default:''"`
	StartedAt     int64  `bun:"started_at,notnull"`
	CompletedAt   int64  `bun:"completed_at,notnull,default:0"`
}

// NodeExecution is one node's attempt (PRD §16 node_executions).
type NodeExecution struct {
	bun.BaseModel `bun:"table:node_executions"`
	RunID         string `bun:"run_id,pk"`
	NodeID        string `bun:"node_id,pk"`
	Attempt       int    `bun:"attempt,pk,default:1"`
	Status        string `bun:"status,notnull,default:'pending'"`
	StartedAt     int64  `bun:"started_at,notnull,default:0"`
	CompletedAt   int64  `bun:"completed_at,notnull,default:0"`
	Error         string `bun:"error,notnull,default:''"`
}

// CreateRun persists a run when it launches.
func (s *Store) CreateRun(ctx context.Context, r RunRecord) error {
	r.StartedAt = time.Now().UnixMilli()
	if _, err := s.db.NewInsert().Model(&r).On("CONFLICT (id) DO NOTHING").Exec(ctx); err != nil {
		return fmt.Errorf("store create run: %w", err)
	}
	return nil
}

// UpdateRunStatus persists a run's coarse status (running/…/completed/failed).
func (s *Store) UpdateRunStatus(ctx context.Context, runID, status string, completedAt int64) error {
	if _, err := s.db.NewUpdate().Model((*RunRecord)(nil)).
		Set("status = ?", status).
		Set("completed_at = ?", completedAt).
		Where("id = ?", runID).Exec(ctx); err != nil {
		return fmt.Errorf("store update run status: %w", err)
	}
	return nil
}

// UpsertNodeExecution records a node attempt's latest status/error.
func (s *Store) UpsertNodeExecution(ctx context.Context, n NodeExecution) error {
	if n.StartedAt == 0 {
		n.StartedAt = time.Now().UnixMilli()
	}
	if _, err := s.db.NewInsert().Model(&n).
		On("CONFLICT (run_id, node_id, attempt) DO UPDATE SET status = excluded.status, completed_at = excluded.completed_at, error = excluded.error").
		Exec(ctx); err != nil {
		return fmt.Errorf("store upsert node execution: %w", err)
	}
	return nil
}

// ListRuns returns persisted runs, newest first.
func (s *Store) ListRuns(ctx context.Context, limit int) ([]RunRecord, error) {
	out := []RunRecord{}
	if err := s.db.NewSelect().Model(&out).Order("started_at DESC").Limit(limit).Scan(ctx); err != nil {
		return nil, fmt.Errorf("store list runs: %w", err)
	}
	return out, nil
}

// GetRun returns a single persisted run by ID.
func (s *Store) GetRun(ctx context.Context, runID string) (RunRecord, error) {
	var r RunRecord
	if err := s.db.NewSelect().Model(&r).Where("id = ?", runID).Scan(ctx); err != nil {
		return r, fmt.Errorf("store get run: %w", err)
	}
	return r, nil
}

// ListNodeExecutions returns a run's node attempts, oldest first.
func (s *Store) ListNodeExecutions(ctx context.Context, runID string) ([]NodeExecution, error) {
	out := []NodeExecution{}
	if err := s.db.NewSelect().Model(&out).Where("run_id = ?", runID).Order("started_at ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("store list node executions: %w", err)
	}
	return out, nil
}

// CrashRecoverRunning marks any run still genuinely mid-flight as failed (the
// process died mid-run: PRD F.3 infra failure). `paused` is a deliberate
// terminal outcome (F.5 onReject=pause) and must NOT be flipped to failed;
// `waiting_approval` runs are stranded by the crash and consumed as failed (the
// pending decision is lost — documented tradeoff until live rehydration).
// Returns the count recovered.
func (s *Store) CrashRecoverRunning(ctx context.Context) (int, error) {
	res, err := s.db.NewUpdate().Model((*RunRecord)(nil)).
		Set("status = 'failed'").
		Set("completed_at = ?", time.Now().UnixMilli()).
		Where("status IN ('running','waiting_approval')").
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("store crash recover: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
