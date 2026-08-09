package store

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

// Attachment is a user-uploaded file (image/video/doc) owned by a work item or
// a chat message. Stored on the control-plane ARTIFACT_ROOT; only metadata + a
// server-controlled path live in the DB.
type Attachment struct {
	bun.BaseModel `bun:"table:attachments"`
	ID            string `bun:"id,pk" json:"id"`
	OwnerType     string `bun:"owner_type,notnull" json:"ownerType"` // work_item | message
	OwnerID       string `bun:"owner_id,notnull" json:"ownerId"`
	Filename      string `bun:"filename,notnull" json:"filename"`
	Mime          string `bun:"mime,notnull,default:'application/octet-stream'" json:"mime"`
	SizeBytes     int64  `bun:"size_bytes,notnull,default:0" json:"sizeBytes"`
	StorePath     string `bun:"store_path,notnull" json:"-"`
	CreatedAt     int64  `bun:"created_at,notnull,default:0" json:"createdAt"`
}

func (s *Store) CreateAttachment(ctx context.Context, a *Attachment) error {
	a.CreatedAt = time.Now().UnixMilli()
	if _, err := s.db.NewInsert().Model(a).Exec(ctx); err != nil {
		return fmt.Errorf("create attachment: %w", err)
	}
	return nil
}

func (s *Store) ListAttachments(ctx context.Context, ownerType, ownerID string) ([]Attachment, error) {
	out := []Attachment{}
	if err := s.db.NewSelect().Model(&out).Where("owner_type = ? AND owner_id = ?", ownerType, ownerID).Order("created_at ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list attachments: %w", err)
	}
	return out, nil
}

func (s *Store) GetAttachment(ctx context.Context, id string) (*Attachment, error) {
	var a Attachment
	if err := s.db.NewSelect().Model(&a).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, fmt.Errorf("get attachment: %w", err)
	}
	return &a, nil
}

func (s *Store) DeleteAttachment(ctx context.Context, id string) error {
	if _, err := s.db.NewDelete().Model(&Attachment{}).Where("id = ?", id).Exec(ctx); err != nil {
		return fmt.Errorf("delete attachment: %w", err)
	}
	return nil
}
