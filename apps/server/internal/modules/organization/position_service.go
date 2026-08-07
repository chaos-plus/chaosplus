package organization

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/policyx"
	"github.com/uptrace/bun"
)

type ActiveMemberChecker interface {
	IsMemberActiveOn(context.Context, bun.IDB, string, string) (bool, error)
}

type PositionService struct {
	repo           *PositionRepository
	audit          auditx.Appender
	members        ActiveMemberChecker
	administrators AdministratorGuard
	nextID         IDGenerator
	now            func() time.Time
}

func NewPositionService(db *bun.DB, audit auditx.Appender, members ActiveMemberChecker, administrators AdministratorGuard, nextID IDGenerator) *PositionService {
	if db == nil || audit == nil || members == nil || administrators == nil || nextID == nil {
		panic("position service requires database, audit appender, active member checker, administrator guard, and id generator")
	}
	return &PositionService{repo: NewPositionRepository(db), audit: audit, members: members, administrators: administrators, nextID: nextID, now: time.Now}
}

func (s *PositionService) List(ctx context.Context, tenantID string) ([]Position, error) {
	tenantID = normalizePositionTenant(tenantID)
	if !validTenant(tenantID) {
		return nil, ErrPositionInvalid
	}
	rows, err := s.repo.list(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	result := make([]Position, 0, len(rows))
	for _, row := range rows {
		result = append(result, positionFromRow(row))
	}
	return result, nil
}

func (s *PositionService) Get(ctx context.Context, tenantID, id string) (Position, error) {
	tenantID, id = trimPair(tenantID, id)
	if !validTenant(tenantID) || !validID(id) {
		return Position{}, ErrPositionInvalid
	}
	row, err := s.repo.get(ctx, tenantID, id)
	if err != nil {
		return Position{}, err
	}
	return positionFromRow(row), nil
}

func (s *PositionService) Create(ctx context.Context, tenantID string, input CreatePosition) (Position, error) {
	tenantID, input, err := normalizePositionCreate(tenantID, input)
	if err != nil {
		return Position{}, err
	}
	id, err := s.nextID()
	if err != nil {
		return Position{}, fmt.Errorf("generate position id: %w", err)
	}
	if !validID(id) {
		return Position{}, fmt.Errorf("generate position id: %w", ErrPositionInvalid)
	}
	now := s.now().UTC().UnixMilli()
	row := positionRow{TenantID: tenantID, ID: id, Code: input.Code, Name: input.Name, Status: input.Status, SortOrder: input.SortOrder, Version: 1, CreatedAt: now, UpdatedAt: now}
	event := auditx.NewEvent(ctx, tenantID, "position_created", "position", id)
	event.Detail["status"] = input.Status
	err = s.repo.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		repo := s.repo.withExecutor(tx)
		if err := repo.insert(ctx, &row); err != nil {
			return err
		}
		if err := policyx.Advance(ctx, tx, repo.dialect, tenantID, now); err != nil {
			return err
		}
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return Position{}, fmt.Errorf("create position: %w", err)
	}
	return positionFromRow(row), nil
}

func (s *PositionService) Update(ctx context.Context, tenantID, id string, input UpdatePosition) (Position, error) {
	tenantID, id, input, err := normalizePositionUpdate(tenantID, id, input)
	if err != nil {
		return Position{}, err
	}
	now := s.now().UTC().UnixMilli()
	var updated positionRow
	event := auditx.NewEvent(ctx, tenantID, "position_updated", "position", id)
	err = s.repo.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		repo := s.repo.withExecutor(tx)
		current, err := repo.get(ctx, tenantID, id)
		if err != nil {
			return err
		}
		if current.Version != input.Version {
			return ErrPositionVersionConflict
		}
		updated = current
		if input.Code != nil {
			updated.Code = *input.Code
		}
		if input.Name != nil {
			updated.Name = *input.Name
		}
		if input.Status != nil {
			updated.Status = *input.Status
		}
		if input.SortOrder != nil {
			updated.SortOrder = *input.SortOrder
		}
		changed := updated.Code != current.Code || updated.Name != current.Name || updated.Status != current.Status || updated.SortOrder != current.SortOrder
		if !changed {
			return nil
		}
		var verify func() error
		if current.Status == StatusActive && updated.Status == StatusDisabled {
			verify, err = s.administrators.Protect(ctx, tx, repo.dialect, tenantID)
			if err != nil {
				return err
			}
		}
		updated.Version, updated.UpdatedAt = current.Version+1, now
		if err := repo.update(ctx, &updated, current.Version); err != nil {
			return err
		}
		if verify != nil {
			if err := verify(); err != nil {
				return err
			}
		}
		event.Detail["code_changed"] = updated.Code != current.Code
		event.Detail["name_changed"] = updated.Name != current.Name
		event.Detail["status_changed"] = updated.Status != current.Status
		event.Detail["sort_order_changed"] = updated.SortOrder != current.SortOrder
		if err := policyx.Advance(ctx, tx, repo.dialect, tenantID, now); err != nil {
			return err
		}
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return Position{}, fmt.Errorf("update position: %w", err)
	}
	return positionFromRow(updated), nil
}

func (s *PositionService) Delete(ctx context.Context, tenantID, id string, version int64) error {
	tenantID, id = trimPair(tenantID, id)
	if !validTenant(tenantID) || !validID(id) || version < 1 {
		return ErrPositionInvalid
	}
	now := s.now().UTC().UnixMilli()
	event := auditx.NewEvent(ctx, tenantID, "position_deleted", "position", id)
	err := s.repo.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		repo := s.repo.withExecutor(tx)
		current, err := repo.get(ctx, tenantID, id)
		if err != nil {
			return err
		}
		if current.Version != version {
			return ErrPositionVersionConflict
		}
		if err := repo.delete(ctx, tenantID, id, version); err != nil {
			return err
		}
		if err := policyx.Advance(ctx, tx, repo.dialect, tenantID, now); err != nil {
			return err
		}
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return fmt.Errorf("delete position: %w", err)
	}
	return nil
}

func (s *PositionService) ListMembers(ctx context.Context, tenantID, positionID string) ([]PositionMember, error) {
	tenantID, positionID = trimPair(tenantID, positionID)
	if !validTenant(tenantID) || !validID(positionID) {
		return nil, ErrPositionInvalid
	}
	if _, err := s.repo.get(ctx, tenantID, positionID); err != nil {
		return nil, err
	}
	rows, err := s.repo.listMembers(ctx, tenantID, positionID)
	if err != nil {
		return nil, err
	}
	result := make([]PositionMember, 0, len(rows))
	for _, row := range rows {
		result = append(result, positionMemberFromRow(row))
	}
	return result, nil
}

func (s *PositionService) PutMember(ctx context.Context, tenantID, positionID, principalID string, input PositionMemberWindow) (PositionMember, error) {
	tenantID, positionID, principalID, input, err := normalizePositionMember(tenantID, positionID, principalID, input)
	if err != nil {
		return PositionMember{}, err
	}
	now := s.now().UTC().UnixMilli()
	startsAt, endsAt := optionalUnixMilli(input.StartsAt), optionalUnixMilli(input.EndsAt)
	var result positionMemberRow
	event := auditx.NewEvent(ctx, tenantID, "position_member_assigned", "position", positionID)
	event.Detail["principal_id"] = principalID
	err = s.repo.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		repo := s.repo.withExecutor(tx)
		if _, err := repo.get(ctx, tenantID, positionID); err != nil {
			return err
		}
		verify, err := s.administrators.Protect(ctx, tx, repo.dialect, tenantID)
		if err != nil {
			return err
		}
		active, err := s.members.IsMemberActiveOn(ctx, tx, tenantID, principalID)
		if err != nil {
			return fmt.Errorf("check position member: %w", err)
		}
		if !active {
			return ErrPositionMemberInactive
		}
		current, getErr := repo.getMember(ctx, tenantID, positionID, principalID)
		if getErr == nil && current.StartsAt == startsAt && current.EndsAt == endsAt {
			result = current
			return verify()
		}
		if getErr != nil && !errors.Is(getErr, ErrPositionMemberNotFound) {
			return getErr
		}
		createdAt := now
		if getErr == nil {
			createdAt = current.CreatedAt
		}
		row := positionMemberRow{TenantID: tenantID, PositionID: positionID, PrincipalID: principalID, StartsAt: startsAt, EndsAt: endsAt, CreatedAt: createdAt, UpdatedAt: now}
		if err := repo.putMember(ctx, &row); err != nil {
			return err
		}
		result, err = repo.getMember(ctx, tenantID, positionID, principalID)
		if err != nil {
			return err
		}
		if err := verify(); err != nil {
			return err
		}
		if err := policyx.Advance(ctx, tx, repo.dialect, tenantID, now); err != nil {
			return err
		}
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return PositionMember{}, fmt.Errorf("assign position member: %w", err)
	}
	return positionMemberFromRow(result), nil
}

func (s *PositionService) DeleteMember(ctx context.Context, tenantID, positionID, principalID string) (bool, error) {
	tenantID, positionID, principalID, _, err := normalizePositionMember(tenantID, positionID, principalID, PositionMemberWindow{})
	if err != nil {
		return false, err
	}
	now := s.now().UTC().UnixMilli()
	changed := false
	event := auditx.NewEvent(ctx, tenantID, "position_member_removed", "position", positionID)
	event.Detail["principal_id"] = principalID
	err = s.repo.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		repo := s.repo.withExecutor(tx)
		if _, err := repo.get(ctx, tenantID, positionID); err != nil {
			return err
		}
		verify, err := s.administrators.Protect(ctx, tx, repo.dialect, tenantID)
		if err != nil {
			return err
		}
		changed, err = repo.deleteMember(ctx, tenantID, positionID, principalID)
		if err != nil {
			return err
		}
		if err := verify(); err != nil {
			return err
		}
		if !changed {
			return nil
		}
		if err := policyx.Advance(ctx, tx, repo.dialect, tenantID, now); err != nil {
			return err
		}
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return false, fmt.Errorf("remove position member: %w", err)
	}
	return changed, nil
}

func positionFromRow(row positionRow) Position {
	return Position{ID: row.ID, TenantID: row.TenantID, Code: row.Code, Name: row.Name, Status: row.Status, SortOrder: row.SortOrder, Version: row.Version, CreatedAt: unixTime(row.CreatedAt), UpdatedAt: unixTime(row.UpdatedAt)}
}

func positionMemberFromRow(row positionMemberRow) PositionMember {
	result := PositionMember{PositionID: row.PositionID, PrincipalID: row.PrincipalID, DisplayName: row.DisplayName, Email: row.Email, CreatedAt: unixTime(row.CreatedAt), UpdatedAt: unixTime(row.UpdatedAt)}
	if row.StartsAt > 0 {
		value := unixTime(row.StartsAt)
		result.StartsAt = &value
	}
	if row.EndsAt > 0 {
		value := unixTime(row.EndsAt)
		result.EndsAt = &value
	}
	return result
}

func optionalUnixMilli(value *time.Time) int64 {
	if value == nil {
		return 0
	}
	return value.UTC().UnixMilli()
}

func normalizePositionTenant(value string) string {
	value, _ = trimPair(value, "")
	return value
}
