package store

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

// Okr is an objective + key results (plain CRUD; progress aggregation is client-side).
type Okr struct {
	bun.BaseModel `bun:"table:okrs"`
	ID            string `bun:"id,pk" json:"id"`
	Title         string `bun:"title,notnull" json:"title"`
	Objective     string `bun:"objective,notnull,default:''" json:"objective"`
	Period        string `bun:"period,notnull,default:''" json:"period"`
	KeyResults    string `bun:"key_results,notnull,default:'[]'" json:"keyResults"` // JSON [{title,target,progress,unit}]
	CreatedAt     int64  `bun:"created_at,notnull,default:0" json:"createdAt"`
	UpdatedAt     int64  `bun:"updated_at,notnull,default:0" json:"updatedAt"`
}

func (s *Store) CreateOkr(ctx context.Context, o *Okr) error {
	now := time.Now().UnixMilli()
	o.CreatedAt, o.UpdatedAt = now, now
	if _, err := s.db.NewInsert().Model(o).Exec(ctx); err != nil {
		return fmt.Errorf("create okr: %w", err)
	}
	return nil
}

func (s *Store) ListOkrs(ctx context.Context) ([]Okr, error) {
	out := []Okr{}
	if err := s.db.NewSelect().Model(&out).Order("updated_at DESC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list okrs: %w", err)
	}
	return out, nil
}

func (s *Store) GetOkr(ctx context.Context, id string) (*Okr, error) {
	var o Okr
	if err := s.db.NewSelect().Model(&o).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, fmt.Errorf("get okr: %w", err)
	}
	return &o, nil
}

func (s *Store) UpdateOkr(ctx context.Context, o *Okr) error {
	o.UpdatedAt = time.Now().UnixMilli()
	if _, err := s.db.NewUpdate().Model(o).Where("id = ?", o.ID).
		Set("title = ?", o.Title).Set("objective = ?", o.Objective).Set("period = ?", o.Period).
		Set("key_results = ?", o.KeyResults).Set("updated_at = ?", o.UpdatedAt).
		Exec(ctx); err != nil {
		return fmt.Errorf("update okr: %w", err)
	}
	return nil
}

func (s *Store) DeleteOkr(ctx context.Context, id string) error {
	if _, err := s.db.NewDelete().Model(&Okr{}).Where("id = ?", id).Exec(ctx); err != nil {
		return fmt.Errorf("delete okr: %w", err)
	}
	return nil
}
