package store

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

// NodeExecution is one node's attempt (PRD §16 node_executions). Run/def
// persistence lives in the merged base (RunDef / workflow_runs, migrations
// 00011_workflow_runs + 00012_workflows).
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

// ListNodeExecutions returns a run's node attempts, oldest first.
func (s *Store) ListNodeExecutions(ctx context.Context, runID string) ([]NodeExecution, error) {
	out := []NodeExecution{}
	if err := s.db.NewSelect().Model(&out).Where("run_id = ?", runID).Order("started_at ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("store list node executions: %w", err)
	}
	return out, nil
}

// GetRunDef returns a single persisted run (workflow_runs table).
func (s *Store) GetRunDef(ctx context.Context, runID string) (RunDef, error) {
	var r RunDef
	if err := s.db.NewSelect().Model(&r).Where("id = ?", runID).Scan(ctx); err != nil {
		return r, fmt.Errorf("store get run def: %w", err)
	}
	return r, nil
}
