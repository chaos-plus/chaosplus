package testrun

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
	Create(context.Context, *TestRun) error
	List(context.Context, *guid.ID, Status) ([]TestRun, error)
	Get(context.Context, guid.ID) (*TestRun, error)
	Update(context.Context, *TestRun, int64) error
}
type BunRepository struct {
	db     *bun.DB
	nextID func() (guid.ID, error)
}

func NewRepository(db *bun.DB, nextID func() (guid.ID, error)) *BunRepository {
	if db == nil || nextID == nil {
		panic("test run repository requires database and id generator")
	}
	return &BunRepository{db: db, nextID: nextID}
}
func scope(ctx context.Context) (guid.ID, guid.ID, guid.ID, error) {
	t, e, p := authn.TenantIDFromContext(ctx), authn.EntityIDFromContext(ctx), authn.PrincipalIDFromContext(ctx)
	if t.Zero() || e.Zero() || p.Zero() {
		return 0, 0, 0, errors.New("test run requires verified IAM claims")
	}
	return t, e, p, nil
}
func (r *BunRepository) Create(ctx context.Context, value *TestRun) error {
	tenantID, entityID, principalID, err := scope(ctx)
	if err != nil {
		return err
	}
	value.ID, err = r.nextID()
	if err != nil {
		return fmt.Errorf("generate test run id: %w", err)
	}
	now := time.Now().UTC().UnixMilli()
	value.TenantID, value.EntityID, value.OwnerID, value.ExecutorID = tenantID, entityID, principalID, principalID
	value.CreatedAt, value.UpdatedAt, value.CreatedBy, value.UpdatedBy, value.Version = now, now, principalID, principalID, 1
	if err := validate(value); err != nil {
		return err
	}
	if _, err := r.db.NewInsert().Model(value).Exec(ctx); err != nil {
		return fmt.Errorf("create test run: %w", err)
	}
	return nil
}
func (r *BunRepository) List(ctx context.Context, testCaseID *guid.ID, status Status) ([]TestRun, error) {
	tenantID, entityID, _, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	values := []TestRun{}
	query := r.db.NewSelect().Model(&values).Where("tenant_id = ? AND entity_id = ?", tenantID, entityID)
	if testCaseID != nil {
		query = query.Where("test_case_id = ?", *testCaseID)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if err := query.Order("created_at DESC", "id DESC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list test runs: %w", err)
	}
	return values, nil
}
func (r *BunRepository) Get(ctx context.Context, id guid.ID) (*TestRun, error) {
	tenantID, entityID, _, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	value := new(TestRun)
	err = r.db.NewSelect().Model(value).Where("id = ? AND tenant_id = ? AND entity_id = ?", id, tenantID, entityID).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get test run: %w", err)
	}
	return value, nil
}
func (r *BunRepository) Update(ctx context.Context, value *TestRun, version int64) error {
	tenantID, entityID, principalID, err := scope(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	result, err := r.db.NewUpdate().Model((*TestRun)(nil)).Set("status = ?", value.Status).Set("started_at = ?", value.StartedAt).Set("completed_at = ?", value.CompletedAt).Set("observed_result = ?", value.ObservedResult).Set("failure_summary = ?", value.FailureSummary).Set("updated_at = ?", now).Set("updated_by = ?", principalID).Set("version = version + 1").Where("id = ? AND tenant_id = ? AND entity_id = ? AND version = ? AND status IN ('queued','running')", value.ID, tenantID, entityID, version).Exec(ctx)
	if err != nil {
		return fmt.Errorf("update test run: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrVersionConflict
	}
	value.UpdatedAt, value.UpdatedBy, value.Version = now, principalID, version+1
	return nil
}
func (r *BunRepository) Exists(ctx context.Context, id guid.ID) error {
	_, err := r.Get(ctx, id)
	return err
}
