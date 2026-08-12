package requirement

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
	Create(context.Context, *Requirement) error
	List(context.Context, Status, *guid.ID) ([]Requirement, error)
	Get(context.Context, guid.ID) (*Requirement, error)
	Update(context.Context, *Requirement, int64) error
	Delete(context.Context, guid.ID, int64) error
}

type BunRepository struct {
	db     *bun.DB
	nextID func() (guid.ID, error)
}

func NewRepository(db *bun.DB, nextID func() (guid.ID, error)) *BunRepository {
	if db == nil || nextID == nil {
		panic("requirement repository requires database and id generator")
	}
	return &BunRepository{db: db, nextID: nextID}
}

func scope(ctx context.Context) (guid.ID, guid.ID, guid.ID, error) {
	tenantID, entityID, principalID := authn.TenantIDFromContext(ctx), authn.EntityIDFromContext(ctx), authn.PrincipalIDFromContext(ctx)
	if tenantID.Zero() || entityID.Zero() || principalID.Zero() {
		return 0, 0, 0, errors.New("requirement requires verified IAM claims")
	}
	return tenantID, entityID, principalID, nil
}

func (r *BunRepository) Create(ctx context.Context, value *Requirement) error {
	tenantID, entityID, principalID, err := scope(ctx)
	if err != nil {
		return err
	}
	value.ID, err = r.nextID()
	if err != nil {
		return fmt.Errorf("generate requirement id: %w", err)
	}
	now := time.Now().UTC().UnixMilli()
	value.TenantID, value.EntityID, value.OwnerID = tenantID, entityID, principalID
	value.CreatedAt, value.UpdatedAt = now, now
	value.CreatedBy, value.UpdatedBy, value.Version = principalID, principalID, 1
	if _, err := r.db.NewInsert().Model(value).Table("workspace_requirements").Exec(ctx); err != nil {
		return fmt.Errorf("create requirement: %w", err)
	}
	return nil
}

func (r *BunRepository) List(ctx context.Context, status Status, parentID *guid.ID) ([]Requirement, error) {
	tenantID, entityID, _, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	items := []Requirement{}
	query := r.db.NewSelect().Model(&items).Table("workspace_requirements").
		Where("tenant_id = ? AND entity_id = ? AND deleted_at = 0", tenantID, entityID)
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if parentID != nil {
		query = query.Where("parent_id = ?", *parentID)
	}
	if err := query.Order("updated_at DESC", "id DESC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list requirements: %w", err)
	}
	return items, nil
}

func (r *BunRepository) Get(ctx context.Context, id guid.ID) (*Requirement, error) {
	tenantID, entityID, _, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	value := new(Requirement)
	err = r.db.NewSelect().Model(value).Table("workspace_requirements").
		Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0", id, tenantID, entityID).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get requirement: %w", err)
	}
	return value, nil
}

func (r *BunRepository) Update(ctx context.Context, value *Requirement, expectedVersion int64) error {
	tenantID, entityID, principalID, err := scope(ctx)
	if err != nil {
		return err
	}
	result, err := r.db.NewUpdate().Model((*Requirement)(nil)).Table("workspace_requirements").
		Set("title = ?", value.Title).Set("description = ?", value.Description).
		Set("acceptance_criteria = ?", value.AcceptanceCriteria).Set("status = ?", value.Status).
		Set("owner_id = ?", value.OwnerID).Set("updated_at = ?", time.Now().UTC().UnixMilli()).
		Set("updated_by = ?", principalID).Set("version = version + 1").
		Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0 AND version = ?", value.ID, tenantID, entityID, expectedVersion).Exec(ctx)
	if err != nil {
		return fmt.Errorf("update requirement: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrVersionConflict
	}
	value.Version = expectedVersion + 1
	return nil
}

func (r *BunRepository) Delete(ctx context.Context, id guid.ID, version int64) error {
	tenantID, entityID, principalID, err := scope(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	result, err := r.db.NewUpdate().Model((*Requirement)(nil)).Table("workspace_requirements").
		Set("deleted_at = ?", now).Set("deleted_by = ?", principalID).Set("updated_at = ?", now).
		Set("updated_by = ?", principalID).Set("version = version + 1").
		Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0 AND version = ?", id, tenantID, entityID, version).Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete requirement: %w", err)
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
