package machine

import (
	"context"
	"fmt"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	coreid "github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

// Repository is the persistence port owned by the machine module.
type Repository interface {
	Ping(context.Context) error
	Upsert(context.Context, Machine) error
	UpdateEntity(context.Context, coreid.ID, coreid.ID) error
	List(context.Context) ([]Machine, error)
	ListAll(context.Context) ([]Machine, error)
	UpdateAddress(context.Context, coreid.ID, string) error
	TouchHeartbeat(context.Context, coreid.ID) error
	UpdateToken(context.Context, coreid.ID, string) error
	Delete(context.Context, coreid.ID) error
}

type BunRepository struct{ db *bun.DB }

func NewRepository(db *bun.DB) *BunRepository {
	if db == nil {
		panic("machine repository requires database")
	}
	return &BunRepository{db: db}
}

func (r *BunRepository) Ping(ctx context.Context) error { return r.db.PingContext(ctx) }

func (r *BunRepository) Upsert(ctx context.Context, machine Machine) error {
	claims, err := requireClaims(ctx)
	if err != nil {
		return err
	}
	if machine.ID.Zero() {
		return fmt.Errorf("upsert machine: id is required")
	}
	if machine.TenantID.Zero() {
		machine.TenantID = claims.TenantID
	}
	if machine.EntityID.Zero() {
		machine.EntityID = claims.EntityID
	}
	if machine.OwnerID.Zero() {
		machine.OwnerID = claims.PrincipalID
	}
	if machine.TenantID != claims.TenantID || machine.EntityID != claims.EntityID || machine.OwnerID != claims.PrincipalID {
		return fmt.Errorf("upsert machine: ownership scope does not match authenticated claims")
	}
	now := time.Now().UTC().UnixMilli()
	actor := claims.PrincipalID
	if machine.RegisteredAt == 0 {
		machine.RegisteredAt = now
	}
	if machine.CreatedAt == 0 {
		machine.CreatedAt = now
	}
	if machine.CreatedBy.Zero() {
		machine.CreatedBy = actor
	}
	machine.UpdatedAt, machine.UpdatedBy = now, actor
	if machine.Version < 1 {
		machine.Version = 1
	}
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		result, err := tx.NewUpdate().Model((*Machine)(nil)).
			Set("address = ?", machine.Address).
			Set("status = ?", machine.Status).
			Set("last_heartbeat_at = ?", machine.LastHeartbeatAt).
			Set("token_hash = ?", machine.TokenHash).
			Set("os = ?", machine.OS).
			Set("updated_at = ?", machine.UpdatedAt).
			Set("updated_by = ?", machine.UpdatedBy).
			Set("version = version + 1").
			Set("registered_at = CASE WHEN registered_at = 0 THEN ? ELSE registered_at END", machine.RegisteredAt).
			Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0", machine.ID, claims.TenantID, claims.EntityID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("update machine %s: %w", machine.ID, err)
		}
		updated, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("inspect machine update %s: %w", machine.ID, err)
		}
		if updated > 0 {
			return nil
		}
		if _, err := tx.NewInsert().Model(&machine).Exec(ctx); err != nil {
			return fmt.Errorf("insert machine %s: %w", machine.ID, err)
		}
		return nil
	})
}

func (r *BunRepository) UpdateEntity(ctx context.Context, id, entityID coreid.ID) error {
	_, err := r.auditUpdate(ctx, id).Set("entity_id = ?", entityID).Exec(ctx)
	return wrapRepositoryError("update machine entity", err)
}

func (r *BunRepository) List(ctx context.Context) ([]Machine, error) {
	claims, err := requireClaims(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]Machine, 0)
	query := r.db.NewSelect().Model(&items).
		Where("deleted_at = 0").
		Where("tenant_id = ?", claims.TenantID).
		Where("entity_id = ?", claims.EntityID)
	if err := query.Order("id ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list machines: %w", err)
	}
	return items, nil
}

// ListAll is reserved for trusted application lifecycle work such as token
// rehydration. Request handlers must use List so tenant/entity scope is mandatory.
func (r *BunRepository) ListAll(ctx context.Context) ([]Machine, error) {
	items := make([]Machine, 0)
	if err := r.db.NewSelect().Model(&items).Where("deleted_at = 0").Order("id ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list all machines: %w", err)
	}
	return items, nil
}

func (r *BunRepository) UpdateAddress(ctx context.Context, id coreid.ID, address string) error {
	_, err := r.auditUpdate(ctx, id).Set("address = ?", address).Exec(ctx)
	return wrapRepositoryError("update machine address", err)
}

func (r *BunRepository) TouchHeartbeat(ctx context.Context, id coreid.ID) error {
	_, err := r.auditUpdate(ctx, id).Set("last_heartbeat_at = ?", time.Now().UTC().UnixMilli()).Exec(ctx)
	return wrapRepositoryError("touch machine heartbeat", err)
}

func (r *BunRepository) UpdateToken(ctx context.Context, id coreid.ID, tokenHash string) error {
	_, err := r.auditUpdate(ctx, id).Set("token_hash = ?", tokenHash).Exec(ctx)
	return wrapRepositoryError("update machine token", err)
}

func (r *BunRepository) Delete(ctx context.Context, id coreid.ID) error {
	now := time.Now().UTC().UnixMilli()
	_, err := r.auditUpdate(ctx, id).
		Set("deleted_at = ?", now).
		Set("deleted_by = ?", authn.PrincipalIDFromContext(ctx)).
		Exec(ctx)
	return wrapRepositoryError("delete machine", err)
}

func (r *BunRepository) auditUpdate(ctx context.Context, id coreid.ID) *bun.UpdateQuery {
	claims, _ := authn.FromContext(ctx)
	if claims == nil {
		claims = &authn.Claims{}
	}
	return r.db.NewUpdate().Model((*Machine)(nil)).
		Set("updated_at = ?", time.Now().UTC().UnixMilli()).
		Set("updated_by = ?", authn.PrincipalIDFromContext(ctx)).
		Set("version = version + 1").
		Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0", id, claims.TenantID, claims.EntityID)
}

func requireClaims(ctx context.Context) (*authn.Claims, error) {
	claims, ok := authn.FromContext(ctx)
	if !ok || claims.TenantID.Zero() || claims.EntityID.Zero() || claims.PrincipalID.Zero() {
		return nil, fmt.Errorf("machine repository requires authenticated tenant, entity, and principal claims")
	}
	return claims, nil
}

func wrapRepositoryError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
