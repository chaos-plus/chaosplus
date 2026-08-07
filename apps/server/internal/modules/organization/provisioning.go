package organization

import (
	"context"
	"fmt"
	"strings"

	"github.com/chaos-plus/chaosplus/internal/core/extension/policyx"
	"github.com/uptrace/bun"
)

// ProvisionedGroupInput is the canonical group command used by transactional
// directory adapters. Provisioned groups are always static groups.
type ProvisionedGroupInput struct {
	DisplayName string
	Active      bool
	MemberIDs   []string
}

// CreateProvisionedTo writes a static group through a caller-owned transaction.
// The caller owns the corresponding provisioning mapping and audit event.
func (s *GroupService) CreateProvisionedTo(ctx context.Context, db bun.IDB, tenantID string, input ProvisionedGroupInput) (Group, error) {
	input, memberIDs, err := normalizeProvisionedGroup(input)
	if err != nil || db == nil {
		return Group{}, ErrGroupInvalid
	}
	tenantID, create, err := normalizeGroupCreate(tenantID, CreateGroup{Name: input.DisplayName, Type: GroupTypeStatic, Status: provisionedGroupStatus(input.Active)})
	if err != nil {
		return Group{}, err
	}
	id, err := s.nextID()
	if err != nil || !validID(id) {
		return Group{}, fmt.Errorf("generate provisioned group id: %w", errorsOr(err, ErrGroupInvalid))
	}
	now := s.now().UTC().UnixMilli()
	row := groupRow{TenantID: tenantID, ID: id, Name: create.Name, NameKey: nameKey(create.Name), GroupType: GroupTypeStatic, Description: create.Description, Status: create.Status, SortOrder: create.SortOrder, Version: 1, CreatedAt: now, UpdatedAt: now}
	repo := s.repo.withExecutor(db)
	if err := repo.insert(ctx, &row); err != nil {
		return Group{}, err
	}
	for _, principalID := range memberIDs {
		if err := s.requireProvisionedMember(ctx, db, tenantID, principalID); err != nil {
			return Group{}, err
		}
		if err := repo.putMember(ctx, &groupMemberRow{TenantID: tenantID, GroupID: id, PrincipalID: principalID, CreatedAt: now, UpdatedAt: now}); err != nil {
			return Group{}, err
		}
	}
	if err := policyx.Advance(ctx, db, repo.dialect, tenantID, now); err != nil {
		return Group{}, err
	}
	return groupFromRow(row), nil
}

// ReplaceProvisionedTo replaces the profile, status, and complete member set of
// a directory-owned static group inside the caller's transaction.
func (s *GroupService) ReplaceProvisionedTo(ctx context.Context, db bun.IDB, tenantID, id string, input ProvisionedGroupInput) (Group, error) {
	input, memberIDs, err := normalizeProvisionedGroup(input)
	tenantID, id = trimPair(tenantID, id)
	if err != nil || db == nil || !validTenant(tenantID) || !validID(id) {
		return Group{}, ErrGroupInvalid
	}
	repo := s.repo.withExecutor(db)
	current, err := repo.get(ctx, tenantID, id)
	if err != nil {
		return Group{}, err
	}
	if current.GroupType != GroupTypeStatic {
		return Group{}, ErrGroupInvalid
	}
	for _, principalID := range memberIDs {
		if err := s.requireProvisionedMember(ctx, db, tenantID, principalID); err != nil {
			return Group{}, err
		}
	}
	existing, err := repo.listMembers(ctx, tenantID, id)
	if err != nil {
		return Group{}, err
	}
	wanted := make(map[string]struct{}, len(memberIDs))
	for _, principalID := range memberIDs {
		wanted[principalID] = struct{}{}
	}
	removesMember := false
	for _, member := range existing {
		if _, ok := wanted[member.PrincipalID]; !ok {
			removesMember = true
			break
		}
	}
	wantedStatus := provisionedGroupStatus(input.Active)
	var verify func() error
	if removesMember || current.Status == StatusActive && wantedStatus == StatusDisabled {
		verify, err = s.administrators.Protect(ctx, db, repo.dialect, tenantID)
		if err != nil {
			return Group{}, err
		}
	}
	now := s.now().UTC().UnixMilli()
	changed := current.Name != input.DisplayName || current.Status != wantedStatus || len(existing) != len(memberIDs) || removesMember
	updated := current
	if current.Name != input.DisplayName || current.Status != wantedStatus {
		updated.Name, updated.NameKey, updated.Status = input.DisplayName, nameKey(input.DisplayName), wantedStatus
		updated.Version, updated.UpdatedAt = current.Version+1, now
		if err := repo.update(ctx, &updated, current.Version); err != nil {
			return Group{}, err
		}
	}
	for _, member := range existing {
		if _, ok := wanted[member.PrincipalID]; ok {
			delete(wanted, member.PrincipalID)
			continue
		}
		if _, err := repo.deleteMember(ctx, tenantID, id, member.PrincipalID); err != nil {
			return Group{}, err
		}
	}
	for principalID := range wanted {
		if err := repo.putMember(ctx, &groupMemberRow{TenantID: tenantID, GroupID: id, PrincipalID: principalID, CreatedAt: now, UpdatedAt: now}); err != nil {
			return Group{}, err
		}
		changed = true
	}
	if verify != nil {
		if err := verify(); err != nil {
			return Group{}, err
		}
	}
	if changed {
		if err := policyx.Advance(ctx, db, repo.dialect, tenantID, now); err != nil {
			return Group{}, err
		}
	}
	return groupFromRow(updated), nil
}

func (s *GroupService) DisableProvisionedTo(ctx context.Context, db bun.IDB, tenantID, id string) (Group, error) {
	tenantID, id = trimPair(tenantID, id)
	if db == nil || !validTenant(tenantID) || !validID(id) {
		return Group{}, ErrGroupInvalid
	}
	repo := s.repo.withExecutor(db)
	current, err := repo.get(ctx, tenantID, id)
	if err != nil {
		return Group{}, err
	}
	if current.GroupType != GroupTypeStatic {
		return Group{}, ErrGroupInvalid
	}
	if current.Status == StatusDisabled {
		return groupFromRow(current), nil
	}
	verify, err := s.administrators.Protect(ctx, db, repo.dialect, tenantID)
	if err != nil {
		return Group{}, err
	}
	now := s.now().UTC().UnixMilli()
	updated := current
	updated.Status, updated.Version, updated.UpdatedAt = StatusDisabled, current.Version+1, now
	if err := repo.update(ctx, &updated, current.Version); err != nil {
		return Group{}, err
	}
	if err := verify(); err != nil {
		return Group{}, err
	}
	if err := policyx.Advance(ctx, db, repo.dialect, tenantID, now); err != nil {
		return Group{}, err
	}
	return groupFromRow(updated), nil
}

func (s *GroupService) requireProvisionedMember(ctx context.Context, db bun.IDB, tenantID, principalID string) error {
	active, err := s.members.IsMemberActiveOn(ctx, db, tenantID, principalID)
	if err != nil {
		return fmt.Errorf("check provisioned group member: %w", err)
	}
	if !active {
		return ErrGroupMemberInactive
	}
	return nil
}

func normalizeProvisionedGroup(input ProvisionedGroupInput) (ProvisionedGroupInput, []string, error) {
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if !validName(input.DisplayName) || len(input.MemberIDs) > 1000 {
		return ProvisionedGroupInput{}, nil, ErrGroupInvalid
	}
	unique := make(map[string]struct{}, len(input.MemberIDs))
	members := make([]string, 0, len(input.MemberIDs))
	for _, value := range input.MemberIDs {
		value = strings.TrimSpace(value)
		if !validID(value) {
			return ProvisionedGroupInput{}, nil, ErrGroupInvalid
		}
		if _, ok := unique[value]; ok {
			continue
		}
		unique[value] = struct{}{}
		members = append(members, value)
	}
	input.MemberIDs = members
	return input, members, nil
}

func provisionedGroupStatus(active bool) string {
	if active {
		return StatusActive
	}
	return StatusDisabled
}

func errorsOr(err, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
}
