package testcase

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
	Create(context.Context, *TestCase, []StepInput) error
	List(context.Context, Status, *guid.ID) ([]TestCase, error)
	Get(context.Context, guid.ID) (*TestCase, error)
	Update(context.Context, *TestCase, []StepInput, int64) error
	Delete(context.Context, guid.ID, int64) error
}

type BunRepository struct {
	db     *bun.DB
	nextID func() (guid.ID, error)
}

func NewRepository(db *bun.DB, nextID func() (guid.ID, error)) *BunRepository {
	if db == nil || nextID == nil {
		panic("test case repository requires database and id generator")
	}
	return &BunRepository{db: db, nextID: nextID}
}

func scope(ctx context.Context) (guid.ID, guid.ID, guid.ID, error) {
	tenantID, entityID, principalID := authn.TenantIDFromContext(ctx), authn.EntityIDFromContext(ctx), authn.PrincipalIDFromContext(ctx)
	if tenantID.Zero() || entityID.Zero() || principalID.Zero() {
		return 0, 0, 0, errors.New("test case requires verified IAM claims")
	}
	return tenantID, entityID, principalID, nil
}

func (r *BunRepository) Create(ctx context.Context, value *TestCase, steps []StepInput) error {
	tenantID, entityID, principalID, err := scope(ctx)
	if err != nil {
		return err
	}
	value.ID, err = r.nextID()
	if err != nil {
		return fmt.Errorf("generate test case id: %w", err)
	}
	now := time.Now().UTC().UnixMilli()
	value.TenantID, value.EntityID, value.OwnerID = tenantID, entityID, principalID
	value.CreatedAt, value.UpdatedAt, value.CreatedBy, value.UpdatedBy, value.Version = now, now, principalID, principalID, 1
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(value).Exec(ctx); err != nil {
			return fmt.Errorf("create test case: %w", err)
		}
		created, err := r.insertSteps(ctx, tx, value, steps, now, principalID)
		value.Steps = created
		return err
	})
}

func (r *BunRepository) insertSteps(ctx context.Context, db bun.IDB, value *TestCase, inputs []StepInput, now int64, principalID guid.ID) ([]Step, error) {
	steps := make([]Step, 0, len(inputs))
	for i, input := range inputs {
		id, err := r.nextID()
		if err != nil {
			return nil, fmt.Errorf("generate test step id: %w", err)
		}
		steps = append(steps, Step{ID: id, TenantID: value.TenantID, EntityID: value.EntityID, TestCaseID: value.ID, Position: i + 1, Action: strings.TrimSpace(input.Action), ExpectedResult: strings.TrimSpace(input.ExpectedResult), CreatedAt: now, CreatedBy: principalID, UpdatedAt: now, UpdatedBy: principalID, Version: 1})
	}
	if _, err := db.NewInsert().Model(&steps).Exec(ctx); err != nil {
		return nil, fmt.Errorf("create test steps: %w", err)
	}
	return steps, nil
}

func (r *BunRepository) List(ctx context.Context, status Status, requirementID *guid.ID) ([]TestCase, error) {
	tenantID, entityID, _, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	items := []TestCase{}
	query := r.db.NewSelect().Model(&items).Where("tenant_id = ? AND entity_id = ? AND deleted_at = 0", tenantID, entityID)
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if requirementID != nil {
		query = query.Where("requirement_id = ?", *requirementID)
	}
	if err := query.Order("updated_at DESC", "id DESC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list test cases: %w", err)
	}
	for i := range items {
		items[i].Steps, err = r.listSteps(ctx, r.db, tenantID, entityID, items[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (r *BunRepository) Get(ctx context.Context, id guid.ID) (*TestCase, error) {
	tenantID, entityID, _, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	value := new(TestCase)
	err = r.db.NewSelect().Model(value).Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0", id, tenantID, entityID).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get test case: %w", err)
	}
	value.Steps, err = r.listSteps(ctx, r.db, tenantID, entityID, id)
	return value, err
}

func (r *BunRepository) listSteps(ctx context.Context, db bun.IDB, tenantID, entityID, testCaseID guid.ID) ([]Step, error) {
	steps := []Step{}
	if err := db.NewSelect().Model(&steps).Where("tenant_id = ? AND entity_id = ? AND test_case_id = ? AND deleted_at = 0", tenantID, entityID, testCaseID).Order("position ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list test steps: %w", err)
	}
	return steps, nil
}

func (r *BunRepository) Update(ctx context.Context, value *TestCase, steps []StepInput, version int64) error {
	tenantID, entityID, principalID, err := scope(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	err = r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		result, err := tx.NewUpdate().Model((*TestCase)(nil)).Set("owner_id = ?", value.OwnerID).Set("title = ?", value.Title).Set("description = ?", value.Description).Set("preconditions = ?", value.Preconditions).Set("priority = ?", value.Priority).Set("status = ?", value.Status).Set("assignee_id = ?", value.AssigneeID).Set("updated_at = ?", now).Set("updated_by = ?", principalID).Set("version = version + 1").Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0 AND version = ?", value.ID, tenantID, entityID, version).Exec(ctx)
		if err != nil {
			return fmt.Errorf("update test case: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return ErrVersionConflict
		}
		if _, err := tx.NewUpdate().Model((*Step)(nil)).Set("deleted_at = ?", now).Set("deleted_by = ?", principalID).Set("updated_at = ?", now).Set("updated_by = ?", principalID).Set("version = version + 1").Where("tenant_id = ? AND entity_id = ? AND test_case_id = ? AND deleted_at = 0", tenantID, entityID, value.ID).Exec(ctx); err != nil {
			return fmt.Errorf("retire test steps: %w", err)
		}
		value.TenantID, value.EntityID = tenantID, entityID
		created, err := r.insertSteps(ctx, tx, value, steps, now, principalID)
		value.Steps = created
		return err
	})
	if err == nil {
		value.UpdatedAt, value.UpdatedBy, value.Version = now, principalID, version+1
	}
	return err
}

func (r *BunRepository) Delete(ctx context.Context, id guid.ID, version int64) error {
	tenantID, entityID, principalID, err := scope(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		result, err := tx.NewUpdate().Model((*TestCase)(nil)).Set("deleted_at = ?", now).Set("deleted_by = ?", principalID).Set("updated_at = ?", now).Set("updated_by = ?", principalID).Set("version = version + 1").Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0 AND version = ?", id, tenantID, entityID, version).Exec(ctx)
		if err != nil {
			return fmt.Errorf("delete test case: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return ErrVersionConflict
		}
		_, err = tx.NewUpdate().Model((*Step)(nil)).Set("deleted_at = ?", now).Set("deleted_by = ?", principalID).Set("updated_at = ?", now).Set("updated_by = ?", principalID).Set("version = version + 1").Where("tenant_id = ? AND entity_id = ? AND test_case_id = ? AND deleted_at = 0", tenantID, entityID, id).Exec(ctx)
		return err
	})
}

func (r *BunRepository) Exists(ctx context.Context, id guid.ID) error {
	_, err := r.Get(ctx, id)
	return err
}
