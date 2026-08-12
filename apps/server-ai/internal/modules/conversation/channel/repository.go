package channel

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
	Create(context.Context, *Channel, *Member) error
	List(context.Context, guid.ID) ([]Channel, error)
	Get(context.Context, guid.ID) (*Channel, error)
	Update(context.Context, *Channel, int64) error
	Delete(context.Context, guid.ID, int64) error
	AddMember(context.Context, *Member) error
	RemoveMember(context.Context, guid.ID, guid.ID, MemberKind, int64) error
	ListMembers(context.Context, guid.ID) ([]Member, error)
	IsMember(context.Context, guid.ID, guid.ID, MemberKind) (bool, error)
}

type AgentDirectory interface {
	IsAvailable(context.Context, guid.ID) (bool, error)
}

type BunRepository struct {
	db     *bun.DB
	nextID func() (guid.ID, error)
}

func NewRepository(db *bun.DB, nextID func() (guid.ID, error)) *BunRepository {
	if db == nil || nextID == nil {
		panic("channel repository requires database and id generator")
	}
	return &BunRepository{db: db, nextID: nextID}
}

func scope(ctx context.Context) (guid.ID, guid.ID, guid.ID, error) {
	tenantID, entityID, principalID := authn.TenantIDFromContext(ctx), authn.EntityIDFromContext(ctx), authn.PrincipalIDFromContext(ctx)
	if tenantID.Zero() || entityID.Zero() || principalID.Zero() {
		return 0, 0, 0, errors.New("channel requires verified IAM claims")
	}
	return tenantID, entityID, principalID, nil
}

func (r *BunRepository) Create(ctx context.Context, value *Channel, owner *Member) error {
	tenantID, entityID, principalID, err := scope(ctx)
	if err != nil {
		return err
	}
	value.ID, err = r.nextID()
	if err != nil {
		return fmt.Errorf("generate channel id: %w", err)
	}
	owner.ID, err = r.nextID()
	if err != nil {
		return fmt.Errorf("generate channel owner membership id: %w", err)
	}
	now := time.Now().UTC().UnixMilli()
	value.TenantID, value.EntityID, value.OwnerID = tenantID, entityID, principalID
	value.CreatedAt, value.CreatedBy, value.UpdatedAt, value.UpdatedBy, value.Version = now, principalID, now, principalID, 1
	owner.TenantID, owner.EntityID, owner.ChannelID, owner.MemberID = tenantID, entityID, value.ID, principalID
	owner.Kind, owner.Role, owner.JoinedAt, owner.JoinedBy, owner.Version = MemberHuman, RoleOwner, now, principalID, 1
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(value).Exec(ctx); err != nil {
			return fmt.Errorf("create channel: %w", err)
		}
		if _, err := tx.NewInsert().Model(owner).Exec(ctx); err != nil {
			return fmt.Errorf("create channel owner membership: %w", err)
		}
		return nil
	})
}

func (r *BunRepository) List(ctx context.Context, projectID guid.ID) ([]Channel, error) {
	tenantID, entityID, _, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	items := []Channel{}
	query := r.db.NewSelect().Model(&items).
		Where("tenant_id = ? AND entity_id = ? AND deleted_at = 0", tenantID, entityID)
	if !projectID.Zero() {
		query = query.Where("project_id = ?", projectID)
	}
	if err := query.Order("created_at ASC", "id ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	return items, nil
}

func (r *BunRepository) Get(ctx context.Context, id guid.ID) (*Channel, error) {
	tenantID, entityID, _, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	value := new(Channel)
	err = r.db.NewSelect().Model(value).
		Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0", id, tenantID, entityID).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get channel: %w", err)
	}
	return value, nil
}

func (r *BunRepository) Update(ctx context.Context, value *Channel, version int64) error {
	tenantID, entityID, principalID, err := scope(ctx)
	if err != nil {
		return err
	}
	result, err := r.db.NewUpdate().Model((*Channel)(nil)).
		Set("name = ?", value.Name).Set("topic = ?", value.Topic).Set("status = ?", value.Status).
		Set("updated_at = ?", time.Now().UTC().UnixMilli()).Set("updated_by = ?", principalID).Set("version = version + 1").
		Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0 AND version = ?", value.ID, tenantID, entityID, version).Exec(ctx)
	if err != nil {
		return fmt.Errorf("update channel: %w", err)
	}
	return changed(result, value, version)
}

func (r *BunRepository) Delete(ctx context.Context, id guid.ID, version int64) error {
	tenantID, entityID, principalID, err := scope(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		result, err := tx.NewUpdate().Model((*Channel)(nil)).
			Set("deleted_at = ?", now).Set("deleted_by = ?", principalID).Set("updated_at = ?", now).
			Set("updated_by = ?", principalID).Set("version = version + 1").
			Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0 AND version = ?", id, tenantID, entityID, version).Exec(ctx)
		if err != nil {
			return fmt.Errorf("delete channel: %w", err)
		}
		if err := changed(result, nil, version); err != nil {
			return err
		}
		_, err = tx.NewUpdate().Model((*Member)(nil)).
			Set("removed_at = ?", now).Set("removed_by = ?", principalID).Set("version = version + 1").
			Where("tenant_id = ? AND entity_id = ? AND channel_id = ? AND removed_at = 0", tenantID, entityID, id).Exec(ctx)
		return err
	})
}

func (r *BunRepository) AddMember(ctx context.Context, value *Member) error {
	tenantID, entityID, principalID, err := scope(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	var existing Member
	err = r.db.NewSelect().Model(&existing).
		Where("tenant_id = ? AND entity_id = ? AND channel_id = ? AND member_id = ? AND kind = ?", tenantID, entityID, value.ChannelID, value.MemberID, value.Kind).Scan(ctx)
	switch {
	case err == nil && existing.RemovedAt == 0:
		return ErrMemberConflict
	case err == nil:
		result, updateErr := r.db.NewUpdate().Model((*Member)(nil)).
			Set("role = ?", RoleMember).Set("joined_at = ?", now).Set("joined_by = ?", principalID).
			Set("removed_at = 0").Set("removed_by = 0").Set("version = version + 1").
			Where("id = ? AND tenant_id = ? AND entity_id = ? AND version = ?", existing.ID, tenantID, entityID, existing.Version).Exec(ctx)
		if updateErr != nil {
			return fmt.Errorf("restore channel member: %w", updateErr)
		}
		return changed(result, nil, existing.Version)
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("find channel member: %w", err)
	}
	value.ID, err = r.nextID()
	if err != nil {
		return fmt.Errorf("generate channel member id: %w", err)
	}
	value.TenantID, value.EntityID, value.Role = tenantID, entityID, RoleMember
	value.JoinedAt, value.JoinedBy, value.Version = now, principalID, 1
	if _, err := r.db.NewInsert().Model(value).Exec(ctx); err != nil {
		return fmt.Errorf("add channel member: %w", err)
	}
	return nil
}

func (r *BunRepository) RemoveMember(ctx context.Context, channelID, memberID guid.ID, kind MemberKind, version int64) error {
	tenantID, entityID, principalID, err := scope(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	result, err := r.db.NewUpdate().Model((*Member)(nil)).
		Set("removed_at = ?", now).Set("removed_by = ?", principalID).Set("version = version + 1").
		Where("tenant_id = ? AND entity_id = ? AND channel_id = ? AND member_id = ? AND kind = ? AND role = ? AND removed_at = 0 AND version = ?",
			tenantID, entityID, channelID, memberID, kind, RoleMember, version).Exec(ctx)
	if err != nil {
		return fmt.Errorf("remove channel member: %w", err)
	}
	return changed(result, nil, version)
}

func (r *BunRepository) ListMembers(ctx context.Context, channelID guid.ID) ([]Member, error) {
	tenantID, entityID, _, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	items := []Member{}
	if err := r.db.NewSelect().Model(&items).
		Where("tenant_id = ? AND entity_id = ? AND channel_id = ? AND removed_at = 0", tenantID, entityID, channelID).
		Order("joined_at ASC", "id ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list channel members: %w", err)
	}
	return items, nil
}

func (r *BunRepository) IsMember(ctx context.Context, channelID, memberID guid.ID, kind MemberKind) (bool, error) {
	tenantID, entityID, _, err := scope(ctx)
	if err != nil {
		return false, err
	}
	count, err := r.db.NewSelect().Model((*Member)(nil)).
		Where("tenant_id = ? AND entity_id = ? AND channel_id = ? AND member_id = ? AND kind = ? AND removed_at = 0", tenantID, entityID, channelID, memberID, kind).Count(ctx)
	return count == 1, err
}

func changed(result sql.Result, value *Channel, version int64) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrVersionConflict
	}
	if value != nil {
		value.Version = version + 1
	}
	return nil
}
