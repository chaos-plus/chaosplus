package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace"
	"github.com/uptrace/bun"
)

// WorkItem remains an alias for source compatibility. The aggregate is owned
// by the workspace module; this package is only its SQLite adapter.
type WorkItem = workspace.WorkItem

type workItemRow struct {
	bun.BaseModel `bun:"table:work_items"`
	ID            string  `bun:"id,pk"`
	Type          string  `bun:"type,notnull,default:'task'"`
	Title         string  `bun:"title,notnull"`
	Description   string  `bun:"description,notnull,default:''"`
	Status        string  `bun:"status,notnull,default:'open'"`
	ParentID      string  `bun:"parent_id,notnull,default:''"`
	EstimateHours float64 `bun:"estimate_hours,notnull,default:0"`
	SpentHours    float64 `bun:"spent_hours,notnull,default:0"`
	Progress      int     `bun:"progress,notnull,default:0"`
	WorkflowRunID string  `bun:"workflow_run_id,notnull,default:''"`
	AssigneeAgent string  `bun:"assignee_agent,notnull,default:''"`
	ChannelID     string  `bun:"channel_id,notnull,default:''"`
	EntityID      string  `bun:"entity_id,notnull,default:''"`
	CreatedAt     int64   `bun:"created_at,notnull,default:0"`
	UpdatedAt     int64   `bun:"updated_at,notnull,default:0"`
}

func workItemToRow(w *workspace.WorkItem) *workItemRow {
	return &workItemRow{ID: w.ID, Type: w.Type, Title: w.Title, Description: w.Description, Status: w.Status,
		ParentID: w.ParentID, EstimateHours: w.EstimateHours, SpentHours: w.SpentHours, Progress: w.Progress,
		WorkflowRunID: w.WorkflowRunID, AssigneeAgent: w.AssigneeAgent, ChannelID: w.ChannelID,
		EntityID: w.EntityID, CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt}
}

func workItemFromRow(w workItemRow) workspace.WorkItem {
	return workspace.WorkItem{ID: w.ID, Type: w.Type, Title: w.Title, Description: w.Description, Status: w.Status,
		ParentID: w.ParentID, EstimateHours: w.EstimateHours, SpentHours: w.SpentHours, Progress: w.Progress,
		WorkflowRunID: w.WorkflowRunID, AssigneeAgent: w.AssigneeAgent, ChannelID: w.ChannelID,
		EntityID: w.EntityID, CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt}
}

func (s *Store) CreateWorkItem(ctx context.Context, w *workspace.WorkItem) error {
	now := time.Now().UnixMilli()
	w.CreatedAt, w.UpdatedAt = now, now
	w.EntityID = EntityOf(ctx)
	if _, err := s.db.NewInsert().Model(workItemToRow(w)).Exec(ctx); err != nil {
		return fmt.Errorf("create work item: %w", err)
	}
	return nil
}

func (s *Store) ListWorkItems(ctx context.Context, itemType, status, parent string) ([]workspace.WorkItem, error) {
	rows := []workItemRow{}
	q := s.db.NewSelect().Model(&rows)
	q = scopeEntity(q, ctx)
	if itemType != "" {
		q = q.Where("type = ?", itemType)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if parent != "" {
		q = q.Where("parent_id = ?", parent)
	}
	if err := q.Order("updated_at DESC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list work items: %w", err)
	}
	out := make([]workspace.WorkItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, workItemFromRow(row))
	}
	return out, nil
}

func (s *Store) GetWorkItem(ctx context.Context, id string) (*workspace.WorkItem, error) {
	var row workItemRow
	q := scopeEntity(s.db.NewSelect().Model(&row).Where("id = ?", id), ctx)
	if err := q.Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, workspace.ErrNotFound
		}
		return nil, fmt.Errorf("get work item: %w", err)
	}
	item := workItemFromRow(row)
	return &item, nil
}

func (s *Store) UpdateWorkItem(ctx context.Context, w *workspace.WorkItem) error {
	w.UpdatedAt = time.Now().UnixMilli()
	row := workItemToRow(w)
	q := s.db.NewUpdate().Model(row).Where("id = ?", w.ID).
		Set("type = ?", w.Type).Set("title = ?", w.Title).Set("description = ?", w.Description).
		Set("status = ?", w.Status).Set("parent_id = ?", w.ParentID).
		Set("estimate_hours = ?", w.EstimateHours).Set("spent_hours = ?", w.SpentHours).
		Set("progress = ?", w.Progress).Set("workflow_run_id = ?", w.WorkflowRunID).
		Set("assignee_agent = ?", w.AssigneeAgent).Set("channel_id = ?", w.ChannelID).
		Set("updated_at = ?", w.UpdatedAt)
	q = scopeEntity(q, ctx)
	res, err := q.Exec(ctx)
	if err != nil {
		return fmt.Errorf("update work item: %w", err)
	}
	return requireAffected(res, workspace.ErrNotFound)
}

func (s *Store) ReconcileStaleRunning(ctx context.Context) (int64, error) {
	q := s.db.NewUpdate().Model(&workItemRow{}).Where("status = ?", "in_progress").
		Set("status = ?", "review").Set("updated_at = ?", time.Now().UnixMilli())
	q = scopeEntity(q, ctx)
	res, err := q.Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("reconcile stale running: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (s *Store) CalibrateEstimate(ctx context.Context, id string, spentHours float64) error {
	q := s.db.NewUpdate().Model(&workItemRow{}).Where("id = ? AND estimate_hours <= 0", id).
		Set("estimate_hours = ?", spentHours)
	q = scopeEntity(q, ctx)
	_, err := q.Exec(ctx)
	if err != nil {
		return fmt.Errorf("calibrate estimate: %w", err)
	}
	return nil
}

func (s *Store) RollupParent(ctx context.Context, parentID string) error {
	if parentID == "" {
		return nil
	}
	kids, err := s.ListWorkItems(ctx, "", "", parentID)
	if err != nil || len(kids) == 0 {
		return err
	}
	var estimate, spent float64
	var progress int
	for _, kid := range kids {
		estimate += kid.EstimateHours
		spent += kid.SpentHours
		progress += kid.Progress
	}
	q := s.db.NewUpdate().Model(&workItemRow{}).Where("id = ?", parentID).
		Set("estimate_hours = ?", estimate).Set("spent_hours = ?", spent).
		Set("progress = ?", progress/len(kids)).Set("updated_at = ?", time.Now().UnixMilli())
	q = scopeEntity(q, ctx)
	_, err = q.Exec(ctx)
	return err
}

func (s *Store) UpdateWorkItemRun(ctx context.Context, id string, progress int, status string, spentHours float64) error {
	q := s.db.NewUpdate().Model(&workItemRow{}).Where("id = ?", id).
		Set("progress = ?", progress).Set("status = ?", status).
		Set("spent_hours = ?", spentHours).Set("updated_at = ?", time.Now().UnixMilli())
	q = scopeEntity(q, ctx)
	res, err := q.Exec(ctx)
	if err != nil {
		return fmt.Errorf("update work item run: %w", err)
	}
	return requireAffected(res, workspace.ErrNotFound)
}

func (s *Store) DeleteWorkItem(ctx context.Context, id string) error {
	q := scopeEntity(s.db.NewDelete().Model(&workItemRow{}).Where("id = ?", id), ctx)
	res, err := q.Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete work item: %w", err)
	}
	return requireAffected(res, workspace.ErrNotFound)
}

func scopeEntity[T interface{ Where(string, ...any) T }](q T, ctx context.Context) T {
	if entityID := EntityOf(ctx); entityID != "" {
		return q.Where("entity_id = ?", entityID)
	}
	return q
}

func requireAffected(result sql.Result, notFound error) error {
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return notFound
	}
	return nil
}
