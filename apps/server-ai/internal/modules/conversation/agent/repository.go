package agent

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
	Create(context.Context, *Agent) error
	List(context.Context) ([]Agent, error)
	Get(context.Context, guid.ID) (*Agent, error)
	Update(context.Context, *Agent, int64) error
	SetLifecycle(context.Context, guid.ID, Status, string, int64) (*Agent, error)
	Delete(context.Context, guid.ID, int64) error
	ListByMachine(context.Context, guid.ID) ([]Agent, error)
	CountByMachine(context.Context) (map[guid.ID]int, error)
}

type BunRepository struct {
	db     *bun.DB
	nextID func() (guid.ID, error)
}

func NewRepository(db *bun.DB, nextID func() (guid.ID, error)) *BunRepository {
	if db == nil || nextID == nil {
		panic("agent repository requires database and id generator")
	}
	return &BunRepository{db: db, nextID: nextID}
}

func scope(ctx context.Context) (guid.ID, guid.ID, guid.ID, error) {
	tenantID, entityID, principalID := authn.TenantIDFromContext(ctx), authn.EntityIDFromContext(ctx), authn.PrincipalIDFromContext(ctx)
	if tenantID.Zero() || entityID.Zero() || principalID.Zero() {
		return 0, 0, 0, errors.New("agent requires verified IAM claims")
	}
	return tenantID, entityID, principalID, nil
}

func (r *BunRepository) Create(ctx context.Context, value *Agent) error {
	tenantID, entityID, principalID, err := scope(ctx)
	if err != nil {
		return err
	}
	value.ID, err = r.nextID()
	if err != nil {
		return fmt.Errorf("generate agent id: %w", err)
	}
	now := time.Now().UTC().UnixMilli()
	value.TenantID, value.EntityID, value.OwnerID = tenantID, entityID, principalID
	value.CreatedAt, value.UpdatedAt, value.CreatedBy, value.UpdatedBy, value.Version = now, now, principalID, principalID, 1
	if _, err := r.db.NewInsert().Model(value).Exec(ctx); err != nil {
		return fmt.Errorf("create agent: %w", err)
	}
	return nil
}

func (r *BunRepository) List(ctx context.Context) ([]Agent, error) {
	tenantID, entityID, _, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	items := []Agent{}
	if err := r.db.NewSelect().Model(&items).
		Where("tenant_id = ? AND entity_id = ? AND deleted_at = 0", tenantID, entityID).
		Order("created_at ASC", "id ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	return items, nil
}

func (r *BunRepository) Get(ctx context.Context, id guid.ID) (*Agent, error) {
	tenantID, entityID, _, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	value := new(Agent)
	err = r.db.NewSelect().Model(value).
		Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0", id, tenantID, entityID).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}
	return value, nil
}

func (r *BunRepository) Update(ctx context.Context, value *Agent, version int64) error {
	tenantID, entityID, principalID, err := scope(ctx)
	if err != nil {
		return err
	}
	result, err := r.db.NewUpdate().Model((*Agent)(nil)).
		Set("name = ?", value.Name).Set("kind = ?", value.Kind).Set("runtime = ?", value.Runtime).
		Set("model = ?", value.Model).Set("provider = ?", value.Provider).Set("system_prompt = ?", value.SystemPrompt).
		Set("description = ?", value.Description).Set("machine_id = ?", value.MachineID).
		Set("updated_at = ?", time.Now().UTC().UnixMilli()).Set("updated_by = ?", principalID).Set("version = version + 1").
		Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0 AND version = ?", value.ID, tenantID, entityID, version).Exec(ctx)
	if err != nil {
		return fmt.Errorf("update agent: %w", err)
	}
	return requireChanged(result, value, version)
}

func (r *BunRepository) SetLifecycle(ctx context.Context, id guid.ID, status Status, handover string, version int64) (*Agent, error) {
	tenantID, entityID, principalID, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	now, retiredAt := time.Now().UTC().UnixMilli(), int64(0)
	if status == StatusRetired {
		retiredAt = now
	}
	result, err := r.db.NewUpdate().Model((*Agent)(nil)).Set("status = ?", status).
		Set("handover_doc = ?", handover).Set("retired_at = ?", retiredAt).
		Set("updated_at = ?", now).Set("updated_by = ?", principalID).Set("version = version + 1").
		Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0 AND version = ?", id, tenantID, entityID, version).Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("update agent lifecycle: %w", err)
	}
	if err := requireChanged(result, nil, version); err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func (r *BunRepository) Delete(ctx context.Context, id guid.ID, version int64) error {
	tenantID, entityID, principalID, err := scope(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	result, err := r.db.NewUpdate().Model((*Agent)(nil)).
		Set("deleted_at = ?", now).Set("deleted_by = ?", principalID).Set("updated_at = ?", now).
		Set("updated_by = ?", principalID).Set("version = version + 1").
		Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0 AND version = ?", id, tenantID, entityID, version).Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}
	return requireChanged(result, nil, version)
}

func (r *BunRepository) ListByMachine(ctx context.Context, machineID guid.ID) ([]Agent, error) {
	tenantID, entityID, _, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	items := []Agent{}
	err = r.db.NewSelect().Model(&items).
		Where("tenant_id = ? AND entity_id = ? AND machine_id = ? AND deleted_at = 0", tenantID, entityID, machineID).
		Order("name ASC", "id ASC").Scan(ctx)
	return items, err
}

func (r *BunRepository) CountByMachine(ctx context.Context) (map[guid.ID]int, error) {
	tenantID, entityID, _, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	rows := []struct {
		MachineID guid.ID `bun:"machine_id"`
		Count     int     `bun:"count"`
	}{}
	if err := r.db.NewSelect().Table("conversation_agents").Column("machine_id").ColumnExpr("COUNT(*) AS count").
		Where("tenant_id = ? AND entity_id = ? AND status <> ? AND deleted_at = 0", tenantID, entityID, StatusRetired).
		Group("machine_id").Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("count agents by machine: %w", err)
	}
	result := make(map[guid.ID]int, len(rows))
	for _, row := range rows {
		result[row.MachineID] = row.Count
	}
	return result, nil
}

func requireChanged(result sql.Result, value *Agent, version int64) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrVersionConflict
	}
	if value != nil {
		value.Version = version + 1
	}
	return nil
}
