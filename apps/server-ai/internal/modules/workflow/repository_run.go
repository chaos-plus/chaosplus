package workflow

import (
	"context"
	"fmt"
	"time"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

// NodeExecution is one node's attempt (PRD §16 node_executions). Run/def
// persistence lives in the merged base (RunDef / workflow_runs, migrations
// 00011_workflow_runs + 00012_workflows).
type NodeExecution struct {
	bun.BaseModel `bun:"table:node_executions"`
	TenantID      guid.ID `bun:"tenant_id,pk"`
	EntityID      guid.ID `bun:"entity_id,pk"`
	RunID         guid.ID `bun:"run_id,pk"`
	NodeKey       string  `bun:"node_key,pk"`
	Attempt       int     `bun:"attempt,pk,default:1"`
	Status        string  `bun:"status,notnull,default:'pending'"`
	StartedAt     int64   `bun:"started_at,notnull,default:0"`
	CompletedAt   int64   `bun:"completed_at,notnull,default:0"`
	Error         string  `bun:"error,notnull,default:''"`
}

// UpsertNodeExecution records a node attempt's latest status/error.
func (s *BunRepository) UpsertNodeExecution(ctx context.Context, n NodeExecution) error {
	if n.StartedAt == 0 {
		n.StartedAt = time.Now().UnixMilli()
	}
	if _, err := s.db.NewInsert().Model(&n).
		On("CONFLICT (tenant_id, entity_id, run_id, node_key, attempt) DO UPDATE SET status = excluded.status, completed_at = excluded.completed_at, error = excluded.error").
		Exec(ctx); err != nil {
		return fmt.Errorf("store upsert node execution: %w", err)
	}
	return nil
}

// ListNodeExecutions returns a run's node attempts, oldest first.
func (s *BunRepository) ListNodeExecutions(ctx context.Context, runID guid.ID) ([]NodeExecution, error) {
	claims, err := requireClaims(ctx)
	if err != nil {
		return nil, err
	}
	out := []NodeExecution{}
	if err := s.db.NewSelect().Model(&out).Where("tenant_id = ? AND entity_id = ? AND run_id = ?", claims.TenantID, claims.EntityID, runID).Order("started_at ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("store list node executions: %w", err)
	}
	return out, nil
}

// GetRunDef returns a single persisted run (workflow_runs table).
func (s *BunRepository) GetRunDef(ctx context.Context, runID guid.ID) (RunDef, error) {
	claims, err := requireClaims(ctx)
	if err != nil {
		return RunDef{}, err
	}
	var r RunDef
	if err := s.db.NewSelect().Model(&r).Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0", runID, claims.TenantID, claims.EntityID).Scan(ctx); err != nil {
		return r, fmt.Errorf("store get run def: %w", err)
	}
	return r, nil
}
