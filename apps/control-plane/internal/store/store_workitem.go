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
	bun.BaseModel `bun:"table:work_items"`
	ID            string `bun:"id,pk" json:"id"`
	Type          string `bun:"type,notnull,default:'task'" json:"type"`
	Title         string `bun:"title,notnull" json:"title"`
	Description   string `bun:"description,notnull,default:''" json:"description"`
	Status        string `bun:"status,notnull,default:'open'" json:"status"`
	AssigneeAgent string `bun:"assignee_agent,notnull,default:''" json:"assigneeAgent"`
	ChannelID     string `bun:"channel_id,notnull,default:''" json:"channelId"`
	CreatedAt     int64  `bun:"created_at,notnull,default:0" json:"createdAt"`
	UpdatedAt     int64  `bun:"updated_at,notnull,default:0" json:"updatedAt"`
}

func (s *Store) CreateWorkItem(ctx context.Context, w *WorkItem) error {
	now := time.Now().UnixMilli()
	w.CreatedAt, w.UpdatedAt = now, now
	if _, err := s.db.NewInsert().Model(w).Exec(ctx); err != nil {
		return fmt.Errorf("create work item: %w", err)
	}
	return nil
}

func (s *Store) ListWorkItems(ctx context.Context, itemType, status string) ([]WorkItem, error) {
	out := []WorkItem{}
	q := s.db.NewSelect().Model(&out)
	if itemType != "" {
		q = q.Where("type = ?", itemType)
	}
	if status != "" {
		q = q.Where("status = ?", status)
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
		Set("status = ?", w.Status).Set("assignee_agent = ?", w.AssigneeAgent).
		Set("channel_id = ?", w.ChannelID).Set("updated_at = ?", w.UpdatedAt).
		Exec(ctx); err != nil {
		return fmt.Errorf("update work item: %w", err)
	}
	return nil
}

func (s *Store) DeleteWorkItem(ctx context.Context, id string) error {
	if _, err := s.db.NewDelete().Model(&WorkItem{}).Where("id = ?", id).Exec(ctx); err != nil {
		return fmt.Errorf("delete work item: %w", err)
	}
	return nil
}
