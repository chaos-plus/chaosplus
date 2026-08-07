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

type GroupService struct {
	repo           *GroupRepository
	audit          auditx.Appender
	members        ActiveMemberChecker
	administrators AdministratorGuard
	nextID         IDGenerator
	now            func() time.Time
}

func NewGroupService(db *bun.DB, audit auditx.Appender, members ActiveMemberChecker, administrators AdministratorGuard, nextID IDGenerator) *GroupService {
	if db == nil || audit == nil || members == nil || administrators == nil || nextID == nil {
		panic("group service requires database, audit appender, active member checker, administrator guard, and id generator")
	}
	return &GroupService{repo: NewGroupRepository(db), audit: audit, members: members, administrators: administrators, nextID: nextID, now: time.Now}
}

func (s *GroupService) List(ctx context.Context, tenantID string) ([]Group, error) {
	tenantID = normalizePositionTenant(tenantID)
	if !validTenant(tenantID) {
		return nil, ErrGroupInvalid
	}
	rows, err := s.repo.list(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	result := make([]Group, 0, len(rows))
	for _, row := range rows {
		result = append(result, groupFromRow(row))
	}
	return result, nil
}

func (s *GroupService) Get(ctx context.Context, tenantID, id string) (Group, error) {
	tenantID, id = trimPair(tenantID, id)
	if !validTenant(tenantID) || !validID(id) {
		return Group{}, ErrGroupInvalid
	}
	row, err := s.repo.get(ctx, tenantID, id)
	if err != nil {
		return Group{}, err
	}
	return groupFromRow(row), nil
}

func (s *GroupService) Create(ctx context.Context, tenantID string, input CreateGroup) (Group, error) {
	tenantID, input, err := normalizeGroupCreate(tenantID, input)
	if err != nil {
		return Group{}, err
	}
	id, err := s.nextID()
	if err != nil {
		return Group{}, fmt.Errorf("generate group id: %w", err)
	}
	if !validID(id) {
		return Group{}, fmt.Errorf("generate group id: %w", ErrGroupInvalid)
	}
	now := s.now().UTC().UnixMilli()
	row := groupRow{TenantID: tenantID, ID: id, Name: input.Name, NameKey: nameKey(input.Name), GroupType: input.Type, RuleJSON: string(input.Rule), Description: input.Description, Status: input.Status, SortOrder: input.SortOrder, Version: 1, CreatedAt: now, UpdatedAt: now}
	event := auditx.NewEvent(ctx, tenantID, "group_created", "group", id)
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
		return Group{}, fmt.Errorf("create group: %w", err)
	}
	return groupFromRow(row), nil
}

func (s *GroupService) Update(ctx context.Context, tenantID, id string, input UpdateGroup) (Group, error) {
	tenantID, id, input, err := normalizeGroupUpdate(tenantID, id, input)
	if err != nil {
		return Group{}, err
	}
	now := s.now().UTC().UnixMilli()
	var updated groupRow
	event := auditx.NewEvent(ctx, tenantID, "group_updated", "group", id)
	err = s.repo.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		repo := s.repo.withExecutor(tx)
		current, err := repo.get(ctx, tenantID, id)
		if err != nil {
			return err
		}
		if current.Version != input.Version {
			return ErrGroupVersionConflict
		}
		updated = current
		if input.Name != nil {
			updated.Name, updated.NameKey = *input.Name, nameKey(*input.Name)
		}
		if input.Description != nil {
			updated.Description = *input.Description
		}
		if input.Status != nil {
			updated.Status = *input.Status
		}
		if input.SortOrder != nil {
			updated.SortOrder = *input.SortOrder
		}
		if input.Rule != nil {
			if current.GroupType != GroupTypeDynamic {
				return ErrGroupRuleType
			}
			updated.RuleJSON = string(*input.Rule)
		}
		changed := updated.Name != current.Name || updated.Description != current.Description || updated.Status != current.Status || updated.SortOrder != current.SortOrder || updated.RuleJSON != current.RuleJSON
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
		event.Detail["name_changed"] = updated.Name != current.Name
		event.Detail["description_changed"] = updated.Description != current.Description
		event.Detail["status_changed"] = updated.Status != current.Status
		event.Detail["sort_order_changed"] = updated.SortOrder != current.SortOrder
		event.Detail["membership_rule_changed"] = updated.RuleJSON != current.RuleJSON
		if err := policyx.Advance(ctx, tx, repo.dialect, tenantID, now); err != nil {
			return err
		}
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return Group{}, fmt.Errorf("update group: %w", err)
	}
	return groupFromRow(updated), nil
}

func (s *GroupService) Delete(ctx context.Context, tenantID, id string, version int64) error {
	tenantID, id = trimPair(tenantID, id)
	if !validTenant(tenantID) || !validID(id) || version < 1 {
		return ErrGroupInvalid
	}
	now := s.now().UTC().UnixMilli()
	event := auditx.NewEvent(ctx, tenantID, "group_deleted", "group", id)
	err := s.repo.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		repo := s.repo.withExecutor(tx)
		current, err := repo.get(ctx, tenantID, id)
		if err != nil {
			return err
		}
		if current.Version != version {
			return ErrGroupVersionConflict
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
		return fmt.Errorf("delete group: %w", err)
	}
	return nil
}

func (s *GroupService) ListMembers(ctx context.Context, tenantID, groupID string) ([]GroupMember, error) {
	tenantID, groupID = trimPair(tenantID, groupID)
	if !validTenant(tenantID) || !validID(groupID) {
		return nil, ErrGroupInvalid
	}
	group, err := s.repo.get(ctx, tenantID, groupID)
	if err != nil {
		return nil, err
	}
	if group.GroupType == GroupTypeDynamic {
		rows, err := s.repo.listDynamicMemberCandidates(ctx, tenantID, groupID)
		if err != nil {
			return nil, err
		}
		result := make([]GroupMember, 0, len(rows))
		for _, row := range rows {
			matched, err := policyx.EvaluateMemberRule([]byte(group.RuleJSON), policyx.MemberFacts{Subject: row.PrincipalID, Email: row.Email, DepartmentID: row.DepartmentID, Status: row.Status})
			if err != nil {
				return nil, fmt.Errorf("evaluate persisted dynamic group rule: %w", err)
			}
			if matched {
				result = append(result, groupMemberFromRow(row))
			}
		}
		return result, nil
	}
	rows, err := s.repo.listMembers(ctx, tenantID, groupID)
	if err != nil {
		return nil, err
	}
	result := make([]GroupMember, 0, len(rows))
	for _, row := range rows {
		result = append(result, groupMemberFromRow(row))
	}
	return result, nil
}

func (s *GroupService) PutMember(ctx context.Context, tenantID, groupID, principalID string, input GroupMemberWindow) (GroupMember, error) {
	tenantID, groupID, principalID, input, err := normalizeGroupMember(tenantID, groupID, principalID, input)
	if err != nil {
		return GroupMember{}, err
	}
	now := s.now().UTC().UnixMilli()
	startsAt, endsAt := optionalUnixMilli(input.StartsAt), optionalUnixMilli(input.EndsAt)
	var result groupMemberRow
	event := auditx.NewEvent(ctx, tenantID, "group_member_assigned", "group", groupID)
	event.Detail["principal_id"] = principalID
	err = s.repo.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		repo := s.repo.withExecutor(tx)
		group, err := repo.get(ctx, tenantID, groupID)
		if err != nil {
			return err
		}
		if group.GroupType == GroupTypeDynamic {
			return ErrDynamicGroupMembers
		}
		verify, err := s.administrators.Protect(ctx, tx, repo.dialect, tenantID)
		if err != nil {
			return err
		}
		active, err := s.members.IsMemberActiveOn(ctx, tx, tenantID, principalID)
		if err != nil {
			return fmt.Errorf("check group member: %w", err)
		}
		if !active {
			return ErrGroupMemberInactive
		}
		current, getErr := repo.getMember(ctx, tenantID, groupID, principalID)
		if getErr == nil && current.StartsAt == startsAt && current.EndsAt == endsAt {
			result = current
			return verify()
		}
		if getErr != nil && !errors.Is(getErr, ErrGroupMemberNotFound) {
			return getErr
		}
		createdAt := now
		if getErr == nil {
			createdAt = current.CreatedAt
		}
		row := groupMemberRow{TenantID: tenantID, GroupID: groupID, PrincipalID: principalID, StartsAt: startsAt, EndsAt: endsAt, CreatedAt: createdAt, UpdatedAt: now}
		if err := repo.putMember(ctx, &row); err != nil {
			return err
		}
		result, err = repo.getMember(ctx, tenantID, groupID, principalID)
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
		return GroupMember{}, fmt.Errorf("assign group member: %w", err)
	}
	return groupMemberFromRow(result), nil
}

func (s *GroupService) DeleteMember(ctx context.Context, tenantID, groupID, principalID string) (bool, error) {
	tenantID, groupID, principalID, _, err := normalizeGroupMember(tenantID, groupID, principalID, GroupMemberWindow{})
	if err != nil {
		return false, err
	}
	now := s.now().UTC().UnixMilli()
	changed := false
	event := auditx.NewEvent(ctx, tenantID, "group_member_removed", "group", groupID)
	event.Detail["principal_id"] = principalID
	err = s.repo.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		repo := s.repo.withExecutor(tx)
		group, err := repo.get(ctx, tenantID, groupID)
		if err != nil {
			return err
		}
		if group.GroupType == GroupTypeDynamic {
			return ErrDynamicGroupMembers
		}
		verify, err := s.administrators.Protect(ctx, tx, repo.dialect, tenantID)
		if err != nil {
			return err
		}
		changed, err = repo.deleteMember(ctx, tenantID, groupID, principalID)
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
		return false, fmt.Errorf("remove group member: %w", err)
	}
	return changed, nil
}

func groupFromRow(row groupRow) Group {
	return Group{ID: row.ID, TenantID: row.TenantID, Name: row.Name, Type: row.GroupType, Rule: MembershipRule(row.RuleJSON), Description: row.Description, Status: row.Status, SortOrder: row.SortOrder, Version: row.Version, CreatedAt: unixTime(row.CreatedAt), UpdatedAt: unixTime(row.UpdatedAt)}
}

func groupMemberFromRow(row groupMemberRow) GroupMember {
	result := GroupMember{GroupID: row.GroupID, PrincipalID: row.PrincipalID, DisplayName: row.DisplayName, Email: row.Email, CreatedAt: unixTime(row.CreatedAt), UpdatedAt: unixTime(row.UpdatedAt)}
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
