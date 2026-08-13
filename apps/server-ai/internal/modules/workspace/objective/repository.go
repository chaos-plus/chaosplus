package objective

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

type Repository interface {
	Create(context.Context, *Objective, []KeyResultInput) error
	List(context.Context, Status) ([]Objective, error)
	Get(context.Context, guid.ID) (*Objective, error)
	Update(context.Context, *Objective, []KeyResultInput, int64) error
	Delete(context.Context, guid.ID, int64) error
}

func (r *BunRepository) KeyResultsExist(ctx context.Context, ids []guid.ID) error {
	if len(ids) == 0 {
		return nil
	}
	tenantID, entityID, _, err := scope(ctx)
	if err != nil {
		return err
	}
	unique := make(map[guid.ID]struct{}, len(ids))
	for _, id := range ids {
		if id.Zero() {
			return ErrInvalid
		}
		unique[id] = struct{}{}
	}
	values := make([]guid.ID, 0, len(unique))
	for id := range unique {
		values = append(values, id)
	}
	count, err := r.db.NewSelect().Model((*KeyResult)(nil)).
		Where("tenant_id = ? AND entity_id = ? AND deleted_at = 0", tenantID, entityID).
		Where("id IN (?)", bun.List(values)).Count(ctx)
	if err != nil {
		return fmt.Errorf("validate key result references: %w", err)
	}
	if count != len(values) {
		return ErrNotFound
	}
	return nil
}

type BunRepository struct {
	db     *bun.DB
	nextID func() (guid.ID, error)
}

func NewRepository(db *bun.DB, nextID func() (guid.ID, error)) *BunRepository {
	if db == nil || nextID == nil {
		panic("objective repository requires database and id generator")
	}
	return &BunRepository{db: db, nextID: nextID}
}

func scope(ctx context.Context) (guid.ID, guid.ID, guid.ID, error) {
	tenantID := authn.TenantIDFromContext(ctx)
	entityID := authn.EntityIDFromContext(ctx)
	principalID := authn.PrincipalIDFromContext(ctx)
	if tenantID.Zero() || entityID.Zero() || principalID.Zero() {
		return 0, 0, 0, errors.New("objective requires verified IAM claims")
	}
	return tenantID, entityID, principalID, nil
}

func (r *BunRepository) Create(ctx context.Context, value *Objective, inputs []KeyResultInput) error {
	tenantID, entityID, principalID, err := scope(ctx)
	if err != nil {
		return err
	}
	value.ID, err = r.nextID()
	if err != nil {
		return fmt.Errorf("generate objective id: %w", err)
	}
	now := time.Now().UTC().UnixMilli()
	value.TenantID, value.EntityID, value.OwnerID = tenantID, entityID, principalID
	value.CreatedAt, value.UpdatedAt = now, now
	value.CreatedBy, value.UpdatedBy, value.Version = principalID, principalID, 1
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(value).Exec(ctx); err != nil {
			return fmt.Errorf("create objective: %w", err)
		}
		results, err := r.insertKeyResults(ctx, tx, *value, inputs, now, principalID)
		if err != nil {
			return err
		}
		value.KeyResults = results
		return nil
	})
}

func (r *BunRepository) insertKeyResults(ctx context.Context, tx bun.IDB, objective Objective, inputs []KeyResultInput, now int64, principalID guid.ID) ([]KeyResult, error) {
	results := make([]KeyResult, 0, len(inputs))
	for _, input := range inputs {
		id, err := r.nextID()
		if err != nil {
			return nil, fmt.Errorf("generate key result id: %w", err)
		}
		result := KeyResult{
			ID: id, TenantID: objective.TenantID, EntityID: objective.EntityID, OwnerID: objective.OwnerID,
			ObjectiveID: objective.ID, Title: strings.TrimSpace(input.Title), TargetValue: input.TargetValue,
			CurrentValue: input.CurrentValue, Unit: strings.TrimSpace(input.Unit), CreatedAt: now,
			CreatedBy: principalID, UpdatedAt: now, UpdatedBy: principalID, Version: 1,
		}
		if _, err := tx.NewInsert().Model(&result).Exec(ctx); err != nil {
			return nil, fmt.Errorf("create key result: %w", err)
		}
		results = append(results, result)
	}
	return results, nil
}

func (r *BunRepository) List(ctx context.Context, status Status) ([]Objective, error) {
	tenantID, entityID, _, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	items := []Objective{}
	query := r.db.NewSelect().Model(&items).
		Where("tenant_id = ? AND entity_id = ? AND deleted_at = 0", tenantID, entityID)
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if err := query.Order("period_start DESC", "id DESC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list objectives: %w", err)
	}
	for i := range items {
		items[i].KeyResults, err = r.listKeyResults(ctx, r.db, tenantID, entityID, items[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (r *BunRepository) Get(ctx context.Context, id guid.ID) (*Objective, error) {
	tenantID, entityID, _, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	value := new(Objective)
	err = r.db.NewSelect().Model(value).
		Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0", id, tenantID, entityID).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get objective: %w", err)
	}
	value.KeyResults, err = r.listKeyResults(ctx, r.db, tenantID, entityID, id)
	if err != nil {
		return nil, err
	}
	return value, nil
}

func (r *BunRepository) listKeyResults(ctx context.Context, db bun.IDB, tenantID, entityID, objectiveID guid.ID) ([]KeyResult, error) {
	results := []KeyResult{}
	err := db.NewSelect().Model(&results).
		Where("tenant_id = ? AND entity_id = ? AND objective_id = ? AND deleted_at = 0", tenantID, entityID, objectiveID).
		Order("id ASC").Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("list key results: %w", err)
	}
	return results, nil
}

func (r *BunRepository) Update(ctx context.Context, value *Objective, inputs []KeyResultInput, version int64) error {
	tenantID, entityID, principalID, err := scope(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		result, err := tx.NewUpdate().Model((*Objective)(nil)).
			Set("owner_id = ?", value.OwnerID).Set("title = ?", value.Title).Set("description = ?", value.Description).
			Set("period_start = ?", value.PeriodStart).Set("period_end = ?", value.PeriodEnd).Set("status = ?", value.Status).
			Set("updated_at = ?", now).Set("updated_by = ?", principalID).Set("version = version + 1").
			Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0 AND version = ?", value.ID, tenantID, entityID, version).Exec(ctx)
		if err != nil {
			return fmt.Errorf("update objective: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return ErrVersionConflict
		}
		existing := make(map[guid.ID]KeyResult, len(value.KeyResults))
		for _, result := range value.KeyResults {
			existing[result.ID] = result
		}
		kept := make(map[guid.ID]struct{}, len(inputs))
		results := make([]KeyResult, 0, len(inputs))
		for _, input := range inputs {
			if input.ID == nil {
				value.TenantID, value.EntityID = tenantID, entityID
				created, err := r.insertKeyResults(ctx, tx, *value, []KeyResultInput{input}, now, principalID)
				if err != nil {
					return err
				}
				results = append(results, created[0])
				continue
			}
			current, ok := existing[*input.ID]
			if !ok {
				return ErrInvalid
			}
			updated, err := tx.NewUpdate().Model((*KeyResult)(nil)).
				Set("owner_id = ?", value.OwnerID).Set("title = ?", strings.TrimSpace(input.Title)).
				Set("target_value = ?", input.TargetValue).Set("current_value = ?", input.CurrentValue).
				Set("unit = ?", strings.TrimSpace(input.Unit)).Set("updated_at = ?", now).
				Set("updated_by = ?", principalID).Set("version = version + 1").
				Where("id = ? AND tenant_id = ? AND entity_id = ? AND objective_id = ? AND deleted_at = 0 AND version = ?", current.ID, tenantID, entityID, value.ID, current.Version).Exec(ctx)
			if err != nil {
				return fmt.Errorf("update key result: %w", err)
			}
			rows, err := updated.RowsAffected()
			if err != nil {
				return err
			}
			if rows == 0 {
				return ErrVersionConflict
			}
			current.OwnerID, current.Title = value.OwnerID, strings.TrimSpace(input.Title)
			current.TargetValue, current.CurrentValue, current.Unit = input.TargetValue, input.CurrentValue, strings.TrimSpace(input.Unit)
			current.UpdatedAt, current.UpdatedBy, current.Version = now, principalID, current.Version+1
			kept[current.ID] = struct{}{}
			results = append(results, current)
		}
		for id := range existing {
			if _, ok := kept[id]; ok {
				continue
			}
			if _, err := tx.NewUpdate().Model((*KeyResult)(nil)).
				Set("deleted_at = ?", now).Set("deleted_by = ?", principalID).Set("updated_at = ?", now).
				Set("updated_by = ?", principalID).Set("version = version + 1").
				Where("id = ? AND tenant_id = ? AND entity_id = ? AND objective_id = ? AND deleted_at = 0", id, tenantID, entityID, value.ID).Exec(ctx); err != nil {
				return fmt.Errorf("retire key result: %w", err)
			}
		}
		value.KeyResults, value.UpdatedAt, value.UpdatedBy, value.Version = results, now, principalID, version+1
		return nil
	})
}

func (r *BunRepository) Delete(ctx context.Context, id guid.ID, version int64) error {
	tenantID, entityID, principalID, err := scope(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		result, err := tx.NewUpdate().Model((*Objective)(nil)).
			Set("deleted_at = ?", now).Set("deleted_by = ?", principalID).Set("updated_at = ?", now).
			Set("updated_by = ?", principalID).Set("version = version + 1").
			Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0 AND version = ?", id, tenantID, entityID, version).Exec(ctx)
		if err != nil {
			return fmt.Errorf("delete objective: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return ErrVersionConflict
		}
		_, err = tx.NewUpdate().Model((*KeyResult)(nil)).
			Set("deleted_at = ?", now).Set("deleted_by = ?", principalID).Set("updated_at = ?", now).
			Set("updated_by = ?", principalID).Set("version = version + 1").
			Where("tenant_id = ? AND entity_id = ? AND objective_id = ? AND deleted_at = 0", tenantID, entityID, id).Exec(ctx)
		return err
	})
}
