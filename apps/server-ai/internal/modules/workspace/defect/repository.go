package defect

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
	"time"
)

type Repository interface {
	Create(context.Context, *Defect) error
	List(context.Context, Status, Severity) ([]Defect, error)
	Get(context.Context, guid.ID) (*Defect, error)
	Update(context.Context, *Defect, int64) error
	Delete(context.Context, guid.ID, int64) error
}
type BunRepository struct {
	db     *bun.DB
	nextID func() (guid.ID, error)
}

func NewRepository(db *bun.DB, nextID func() (guid.ID, error)) *BunRepository {
	if db == nil || nextID == nil {
		panic("defect repository requires database and id generator")
	}
	return &BunRepository{db: db, nextID: nextID}
}
func scope(ctx context.Context) (guid.ID, guid.ID, guid.ID, error) {
	t, e, p := authn.TenantIDFromContext(ctx), authn.EntityIDFromContext(ctx), authn.PrincipalIDFromContext(ctx)
	if t.Zero() || e.Zero() || p.Zero() {
		return 0, 0, 0, errors.New("defect requires verified IAM claims")
	}
	return t, e, p, nil
}
func (r *BunRepository) Create(ctx context.Context, value *Defect) error {
	t, e, p, err := scope(ctx)
	if err != nil {
		return err
	}
	value.ID, err = r.nextID()
	if err != nil {
		return fmt.Errorf("generate defect id: %w", err)
	}
	now := time.Now().UTC().UnixMilli()
	value.TenantID, value.EntityID, value.OwnerID = t, e, p
	value.CreatedAt, value.UpdatedAt, value.CreatedBy, value.UpdatedBy, value.Version = now, now, p, p, 1
	if _, err := r.db.NewInsert().Model(value).Exec(ctx); err != nil {
		return fmt.Errorf("create defect: %w", err)
	}
	return nil
}
func (r *BunRepository) List(ctx context.Context, status Status, severity Severity) ([]Defect, error) {
	t, e, _, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	values := []Defect{}
	q := r.db.NewSelect().Model(&values).Where("tenant_id=? AND entity_id=? AND deleted_at=0", t, e)
	if status != "" {
		q = q.Where("status=?", status)
	}
	if severity != "" {
		q = q.Where("severity=?", severity)
	}
	if err := q.Order("updated_at DESC", "id DESC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list defects: %w", err)
	}
	return values, nil
}
func (r *BunRepository) Get(ctx context.Context, id guid.ID) (*Defect, error) {
	t, e, _, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	value := new(Defect)
	err = r.db.NewSelect().Model(value).Where("id=? AND tenant_id=? AND entity_id=? AND deleted_at=0", id, t, e).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get defect: %w", err)
	}
	return value, nil
}
func (r *BunRepository) Update(ctx context.Context, value *Defect, version int64) error {
	t, e, p, err := scope(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	result, err := r.db.NewUpdate().Model((*Defect)(nil)).Set("owner_id=?", value.OwnerID).Set("title=?", value.Title).Set("description=?", value.Description).Set("reproduction_steps=?", value.ReproductionSteps).Set("expected_result=?", value.ExpectedResult).Set("actual_result=?", value.ActualResult).Set("severity=?", value.Severity).Set("priority=?", value.Priority).Set("status=?", value.Status).Set("resolution=?", value.Resolution).Set("resolution_note=?", value.ResolutionNote).Set("assignee_id=?", value.AssigneeID).Set("updated_at=?", now).Set("updated_by=?", p).Set("version=version+1").Where("id=? AND tenant_id=? AND entity_id=? AND deleted_at=0 AND version=?", value.ID, t, e, version).Exec(ctx)
	if err != nil {
		return fmt.Errorf("update defect: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrVersionConflict
	}
	value.UpdatedAt, value.UpdatedBy, value.Version = now, p, version+1
	return nil
}
func (r *BunRepository) Delete(ctx context.Context, id guid.ID, version int64) error {
	t, e, p, err := scope(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	result, err := r.db.NewUpdate().Model((*Defect)(nil)).Set("deleted_at=?", now).Set("deleted_by=?", p).Set("updated_at=?", now).Set("updated_by=?", p).Set("version=version+1").Where("id=? AND tenant_id=? AND entity_id=? AND deleted_at=0 AND version=?", id, t, e, version).Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete defect: %w", err)
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
