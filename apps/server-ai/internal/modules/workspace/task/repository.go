package task

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
	Create(context.Context, *Task) error
	List(context.Context, Status, *guid.ID) ([]Task, error)
	Get(context.Context, guid.ID) (*Task, error)
	Update(context.Context, *Task, int64) error
	Delete(context.Context, guid.ID, int64) error
}

type BunRepository struct {
	db     *bun.DB
	nextID func() (guid.ID, error)
}

func NewRepository(db *bun.DB, nextID func() (guid.ID, error)) *BunRepository {
	if db == nil || nextID == nil {
		panic("task repository requires database and id generator")
	}
	return &BunRepository{db: db, nextID: nextID}
}
func claims(ctx context.Context) (guid.ID, guid.ID, guid.ID, error) {
	t, e, p := authn.TenantIDFromContext(ctx), authn.EntityIDFromContext(ctx), authn.PrincipalIDFromContext(ctx)
	if t.Zero() || e.Zero() || p.Zero() {
		return 0, 0, 0, errors.New("task requires verified IAM claims")
	}
	return t, e, p, nil
}
func (r *BunRepository) Create(ctx context.Context, v *Task) error {
	t, e, p, err := claims(ctx)
	if err != nil {
		return err
	}
	v.ID, err = r.nextID()
	if err != nil {
		return fmt.Errorf("generate task id: %w", err)
	}
	now := time.Now().UTC().UnixMilli()
	v.TenantID, v.EntityID, v.OwnerID = t, e, p
	v.CreatedAt, v.UpdatedAt = now, now
	v.CreatedBy, v.UpdatedBy, v.Version = p, p, 1
	_, err = r.db.NewInsert().Model(v).Exec(ctx)
	if err != nil {
		return fmt.Errorf("create task: %w", err)
	}
	return nil
}
func (r *BunRepository) List(ctx context.Context, s Status, requirementID *guid.ID) ([]Task, error) {
	t, e, _, err := claims(ctx)
	if err != nil {
		return nil, err
	}
	out := []Task{}
	q := r.db.NewSelect().Model(&out).Where("tenant_id = ? AND entity_id = ? AND deleted_at = 0", t, e)
	if s != "" {
		q = q.Where("status = ?", s)
	}
	if requirementID != nil {
		q = q.Where("requirement_id = ?", *requirementID)
	}
	if err := q.Order("updated_at DESC", "id DESC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	return out, nil
}
func (r *BunRepository) Get(ctx context.Context, id guid.ID) (*Task, error) {
	t, e, _, err := claims(ctx)
	if err != nil {
		return nil, err
	}
	v := new(Task)
	err = r.db.NewSelect().Model(v).Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0", id, t, e).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get task: %w", err)
	}
	return v, nil
}

func (r *BunRepository) Exists(ctx context.Context, id guid.ID) error {
	_, err := r.Get(ctx, id)
	return err
}
func (r *BunRepository) Update(ctx context.Context, v *Task, version int64) error {
	t, e, p, err := claims(ctx)
	if err != nil {
		return err
	}
	res, err := r.db.NewUpdate().Model((*Task)(nil)).Set("title = ?", v.Title).Set("description = ?", v.Description).Set("status = ?", v.Status).Set("estimate_ms = ?", v.EstimateMS).Set("spent_ms = ?", v.SpentMS).Set("progress = ?", v.Progress).Set("workflow_run_id = ?", v.WorkflowRunID).Set("workflow_id = ?", v.WorkflowID).Set("project_id = ?", v.ProjectID).Set("workspace = ?", v.Workspace).Set("assignee_id = ?", v.AssigneeID).Set("owner_id = ?", v.OwnerID).Set("updated_at = ?", time.Now().UTC().UnixMilli()).Set("updated_by = ?", p).Set("version = version + 1").Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0 AND version = ?", v.ID, t, e, version).Exec(ctx)
	if err != nil {
		return fmt.Errorf("update task: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrVersionConflict
	}
	v.Version = version + 1
	return nil
}
func (r *BunRepository) Delete(ctx context.Context, id guid.ID, version int64) error {
	t, e, p, err := claims(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	res, err := r.db.NewUpdate().Model((*Task)(nil)).Set("deleted_at = ?", now).Set("deleted_by = ?", p).Set("updated_at = ?", now).Set("updated_by = ?", p).Set("version = version + 1").Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0 AND version = ?", id, t, e, version).Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete task: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrVersionConflict
	}
	return nil
}
