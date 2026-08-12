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

type Attachment = workspace.Attachment

type attachmentRow struct {
	bun.BaseModel `bun:"table:attachments"`
	ID            string `bun:"id,pk"`
	OwnerType     string `bun:"owner_type,notnull"`
	OwnerID       string `bun:"owner_id,notnull"`
	Filename      string `bun:"filename,notnull"`
	Mime          string `bun:"mime,notnull,default:'application/octet-stream'"`
	SizeBytes     int64  `bun:"size_bytes,notnull,default:0"`
	StorePath     string `bun:"store_path,notnull"`
	EntityID      string `bun:"entity_id,notnull,default:''"`
	CreatedAt     int64  `bun:"created_at,notnull,default:0"`
}

func attachmentToRow(a *workspace.Attachment) *attachmentRow {
	return &attachmentRow{ID: a.ID, OwnerType: a.OwnerType, OwnerID: a.OwnerID, Filename: a.Filename, Mime: a.Mime, SizeBytes: a.SizeBytes, StorePath: a.StorePath, EntityID: a.EntityID, CreatedAt: a.CreatedAt}
}

func attachmentFromRow(a attachmentRow) workspace.Attachment {
	return workspace.Attachment{ID: a.ID, OwnerType: a.OwnerType, OwnerID: a.OwnerID, Filename: a.Filename, Mime: a.Mime, SizeBytes: a.SizeBytes, StorePath: a.StorePath, EntityID: a.EntityID, CreatedAt: a.CreatedAt}
}

func (s *Store) CreateAttachment(ctx context.Context, a *workspace.Attachment) error {
	a.CreatedAt, a.EntityID = time.Now().UnixMilli(), EntityOf(ctx)
	_, err := s.db.NewInsert().Model(attachmentToRow(a)).Exec(ctx)
	return err
}

func (s *Store) ListAttachments(ctx context.Context, ownerType, ownerID string) ([]workspace.Attachment, error) {
	rows := []attachmentRow{}
	q := s.db.NewSelect().Model(&rows).Where("owner_type = ? AND owner_id = ?", ownerType, ownerID)
	q = scopeEntity(q, ctx)
	if err := q.Order("created_at ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list attachments: %w", err)
	}
	out := make([]workspace.Attachment, 0, len(rows))
	for _, row := range rows {
		out = append(out, attachmentFromRow(row))
	}
	return out, nil
}

func (s *Store) GetAttachment(ctx context.Context, id string) (*workspace.Attachment, error) {
	var row attachmentRow
	q := scopeEntity(s.db.NewSelect().Model(&row).Where("id = ?", id), ctx)
	if err := q.Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, workspace.ErrNotFound
		}
		return nil, err
	}
	a := attachmentFromRow(row)
	return &a, nil
}

func (s *Store) DeleteAttachment(ctx context.Context, id string) error {
	q := scopeEntity(s.db.NewDelete().Model(&attachmentRow{}).Where("id = ?", id), ctx)
	res, err := q.Exec(ctx)
	if err != nil {
		return err
	}
	return requireAffected(res, workspace.ErrNotFound)
}
