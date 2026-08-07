package organization

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/policyx"
)

const (
	GroupTypeStatic           = "static"
	GroupTypeDynamic          = "dynamic"
	maxGroupDescriptionLength = 1024
)

var (
	ErrGroupInvalid           = errors.New("invalid group")
	ErrGroupNotFound          = errors.New("group not found")
	ErrGroupNameConflict      = errors.New("group name already exists")
	ErrGroupVersionConflict   = errors.New("group version conflict")
	ErrGroupHasMembers        = errors.New("group has members")
	ErrGroupRoleBound         = errors.New("group is assigned to a role")
	ErrGroupRelationshipBound = errors.New("group is referenced by a relationship")
	ErrGroupMemberInactive    = errors.New("group member is not an active tenant member")
	ErrGroupMemberNotFound    = errors.New("group member not found")
	ErrGroupRuleInvalid       = errors.New("invalid dynamic group membership rule")
	ErrGroupRuleType          = errors.New("membership rules require a dynamic group")
	ErrDynamicGroupMembers    = errors.New("dynamic group membership is computed from its rule")
)

type MembershipRule json.RawMessage

func (rule MembershipRule) MarshalJSON() ([]byte, error) {
	return json.RawMessage(rule).MarshalJSON()
}

func (rule *MembershipRule) UnmarshalJSON(data []byte) error {
	var raw json.RawMessage
	if err := raw.UnmarshalJSON(data); err != nil {
		return err
	}
	*rule = MembershipRule(raw)
	return nil
}

type Group struct {
	ID          string         `json:"id"`
	TenantID    string         `json:"tenant_id"`
	Name        string         `json:"name"`
	Type        string         `json:"type"`
	Rule        MembershipRule `json:"membership_rule,omitempty"`
	Description string         `json:"description,omitempty"`
	Status      string         `json:"status"`
	SortOrder   int            `json:"sort_order"`
	Version     int64          `json:"version"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type CreateGroup struct {
	Name        string
	Type        string
	Rule        MembershipRule
	Description string
	Status      string
	SortOrder   int
}

type UpdateGroup struct {
	Name        *string
	Description *string
	Status      *string
	SortOrder   *int
	Rule        *MembershipRule
	Version     int64
}

type GroupMember struct {
	GroupID     string     `json:"group_id"`
	PrincipalID string     `json:"principal_id"`
	DisplayName string     `json:"display_name"`
	Email       string     `json:"email,omitempty"`
	StartsAt    *time.Time `json:"starts_at,omitempty"`
	EndsAt      *time.Time `json:"ends_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type GroupMemberWindow = MembershipWindow

func normalizeGroupCreate(tenantID string, input CreateGroup) (string, CreateGroup, error) {
	tenantID = strings.TrimSpace(tenantID)
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	if input.Type == "" {
		input.Type = GroupTypeStatic
	}
	if input.Status == "" {
		input.Status = StatusActive
	}
	if !validTenant(tenantID) || !validName(input.Name) || (input.Type != GroupTypeStatic && input.Type != GroupTypeDynamic) || len(input.Description) > maxGroupDescriptionLength || !validStatus(input.Status) || !validSortOrder(input.SortOrder) {
		return "", CreateGroup{}, ErrGroupInvalid
	}
	if input.Type == GroupTypeStatic {
		if len(strings.TrimSpace(string(input.Rule))) > 0 {
			return "", CreateGroup{}, ErrGroupRuleType
		}
		input.Rule = nil
		return tenantID, input, nil
	}
	canonical, err := policyx.CanonicalMemberRule(json.RawMessage(input.Rule))
	if err != nil {
		return "", CreateGroup{}, ErrGroupRuleInvalid
	}
	input.Rule = MembershipRule(canonical)
	return tenantID, input, nil
}

func normalizeGroupUpdate(tenantID, id string, input UpdateGroup) (string, string, UpdateGroup, error) {
	tenantID, id = trimPair(tenantID, id)
	if !validTenant(tenantID) || !validID(id) || input.Version < 1 || (input.Name == nil && input.Description == nil && input.Status == nil && input.SortOrder == nil && input.Rule == nil) {
		return "", "", UpdateGroup{}, ErrGroupInvalid
	}
	if input.Name != nil {
		value := strings.TrimSpace(*input.Name)
		if !validName(value) {
			return "", "", UpdateGroup{}, ErrGroupInvalid
		}
		input.Name = &value
	}
	if input.Description != nil {
		value := strings.TrimSpace(*input.Description)
		if len(value) > maxGroupDescriptionLength {
			return "", "", UpdateGroup{}, ErrGroupInvalid
		}
		input.Description = &value
	}
	if input.Status != nil && !validStatus(*input.Status) {
		return "", "", UpdateGroup{}, ErrGroupInvalid
	}
	if input.SortOrder != nil && !validSortOrder(*input.SortOrder) {
		return "", "", UpdateGroup{}, ErrGroupInvalid
	}
	if input.Rule != nil {
		canonical, err := policyx.CanonicalMemberRule(json.RawMessage(*input.Rule))
		if err != nil {
			return "", "", UpdateGroup{}, ErrGroupRuleInvalid
		}
		value := MembershipRule(canonical)
		input.Rule = &value
	}
	return tenantID, id, input, nil
}

func normalizeGroupMember(tenantID, groupID, principalID string, input GroupMemberWindow) (string, string, string, GroupMemberWindow, error) {
	tenantID, groupID = trimPair(tenantID, groupID)
	principalID = strings.TrimSpace(principalID)
	if !validTenant(tenantID) || !validID(groupID) || principalID == "" || len(principalID) > 255 {
		return "", "", "", GroupMemberWindow{}, ErrGroupInvalid
	}
	input.StartsAt = normalizeOptionalTime(input.StartsAt)
	input.EndsAt = normalizeOptionalTime(input.EndsAt)
	if (input.StartsAt != nil && input.StartsAt.IsZero()) || (input.EndsAt != nil && input.EndsAt.IsZero()) || (input.StartsAt != nil && input.EndsAt != nil && !input.EndsAt.After(*input.StartsAt)) {
		return "", "", "", GroupMemberWindow{}, ErrGroupInvalid
	}
	return tenantID, groupID, principalID, input, nil
}
