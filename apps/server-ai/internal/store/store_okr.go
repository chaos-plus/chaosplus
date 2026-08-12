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

type Okr = workspace.Okr

type okrRow struct {
	bun.BaseModel `bun:"table:okrs"`
	ID            string `bun:"id,pk"`
	Title         string `bun:"title,notnull"`
	Objective     string `bun:"objective,notnull,default:''"`
	Period        string `bun:"period,notnull,default:''"`
	KeyResults    string `bun:"key_results,notnull,default:'[]'"`
	EntityID      string `bun:"entity_id,notnull,default:''"`
	CreatedAt     int64  `bun:"created_at,notnull,default:0"`
	UpdatedAt     int64  `bun:"updated_at,notnull,default:0"`
}

func okrToRow(o *workspace.Okr) *okrRow {
	return &okrRow{ID: o.ID, Title: o.Title, Objective: o.Objective, Period: o.Period, KeyResults: o.KeyResults, EntityID: o.EntityID, CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt}
}

func okrFromRow(o okrRow) workspace.Okr {
	return workspace.Okr{ID: o.ID, Title: o.Title, Objective: o.Objective, Period: o.Period, KeyResults: o.KeyResults, EntityID: o.EntityID, CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt}
}

func (s *Store) CreateOkr(ctx context.Context, o *workspace.Okr) error {
	now := time.Now().UnixMilli()
	o.CreatedAt, o.UpdatedAt, o.EntityID = now, now, EntityOf(ctx)
	_, err := s.db.NewInsert().Model(okrToRow(o)).Exec(ctx)
	return err
}

func (s *Store) ListOkrs(ctx context.Context) ([]workspace.Okr, error) {
	rows := []okrRow{}
	q := scopeEntity(s.db.NewSelect().Model(&rows), ctx)
	if err := q.Order("updated_at DESC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list okrs: %w", err)
	}
	out := make([]workspace.Okr, 0, len(rows))
	for _, row := range rows {
		out = append(out, okrFromRow(row))
	}
	return out, nil
}

func (s *Store) GetOkr(ctx context.Context, id string) (*workspace.Okr, error) {
	var row okrRow
	q := scopeEntity(s.db.NewSelect().Model(&row).Where("id = ?", id), ctx)
	if err := q.Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, workspace.ErrNotFound
		}
		return nil, err
	}
	o := okrFromRow(row)
	return &o, nil
}

func (s *Store) UpdateOkr(ctx context.Context, o *workspace.Okr) error {
	o.UpdatedAt = time.Now().UnixMilli()
	q := s.db.NewUpdate().Model(okrToRow(o)).Where("id = ?", o.ID).
		Set("title = ?", o.Title).Set("objective = ?", o.Objective).Set("period = ?", o.Period).
		Set("key_results = ?", o.KeyResults).Set("updated_at = ?", o.UpdatedAt)
	q = scopeEntity(q, ctx)
	res, err := q.Exec(ctx)
	if err != nil {
		return err
	}
	return requireAffected(res, workspace.ErrNotFound)
}

func (s *Store) DeleteOkr(ctx context.Context, id string) error {
	q := scopeEntity(s.db.NewDelete().Model(&okrRow{}).Where("id = ?", id), ctx)
	res, err := q.Exec(ctx)
	if err != nil {
		return err
	}
	return requireAffected(res, workspace.ErrNotFound)
}
