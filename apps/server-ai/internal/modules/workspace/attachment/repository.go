package attachment

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

type Repository interface {
	Create(context.Context, *Attachment) error
	List(context.Context, ResourceType, guid.ID) ([]Attachment, error)
	Get(context.Context, guid.ID) (*Attachment, error)
	MarkDeleting(context.Context, guid.ID, int64) (*Attachment, error)
	MarkDeleteFailed(context.Context, guid.ID, int64) error
	CompleteDelete(context.Context, guid.ID, int64) error
}

type BunRepository struct{ db *bun.DB }

func NewRepository(db *bun.DB) *BunRepository {
	if db == nil {
		panic("attachment repository requires database")
	}
	return &BunRepository{db: db}
}

func scope(ctx context.Context) (guid.ID, guid.ID, guid.ID, error) {
	tenantID, entityID, principalID := authn.TenantIDFromContext(ctx), authn.EntityIDFromContext(ctx), authn.PrincipalIDFromContext(ctx)
	if tenantID.Zero() || entityID.Zero() || principalID.Zero() {
		return 0, 0, 0, errors.New("attachment requires verified IAM claims")
	}
	return tenantID, entityID, principalID, nil
}

func (r *BunRepository) Create(ctx context.Context, value *Attachment) error {
	tenantID, entityID, principalID, err := scope(ctx)
	if err != nil {
		return err
	}
	if value.ID.Zero() || value.TenantID != tenantID || value.EntityID != entityID || value.OwnerID != principalID {
		return ErrInvalid
	}
	now := time.Now().UTC().UnixMilli()
	value.Status, value.CreatedAt, value.UpdatedAt = StatusAvailable, now, now
	value.CreatedBy, value.UpdatedBy, value.Version = principalID, principalID, 1
	if _, err := r.db.NewInsert().Model(value).Table("workspace_attachments").Exec(ctx); err != nil {
		return fmt.Errorf("create attachment: %w", err)
	}
	return nil
}

func (r *BunRepository) List(ctx context.Context, resourceType ResourceType, resourceID guid.ID) ([]Attachment, error) {
	tenantID, entityID, _, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	items := []Attachment{}
	err = r.db.NewSelect().Model(&items).Table("workspace_attachments").
		Where("tenant_id = ? AND entity_id = ? AND resource_type = ? AND resource_id = ? AND deleted_at = 0", tenantID, entityID, resourceType, resourceID).
		Order("created_at ASC", "id ASC").Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("list attachments: %w", err)
	}
	return items, nil
}

func (r *BunRepository) Get(ctx context.Context, id guid.ID) (*Attachment, error) {
	tenantID, entityID, _, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	value := new(Attachment)
	err = r.db.NewSelect().Model(value).Table("workspace_attachments").
		Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0", id, tenantID, entityID).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get attachment: %w", err)
	}
	return value, nil
}

func (r *BunRepository) MarkDeleting(ctx context.Context, id guid.ID, version int64) (*Attachment, error) {
	tenantID, entityID, principalID, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().UnixMilli()
	result, err := r.db.NewUpdate().Model((*Attachment)(nil)).Table("workspace_attachments").
		Set("status = ?", StatusDeleting).Set("updated_at = ?", now).Set("updated_by = ?", principalID).Set("version = version + 1").
		Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0 AND version = ? AND status IN (?, ?)", id, tenantID, entityID, version, StatusAvailable, StatusDeleteFailed).Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("mark attachment deleting: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if rows == 0 {
		return nil, ErrVersionConflict
	}
	return r.Get(ctx, id)
}

func (r *BunRepository) MarkDeleteFailed(ctx context.Context, id guid.ID, version int64) error {
	tenantID, entityID, principalID, err := scope(ctx)
	if err != nil {
		return err
	}
	result, err := r.db.NewUpdate().Model((*Attachment)(nil)).Table("workspace_attachments").
		Set("status = ?", StatusDeleteFailed).Set("updated_at = ?", time.Now().UTC().UnixMilli()).Set("updated_by = ?", principalID).Set("version = version + 1").
		Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0 AND version = ? AND status = ?", id, tenantID, entityID, version, StatusDeleting).Exec(ctx)
	if err != nil {
		return fmt.Errorf("mark attachment delete failed: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrVersionConflict
	}
	return nil
}

func (r *BunRepository) CompleteDelete(ctx context.Context, id guid.ID, version int64) error {
	tenantID, entityID, principalID, err := scope(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	result, err := r.db.NewUpdate().Model((*Attachment)(nil)).Table("workspace_attachments").
		Set("deleted_at = ?", now).Set("deleted_by = ?", principalID).Set("updated_at = ?", now).Set("updated_by = ?", principalID).Set("version = version + 1").
		Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0 AND version = ? AND status = ?", id, tenantID, entityID, version, StatusDeleting).Exec(ctx)
	if err != nil {
		return fmt.Errorf("complete attachment delete: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrVersionConflict
	}
	return nil
}
