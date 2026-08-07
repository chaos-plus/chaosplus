package iam

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/uptrace/bun"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"

	iamdomain "github.com/chaos-plus/chaosplus/internal/modules/iam/domain"
)

const (
	maxEntityDepth         = 16
	maxEntityMetadataBytes = 16 << 10
)

type entityRow struct {
	bun.BaseModel `bun:"table:iam_entities"`
	TenantID      string `bun:"tenant_id,pk"`
	ID            string `bun:"id,pk"`
	ParentID      *string
	Type          string
	Name          string
	Status        iamdomain.EntityStatus
	Metadata      string
	CreatedAt     int64
	UpdatedAt     int64
}

type entityRoleBindingRow struct {
	bun.BaseModel `bun:"table:iam_role_bindings"`
	TenantID      string `bun:"tenant_id,pk"`
	RoleID        string `bun:"role_id,pk"`
	PrincipalID   string `bun:"principal_id,pk"`
	ScopeType     string `bun:"scope_type,pk"`
	ScopeID       string `bun:"scope_id,pk"`
	Effect        iamdomain.BindingEffect
	ExpiresAt     int64
	CreatedAt     int64
}

func (r *Repository) CreateEntity(ctx context.Context, entity iamdomain.Entity) (iamdomain.Entity, error) {
	id, err := r.nextID()
	if err != nil {
		return iamdomain.Entity{}, fmt.Errorf("generate entity id: %w", err)
	}
	metadata, err := json.Marshal(entity.Metadata)
	if err != nil {
		return iamdomain.Entity{}, fmt.Errorf("encode entity metadata: %w", err)
	}
	now := r.now().UTC().UnixMilli()
	row := entityRow{
		TenantID: entity.TenantID, ID: id, ParentID: nullableString(entity.ParentID), Type: entity.Type,
		Name: entity.Name, Status: entity.Status, Metadata: string(metadata), CreatedAt: now, UpdatedAt: now,
	}
	if _, err := r.executor.NewInsert().Model(&row).Exec(ctx); err != nil {
		if bunx.IsUniqueViolation(err) {
			return iamdomain.Entity{}, iamdomain.ErrEntityConflict
		}
		return iamdomain.Entity{}, fmt.Errorf("insert entity: %w", err)
	}
	return entityFromRow(row)
}

func (r *Repository) ListEntities(ctx context.Context, tenantID string) ([]iamdomain.Entity, error) {
	var rows []entityRow
	if err := r.executor.NewSelect().Model(&rows).Where("tenant_id = ?", tenantID).
		Order("parent_id ASC", "type ASC", "name ASC", "id ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list entities: %w", err)
	}
	entities := make([]iamdomain.Entity, 0, len(rows))
	for _, row := range rows {
		entity, err := entityFromRow(row)
		if err != nil {
			return nil, err
		}
		entities = append(entities, entity)
	}
	return entities, nil
}

func (r *Repository) GetEntity(ctx context.Context, tenantID, entityID string) (iamdomain.Entity, error) {
	row, err := r.getEntityRow(ctx, tenantID, entityID)
	if err != nil {
		return iamdomain.Entity{}, err
	}
	return entityFromRow(row)
}

func (r *Repository) UpdateEntity(ctx context.Context, entity iamdomain.Entity) (iamdomain.Entity, error) {
	metadata, err := json.Marshal(entity.Metadata)
	if err != nil {
		return iamdomain.Entity{}, fmt.Errorf("encode entity metadata: %w", err)
	}
	now := r.now().UTC().UnixMilli()
	result, err := r.executor.NewUpdate().Model((*entityRow)(nil)).
		Set("parent_id = ?", nullableString(entity.ParentID)).Set("type = ?", entity.Type).
		Set("name = ?", entity.Name).Set("status = ?", entity.Status).
		Set("metadata = ?", string(metadata)).Set("updated_at = ?", now).
		Where("tenant_id = ? AND id = ?", entity.TenantID, entity.ID).Exec(ctx)
	if err != nil {
		if bunx.IsUniqueViolation(err) {
			return iamdomain.Entity{}, iamdomain.ErrEntityConflict
		}
		return iamdomain.Entity{}, fmt.Errorf("update entity: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return iamdomain.Entity{}, iamdomain.ErrEntityNotFound
	}
	return r.GetEntity(ctx, entity.TenantID, entity.ID)
}

func (r *Repository) DeleteEntity(ctx context.Context, tenantID, entityID string) error {
	if _, err := r.getEntityRow(ctx, tenantID, entityID); err != nil {
		return err
	}
	children, err := r.executor.NewSelect().Model((*entityRow)(nil)).
		Where("tenant_id = ? AND parent_id = ?", tenantID, entityID).Count(ctx)
	if err != nil {
		return fmt.Errorf("count entity children: %w", err)
	}
	if children > 0 {
		return iamdomain.ErrEntityHasChildren
	}
	bindings, err := r.executor.NewSelect().Model((*entityRoleBindingRow)(nil)).
		Where("tenant_id = ? AND scope_type = 'entity' AND scope_id = ?", tenantID, entityID).Count(ctx)
	if err != nil {
		return fmt.Errorf("count entity bindings: %w", err)
	}
	if bindings > 0 {
		return iamdomain.ErrEntityHasBindings
	}
	relationships, err := r.entityRelationshipCount(ctx, tenantID, entityID)
	if err != nil {
		return err
	}
	if relationships > 0 {
		return iamdomain.ErrEntityHasRelationships
	}
	if _, err := r.executor.NewDelete().Model((*entityRow)(nil)).
		Where("tenant_id = ? AND id = ?", tenantID, entityID).Exec(ctx); err != nil {
		return fmt.Errorf("delete entity: %w", err)
	}
	return nil
}

func (r *Repository) ListEntityRoleBindings(ctx context.Context, tenantID, entityID string) ([]iamdomain.EntityRoleBinding, error) {
	if _, err := r.getEntityRow(ctx, tenantID, entityID); err != nil {
		return nil, err
	}
	var rows []entityRoleBindingRow
	if err := r.executor.NewSelect().Model(&rows).
		Where("tenant_id = ? AND scope_type = 'entity' AND scope_id = ?", tenantID, entityID).
		Order("role_id ASC", "principal_id ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list entity role bindings: %w", err)
	}
	bindings := make([]iamdomain.EntityRoleBinding, 0, len(rows))
	for _, row := range rows {
		bindings = append(bindings, bindingFromRow(row))
	}
	return bindings, nil
}

func (r *Repository) PutEntityRoleBinding(ctx context.Context, tenantID, entityID, roleID, principalID string, effect iamdomain.BindingEffect, expiresAt time.Time) (bool, iamdomain.EntityRoleBinding, error) {
	key := "tenant_id = ? AND role_id = ? AND principal_id = ? AND scope_type = 'entity' AND scope_id = ?"
	var current entityRoleBindingRow
	err := r.executor.NewSelect().Model(&current).Where(key, tenantID, roleID, principalID, entityID).Scan(ctx)
	expiresMillis := unixMillisOrZero(expiresAt)
	if err == nil {
		if current.Effect == effect && current.ExpiresAt == expiresMillis {
			return false, bindingFromRow(current), nil
		}
		if _, err := r.executor.NewUpdate().Model((*entityRoleBindingRow)(nil)).
			Set("effect = ?", effect).Set("expires_at = ?", expiresMillis).
			Where(key, tenantID, roleID, principalID, entityID).Exec(ctx); err != nil {
			return false, iamdomain.EntityRoleBinding{}, fmt.Errorf("update entity role binding: %w", err)
		}
		current.Effect, current.ExpiresAt = effect, expiresMillis
		return true, bindingFromRow(current), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, iamdomain.EntityRoleBinding{}, fmt.Errorf("get entity role binding: %w", err)
	}
	row := entityRoleBindingRow{
		TenantID: tenantID, RoleID: roleID, PrincipalID: principalID, ScopeType: "entity", ScopeID: entityID,
		Effect: effect, ExpiresAt: expiresMillis, CreatedAt: r.now().UTC().UnixMilli(),
	}
	if _, err := r.executor.NewInsert().Model(&row).Exec(ctx); err != nil {
		return false, iamdomain.EntityRoleBinding{}, fmt.Errorf("insert entity role binding: %w", err)
	}
	return true, bindingFromRow(row), nil
}

func (r *Repository) DeleteEntityRoleBinding(ctx context.Context, tenantID, entityID, roleID, principalID string) (bool, error) {
	result, err := r.executor.NewDelete().Model((*entityRoleBindingRow)(nil)).
		Where("tenant_id = ? AND role_id = ? AND principal_id = ? AND scope_type = 'entity' AND scope_id = ?", tenantID, roleID, principalID, entityID).Exec(ctx)
	if err != nil {
		return false, fmt.Errorf("delete entity role binding: %w", err)
	}
	affected, _ := result.RowsAffected()
	return affected > 0, nil
}

func (r *Repository) getEntityRow(ctx context.Context, tenantID, entityID string) (entityRow, error) {
	var row entityRow
	if err := r.executor.NewSelect().Model(&row).Where("tenant_id = ? AND id = ?", tenantID, entityID).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return entityRow{}, iamdomain.ErrEntityNotFound
		}
		return entityRow{}, fmt.Errorf("get entity: %w", err)
	}
	return row, nil
}

func (s *Service) CreateEntity(ctx context.Context, entity iamdomain.Entity) (iamdomain.Entity, error) {
	if err := normalizeEntity(&entity); err != nil {
		return iamdomain.Entity{}, err
	}
	record := newAuditRecord(ctx, entity.TenantID, "entity_created", "entity", "")
	record.PolicyChanged = true
	var created iamdomain.Entity
	err := s.writes.Run(ctx, record, func(repo *Repository) error {
		if err := validateEntityParent(ctx, repo, entity.TenantID, "", entity.ParentID); err != nil {
			return err
		}
		var err error
		created, err = repo.CreateEntity(ctx, entity)
		record.TargetID = created.ID
		return err
	})
	return created, err
}

func (s *Service) ListEntities(ctx context.Context, tenantID string) ([]iamdomain.Entity, error) {
	if err := validateTenant(tenantID); err != nil {
		return nil, err
	}
	return s.repo.ListEntities(ctx, tenantID)
}

func (s *Service) GetEntity(ctx context.Context, tenantID, entityID string) (iamdomain.Entity, error) {
	if err := validateEntityRef(tenantID, entityID); err != nil {
		return iamdomain.Entity{}, err
	}
	return s.repo.GetEntity(ctx, tenantID, entityID)
}

func (s *Service) UpdateEntity(ctx context.Context, tenantID, entityID string, patch iamdomain.EntityPatch) (iamdomain.Entity, error) {
	if err := validateEntityRef(tenantID, entityID); err != nil {
		return iamdomain.Entity{}, err
	}
	if patch.ParentID == nil && patch.Type == nil && patch.Name == nil && patch.Status == nil && patch.Metadata == nil {
		return iamdomain.Entity{}, fmt.Errorf("%w: no entity fields supplied", iamdomain.ErrInvalidArgument)
	}
	record := newAuditRecord(ctx, tenantID, "entity_updated", "entity", entityID)
	record.PolicyChanged = true
	var updated iamdomain.Entity
	err := s.writes.Run(ctx, record, func(repo *Repository) error {
		current, err := repo.GetEntity(ctx, tenantID, entityID)
		if err != nil {
			return err
		}
		if patch.Type != nil && strings.TrimSpace(*patch.Type) != current.Type {
			count, err := repo.entityRelationshipCount(ctx, tenantID, entityID)
			if err != nil {
				return err
			}
			if count > 0 {
				return iamdomain.ErrEntityHasRelationships
			}
		}
		applyEntityPatch(&current, patch)
		if err := normalizeEntity(&current); err != nil {
			return err
		}
		if err := validateEntityParent(ctx, repo, tenantID, entityID, current.ParentID); err != nil {
			return err
		}
		updated, err = repo.UpdateEntity(ctx, current)
		return err
	})
	return updated, err
}

func (s *Service) DeleteEntity(ctx context.Context, tenantID, entityID string) error {
	if err := validateEntityRef(tenantID, entityID); err != nil {
		return err
	}
	record := newAuditRecord(ctx, tenantID, "entity_deleted", "entity", entityID)
	record.PolicyChanged = true
	return s.writes.Run(ctx, record, func(repo *Repository) error {
		return repo.DeleteEntity(ctx, tenantID, entityID)
	})
}

func (s *Service) ListEntityRoleBindings(ctx context.Context, tenantID, entityID string) ([]iamdomain.EntityRoleBinding, error) {
	if err := validateEntityRef(tenantID, entityID); err != nil {
		return nil, err
	}
	return s.repo.ListEntityRoleBindings(ctx, tenantID, entityID)
}

func (s *Service) PutEntityRoleBinding(ctx context.Context, tenantID, entityID, roleID, principalID string, effect iamdomain.BindingEffect, expiresAt time.Time) (iamdomain.EntityRoleBinding, bool, error) {
	if err := validateEntityBinding(tenantID, entityID, roleID, principalID, effect, expiresAt); err != nil {
		return iamdomain.EntityRoleBinding{}, false, err
	}
	// A deny binding only narrows access, so only allow bindings can escalate.
	if effect == iamdomain.BindingAllow {
		if err := s.requireGrantableRole(ctx, tenantID, roleID); err != nil {
			return iamdomain.EntityRoleBinding{}, false, err
		}
	}
	record := newAuditRecord(ctx, tenantID, "entity_role_binding_put", "entity", entityID)
	record.Detail["role_id"], record.Detail["principal_id"], record.Detail["effect"] = roleID, principalID, effect
	var binding iamdomain.EntityRoleBinding
	var changed bool
	err := s.writes.Run(ctx, record, func(repo *Repository) error {
		if _, err := repo.GetEntity(ctx, tenantID, entityID); err != nil {
			return err
		}
		if err := ensureRole(ctx, repo.executor, tenantID, roleID); err != nil {
			return err
		}
		active, err := repo.IsMemberActive(ctx, tenantID, principalID)
		if err != nil {
			return err
		}
		if !active {
			return iamdomain.ErrMemberInactive
		}
		changed, binding, err = repo.PutEntityRoleBinding(ctx, tenantID, entityID, roleID, principalID, effect, expiresAt)
		record.PolicyChanged, record.SkipAudit = changed, !changed
		return err
	})
	return binding, changed, err
}

func (s *Service) DeleteEntityRoleBinding(ctx context.Context, tenantID, entityID, roleID, principalID string) (bool, error) {
	if err := validateEntityBinding(tenantID, entityID, roleID, principalID, iamdomain.BindingAllow, time.Time{}); err != nil {
		return false, err
	}
	record := newAuditRecord(ctx, tenantID, "entity_role_binding_deleted", "entity", entityID)
	record.Detail["role_id"], record.Detail["principal_id"] = roleID, principalID
	var changed bool
	err := s.writes.Run(ctx, record, func(repo *Repository) error {
		if _, err := repo.GetEntity(ctx, tenantID, entityID); err != nil {
			return err
		}
		var err error
		changed, err = repo.DeleteEntityRoleBinding(ctx, tenantID, entityID, roleID, principalID)
		record.PolicyChanged, record.SkipAudit = changed, !changed
		return err
	})
	return changed, err
}

func normalizeEntity(entity *iamdomain.Entity) error {
	if entity == nil || validateTenant(entity.TenantID) != nil {
		return fmt.Errorf("%w: invalid entity tenant", iamdomain.ErrInvalidArgument)
	}
	entity.ParentID = strings.TrimSpace(entity.ParentID)
	entity.Type = strings.TrimSpace(entity.Type)
	entity.Name = strings.TrimSpace(entity.Name)
	if !validEntityType(entity.Type) || entity.Name == "" || len(entity.Name) > 200 {
		return fmt.Errorf("%w: invalid entity type or name", iamdomain.ErrInvalidArgument)
	}
	if entity.Status == "" {
		entity.Status = iamdomain.EntityActive
	}
	if entity.Status != iamdomain.EntityActive && entity.Status != iamdomain.EntityDisabled {
		return fmt.Errorf("%w: invalid entity status", iamdomain.ErrInvalidArgument)
	}
	if entity.Metadata == nil {
		entity.Metadata = map[string]any{}
	}
	metadata, err := json.Marshal(entity.Metadata)
	if err != nil || len(metadata) > maxEntityMetadataBytes {
		return fmt.Errorf("%w: invalid entity metadata", iamdomain.ErrInvalidArgument)
	}
	return nil
}

func validateEntityParent(ctx context.Context, repo *Repository, tenantID, entityID, parentID string) error {
	if parentID == "" {
		return nil
	}
	current := parentID
	for ancestors := 1; ; ancestors++ {
		if current == entityID {
			return iamdomain.ErrEntityHierarchy
		}
		parent, err := repo.GetEntity(ctx, tenantID, current)
		if err != nil {
			return err
		}
		if parent.ParentID == "" {
			return nil
		}
		if ancestors >= maxEntityDepth-1 {
			return iamdomain.ErrEntityHierarchy
		}
		current = parent.ParentID
	}
}

func validateEntityRef(tenantID, entityID string) error {
	if err := validateTenant(tenantID); err != nil {
		return err
	}
	entityID = strings.TrimSpace(entityID)
	if entityID == "" || len(entityID) > 64 {
		return fmt.Errorf("%w: invalid entity id", iamdomain.ErrInvalidArgument)
	}
	return nil
}

func validateEntityBinding(tenantID, entityID, roleID, principalID string, effect iamdomain.BindingEffect, expiresAt time.Time) error {
	if err := validateEntityRef(tenantID, entityID); err != nil {
		return err
	}
	if err := validateRoleRef(tenantID, roleID); err != nil {
		return err
	}
	if strings.TrimSpace(principalID) == "" || len(principalID) > 64 {
		return fmt.Errorf("%w: invalid binding principal", iamdomain.ErrInvalidArgument)
	}
	if effect != iamdomain.BindingAllow && effect != iamdomain.BindingDeny {
		return fmt.Errorf("%w: invalid binding effect", iamdomain.ErrInvalidArgument)
	}
	if !expiresAt.IsZero() && !expiresAt.After(time.Now().UTC()) {
		return fmt.Errorf("%w: binding expiry must be in the future", iamdomain.ErrInvalidArgument)
	}
	return nil
}

func applyEntityPatch(entity *iamdomain.Entity, patch iamdomain.EntityPatch) {
	if patch.ParentID != nil {
		entity.ParentID = *patch.ParentID
	}
	if patch.Type != nil {
		entity.Type = *patch.Type
	}
	if patch.Name != nil {
		entity.Name = *patch.Name
	}
	if patch.Status != nil {
		entity.Status = *patch.Status
	}
	if patch.Metadata != nil {
		entity.Metadata = *patch.Metadata
	}
}

func validEntityType(value string) bool {
	if value == "" || len(value) > 64 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, char := range value[1:] {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '_' && char != '-' {
			return false
		}
	}
	return true
}

func entityFromRow(row entityRow) (iamdomain.Entity, error) {
	metadata := map[string]any{}
	if err := json.Unmarshal([]byte(row.Metadata), &metadata); err != nil {
		return iamdomain.Entity{}, fmt.Errorf("decode entity metadata: %w", err)
	}
	parentID := ""
	if row.ParentID != nil {
		parentID = *row.ParentID
	}
	return iamdomain.Entity{
		ID: row.ID, TenantID: row.TenantID, ParentID: parentID, Type: row.Type, Name: row.Name,
		Status: row.Status, Metadata: metadata, CreatedAt: time.UnixMilli(row.CreatedAt).UTC(), UpdatedAt: time.UnixMilli(row.UpdatedAt).UTC(),
	}, nil
}

func bindingFromRow(row entityRoleBindingRow) iamdomain.EntityRoleBinding {
	binding := iamdomain.EntityRoleBinding{
		EntityID: row.ScopeID, RoleID: row.RoleID, PrincipalID: row.PrincipalID,
		Effect: row.Effect, CreatedAt: time.UnixMilli(row.CreatedAt).UTC(),
	}
	if row.ExpiresAt > 0 {
		binding.ExpiresAt = time.UnixMilli(row.ExpiresAt).UTC()
	}
	return binding
}

func nullableString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func unixMillisOrZero(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UTC().UnixMilli()
}
