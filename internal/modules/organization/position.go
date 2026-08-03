package organization

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
)

const maxPositionCodeLength = 64

var (
	positionCodePattern          = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)
	ErrPositionInvalid           = errors.New("invalid position")
	ErrPositionNotFound          = errors.New("position not found")
	ErrPositionCodeConflict      = errors.New("position code already exists")
	ErrPositionVersionConflict   = errors.New("position version conflict")
	ErrPositionHasMembers        = errors.New("position has members")
	ErrPositionRoleBound         = errors.New("position is assigned to a role")
	ErrPositionRelationshipBound = errors.New("position is referenced by a relationship")
	ErrPositionMemberInactive    = errors.New("position member is not an active tenant member")
	ErrPositionMemberNotFound    = errors.New("position member not found")
)

type Position struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	SortOrder int       `json:"sort_order"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreatePosition struct {
	Code      string
	Name      string
	Status    string
	SortOrder int
}

type UpdatePosition struct {
	Code      *string
	Name      *string
	Status    *string
	SortOrder *int
	Version   int64
}

type PositionMember struct {
	PositionID  string     `json:"position_id"`
	PrincipalID string     `json:"principal_id"`
	DisplayName string     `json:"display_name"`
	Email       string     `json:"email,omitempty"`
	StartsAt    *time.Time `json:"starts_at,omitempty"`
	EndsAt      *time.Time `json:"ends_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type PositionMemberWindow = MembershipWindow

func normalizePositionCreate(tenantID string, input CreatePosition) (string, CreatePosition, error) {
	tenantID = strings.TrimSpace(tenantID)
	input.Code = normalizePositionCode(input.Code)
	input.Name = strings.TrimSpace(input.Name)
	if input.Status == "" {
		input.Status = StatusActive
	}
	if !validTenant(tenantID) || !validPositionCode(input.Code) || !validPositionName(input.Name) || !validStatus(input.Status) || !validSortOrder(input.SortOrder) {
		return "", CreatePosition{}, ErrPositionInvalid
	}
	return tenantID, input, nil
}

func normalizePositionUpdate(tenantID, id string, input UpdatePosition) (string, string, UpdatePosition, error) {
	tenantID, id = trimPair(tenantID, id)
	if !validTenant(tenantID) || !validID(id) || input.Version < 1 || (input.Code == nil && input.Name == nil && input.Status == nil && input.SortOrder == nil) {
		return "", "", UpdatePosition{}, ErrPositionInvalid
	}
	if input.Code != nil {
		value := normalizePositionCode(*input.Code)
		if !validPositionCode(value) {
			return "", "", UpdatePosition{}, ErrPositionInvalid
		}
		input.Code = &value
	}
	if input.Name != nil {
		value := strings.TrimSpace(*input.Name)
		if !validPositionName(value) {
			return "", "", UpdatePosition{}, ErrPositionInvalid
		}
		input.Name = &value
	}
	if input.Status != nil && !validStatus(*input.Status) {
		return "", "", UpdatePosition{}, ErrPositionInvalid
	}
	if input.SortOrder != nil && !validSortOrder(*input.SortOrder) {
		return "", "", UpdatePosition{}, ErrPositionInvalid
	}
	return tenantID, id, input, nil
}

func normalizePositionMember(tenantID, positionID, principalID string, input PositionMemberWindow) (string, string, string, PositionMemberWindow, error) {
	tenantID, positionID = trimPair(tenantID, positionID)
	principalID = strings.TrimSpace(principalID)
	if !validTenant(tenantID) || !validID(positionID) || principalID == "" || len(principalID) > 255 {
		return "", "", "", PositionMemberWindow{}, ErrPositionInvalid
	}
	input.StartsAt = normalizeOptionalTime(input.StartsAt)
	input.EndsAt = normalizeOptionalTime(input.EndsAt)
	if (input.StartsAt != nil && input.StartsAt.IsZero()) || (input.EndsAt != nil && input.EndsAt.IsZero()) || (input.StartsAt != nil && input.EndsAt != nil && !input.EndsAt.After(*input.StartsAt)) {
		return "", "", "", PositionMemberWindow{}, ErrPositionInvalid
	}
	return tenantID, positionID, principalID, input, nil
}

func normalizePositionCode(value string) string { return strings.ToLower(strings.TrimSpace(value)) }
func validPositionCode(value string) bool {
	return len(value) <= maxPositionCodeLength && positionCodePattern.MatchString(value)
}
func validPositionName(value string) bool {
	return value != "" && len(value) <= 128 && strings.IndexFunc(value, unicode.IsControl) < 0
}
func normalizeOptionalTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC().Truncate(time.Millisecond)
	return &normalized
}
