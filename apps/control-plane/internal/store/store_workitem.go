package store

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

// WorkItem is a workspace item (requirement / task / bug) — the system of record
// that chat channels reference and subscribe to.
type WorkItem struct {
	bun.BaseModel  `bun:"table:work_items"`
	ID             string  `bun:"id,pk" json:"id"`
	Type           string  `bun:"type,notnull,default:'task'" json:"type"`
	Title          string  `bun:"title,notnull" json:"title"`
	Description    string  `bun:"description,notnull,default:''" json:"description"`
	Status         string  `bun:"status,notnull,default:'open'" json:"status"`
	ParentID       string  `bun:"parent_id,notnull,default:''" json:"parentId"`
	EstimateHours  float64 `bun:"estimate_hours,notnull,default:0" json:"estimateHours"`
	SpentHours     float64 `bun:"spent_hours,notnull,default:0" json:"spentHours"`
	Progress       int     `bun:"progress,notnull,default:0" json:"progress"`
	WorkflowRunID  string  `bun:"workflow_run_id,notnull,default:''" json:"workflowRunId"`
	AssigneeAgent  string  `bun:"assignee_agent,notnull,default:''" json:"assigneeAgent"`
	ChannelID      string  `bun:"channel_id,notnull,default:''" json:"channelId"`
	CreatedAt      int64   `bun:"created_at,notnull,default:0" json:"createdAt"`
	UpdatedAt      int64   `bun:"updated_at,notnull,default:0" json:"updatedAt"`
}

func (s *Store) CreateWorkItem(ctx context.Context, w *WorkItem) error {
	now := time.Now().UnixMilli()
	w.CreatedAt, w.UpdatedAt = now, now
	if _, err := s.db.NewInsert().Model(w).Exec(ctx); err != nil {
		return fmt.Errorf("create work item: %w", err)
	}
	return nil
}

func (s *Store) ListWorkItems(ctx context.Context, itemType, status, parent string) ([]WorkItem, error) {
	out := []WorkItem{}
	q := s.db.NewSelect().Model(&out)
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
	return out, nil
}

func (s *Store) GetWorkItem(ctx context.Context, id string) (*WorkItem, error) {
	var w WorkItem
	if err := s.db.NewSelect().Model(&w).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, fmt.Errorf("get work item: %w", err)
	}
	return &w, nil
}

func (s *Store) UpdateWorkItem(ctx context.Context, w *WorkItem) error {
	w.UpdatedAt = time.Now().UnixMilli()
	if _, err := s.db.NewUpdate().Model(w).Where("id = ?", w.ID).
		Set("type = ?", w.Type).Set("title = ?", w.Title).Set("description = ?", w.Description).
		Set("status = ?", w.Status).Set("parent_id = ?", w.ParentID).
		Set("estimate_hours = ?", w.EstimateHours).Set("spent_hours = ?", w.SpentHours).
		Set("progress = ?", w.Progress).Set("workflow_run_id = ?", w.WorkflowRunID).
		Set("assignee_agent = ?", w.AssigneeAgent).
		Set("channel_id = ?", w.ChannelID).Set("updated_at = ?", w.UpdatedAt).
		Exec(ctx); err != nil {
		return fmt.Errorf("update work item: %w", err)
	}
	return nil
}

// CalibrateEstimate 回填估时:首次执行完成且人工未估时,用实际耗时作为校准值。
func (s *Store) CalibrateEstimate(ctx context.Context, id string, spentHours float64) error {
	if _, err := s.db.NewUpdate().Model(&WorkItem{}).
		Where("id = ? AND estimate_hours <= 0", id).
		Set("estimate_hours = ?", spentHours).
		Exec(ctx); err != nil {
		return fmt.Errorf("calibrate estimate: %w", err)
	}
	return nil
}

// RollupParent 把子任务的工时/进度汇总到父项(进度取子项均值,工时取合计)。
func (s *Store) RollupParent(ctx context.Context, parentID string) error {
	if parentID == "" {
		return nil
	}
	kids, err := s.ListWorkItems(ctx, "", "", parentID)
	if err != nil {
		return err
	}
	if len(kids) == 0 {
		return nil
	}
	var estimate, spent float64
	var progress int
	for _, k := range kids {
		estimate += k.EstimateHours
		spent += k.SpentHours
		progress += k.Progress
	}
	progress /= len(kids)
	if _, err := s.db.NewUpdate().Model(&WorkItem{}).Where("id = ?", parentID).
		Set("estimate_hours = ?", estimate).Set("spent_hours = ?", spent).
		Set("progress = ?", progress).Set("updated_at = ?", time.Now().UnixMilli()).
		Exec(ctx); err != nil {
		return fmt.Errorf("rollup parent: %w", err)
	}
	return nil
}

// UpdateWorkItemRun 只在 run 推进时更新 progress/status/spent_hours(投影,不覆盖人工字段)。
func (s *Store) UpdateWorkItemRun(ctx context.Context, id string, progress int, status string, spentHours float64) error {
	now := time.Now().UnixMilli()
	if _, err := s.db.NewUpdate().Model(&WorkItem{}).Where("id = ?", id).
		Set("progress = ?", progress).Set("status = ?", status).
		Set("spent_hours = ?", spentHours).Set("updated_at = ?", now).
		Exec(ctx); err != nil {
		return fmt.Errorf("update work item run: %w", err)
	}
	return nil
}

func (s *Store) DeleteWorkItem(ctx context.Context, id string) error {
	if _, err := s.db.NewDelete().Model(&WorkItem{}).Where("id = ?", id).Exec(ctx); err != nil {
		return fmt.Errorf("delete work item: %w", err)
	}
	return nil
}
