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

func (r *BunRepository) KeyResultsInUse(ctx context.Context, ids []guid.ID) (bool, error) {
	if len(ids) == 0 {
		return false, nil
	}
	tenantID, entityID, _, err := scope(ctx)
	if err != nil {
		return false, err
	}
	count, err := r.db.NewSelect().Model((*keyResultLink)(nil)).ModelTableExpr("workspace_requirement_key_results AS link").
		Join("JOIN workspace_requirements AS requirement ON requirement.id = link.requirement_id AND requirement.tenant_id = link.tenant_id AND requirement.entity_id = link.entity_id AND requirement.deleted_at = 0").
		Where("link.tenant_id = ? AND link.entity_id = ? AND link.key_result_id IN (?)", tenantID, entityID, bun.List(ids)).Count(ctx)
	if err != nil {
		return false, fmt.Errorf("check key result usage: %w", err)
	}
	return count > 0, nil
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
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(value).Exec(ctx); err != nil {
			return fmt.Errorf("create requirement: %w", err)
		}
		return replaceKeyResults(ctx, tx, value, principalID)
	})
}

func (r *BunRepository) List(ctx context.Context, status Status, parentID *guid.ID) ([]Requirement, error) {
	tenantID, entityID, _, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	items := []Requirement{}
	query := r.db.NewSelect().Model(&items).
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
	for i := range items {
		items[i].KeyResultIDs, err = keyResultIDs(ctx, r.db, tenantID, entityID, items[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (r *BunRepository) Get(ctx context.Context, id guid.ID) (*Requirement, error) {
	tenantID, entityID, _, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	value := new(Requirement)
	err = r.db.NewSelect().Model(value).
		Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0", id, tenantID, entityID).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get requirement: %w", err)
	}
	value.KeyResultIDs, err = keyResultIDs(ctx, r.db, tenantID, entityID, id)
	if err != nil {
		return nil, err
	}
	return value, nil
}

func (r *BunRepository) Update(ctx context.Context, value *Requirement, expectedVersion int64) error {
	tenantID, entityID, principalID, err := scope(ctx)
	if err != nil {
		return err
	}
	err = r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		result, err := tx.NewUpdate().Model((*Requirement)(nil)).
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
		if err := replaceKeyResults(ctx, tx, value, principalID); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	value.Version = expectedVersion + 1
	return nil
}

func replaceKeyResults(ctx context.Context, db bun.IDB, value *Requirement, principalID guid.ID) error {
	if _, err := db.NewDelete().Model((*keyResultLink)(nil)).Where("tenant_id = ? AND entity_id = ? AND requirement_id = ?", value.TenantID, value.EntityID, value.ID).Exec(ctx); err != nil {
		return fmt.Errorf("replace requirement key results: %w", err)
	}
	seen := make(map[guid.ID]struct{}, len(value.KeyResultIDs))
	links := make([]keyResultLink, 0, len(value.KeyResultIDs))
	now := time.Now().UTC().UnixMilli()
	for _, id := range value.KeyResultIDs {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		links = append(links, keyResultLink{TenantID: value.TenantID, EntityID: value.EntityID, RequirementID: value.ID, KeyResultID: id, CreatedAt: now, CreatedBy: principalID})
	}
	if len(links) == 0 {
		return nil
	}
	if _, err := db.NewInsert().Model(&links).Exec(ctx); err != nil {
		return fmt.Errorf("create requirement key results: %w", err)
	}
	return nil
}

func keyResultIDs(ctx context.Context, db bun.IDB, tenantID, entityID, requirementID guid.ID) ([]guid.ID, error) {
	links := []keyResultLink{}
	if err := db.NewSelect().Model(&links).Column("key_result_id").Where("tenant_id = ? AND entity_id = ? AND requirement_id = ?", tenantID, entityID, requirementID).Order("key_result_id ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list requirement key results: %w", err)
	}
	ids := make([]guid.ID, len(links))
	for i := range links {
		ids[i] = links[i].KeyResultID
	}
	return ids, nil
}

func (r *BunRepository) Exists(ctx context.Context, id guid.ID) error {
	_, err := r.Get(ctx, id)
	return err
}

func (r *BunRepository) Delete(ctx context.Context, id guid.ID, version int64) error {
	tenantID, entityID, principalID, err := scope(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		result, err := tx.NewUpdate().Model((*Requirement)(nil)).
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
		if _, err := tx.NewDelete().Model((*keyResultLink)(nil)).
			Where("tenant_id = ? AND entity_id = ? AND requirement_id = ?", tenantID, entityID, id).Exec(ctx); err != nil {
			return fmt.Errorf("delete requirement key results: %w", err)
		}
		return nil
	})
}
