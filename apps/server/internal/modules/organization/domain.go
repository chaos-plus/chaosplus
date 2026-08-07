package organization

import (
	"errors"
	"strings"
	"time"
	"unicode"
)

const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
	maxSortOrder   = 1_000_000
)

var (
	ErrInvalid          = errors.New("invalid department")
	ErrNotFound         = errors.New("department not found")
	ErrNameConflict     = errors.New("department name already exists under parent")
	ErrHierarchyCycle   = errors.New("department hierarchy cycle")
	ErrHierarchyCorrupt = errors.New("department hierarchy is corrupt")
	ErrHasChildren      = errors.New("department has children")
	ErrInUse            = errors.New("department is in use")
	ErrVersionConflict  = errors.New("department version conflict")
)

// Department is a tenant-owned node in the administrative organization tree.
// Business entities such as companies, merchants, and stores are separate
// concepts and do not belong in this hierarchy.
type Department struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	ParentID  string    `json:"parent_id,omitempty"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	SortOrder int       `json:"sort_order"`
	Depth     int       `json:"depth"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateDepartment struct {
	ParentID  string
	Name      string
	Status    string
	SortOrder int
}

type UpdateDepartment struct {
	ParentID  *string
	Name      *string
	Status    *string
	SortOrder *int
	Version   int64
}

// MembershipWindow is shared by tenant-owned directory relationships. Nil
// boundaries mean immediately effective or no expiry.
type MembershipWindow struct {
	StartsAt *time.Time
	EndsAt   *time.Time
}

func normalizeCreate(tenantID string, input CreateDepartment) (string, CreateDepartment, error) {
	tenantID = strings.TrimSpace(tenantID)
	input.ParentID = strings.TrimSpace(input.ParentID)
	input.Name = strings.TrimSpace(input.Name)
	if input.Status == "" {
		input.Status = StatusActive
	}
	if !validTenant(tenantID) || !validOptionalID(input.ParentID) || !validName(input.Name) || !validStatus(input.Status) || !validSortOrder(input.SortOrder) {
		return "", CreateDepartment{}, ErrInvalid
	}
	return tenantID, input, nil
}

func normalizeUpdate(tenantID, id string, input UpdateDepartment) (string, string, UpdateDepartment, error) {
	tenantID, id = strings.TrimSpace(tenantID), strings.TrimSpace(id)
	if !validTenant(tenantID) || !validID(id) || input.Version < 1 || (input.ParentID == nil && input.Name == nil && input.Status == nil && input.SortOrder == nil) {
		return "", "", UpdateDepartment{}, ErrInvalid
	}
	if input.ParentID != nil {
		value := strings.TrimSpace(*input.ParentID)
		if !validOptionalID(value) {
			return "", "", UpdateDepartment{}, ErrInvalid
		}
		input.ParentID = &value
	}
	if input.Name != nil {
		value := strings.TrimSpace(*input.Name)
		if !validName(value) {
			return "", "", UpdateDepartment{}, ErrInvalid
		}
		input.Name = &value
	}
	if input.Status != nil && !validStatus(*input.Status) {
		return "", "", UpdateDepartment{}, ErrInvalid
	}
	if input.SortOrder != nil && !validSortOrder(*input.SortOrder) {
		return "", "", UpdateDepartment{}, ErrInvalid
	}
	return tenantID, id, input, nil
}

func validTenant(value string) bool { return value != "" && len(value) <= 128 }
func validID(value string) bool     { return value != "" && len(value) <= 128 }
func validOptionalID(value string) bool {
	return value == "" || validID(value)
}
func validName(value string) bool {
	return value != "" && len(value) <= 128 && strings.IndexFunc(value, unicode.IsControl) < 0
}
func validStatus(value string) bool {
	return value == StatusActive || value == StatusDisabled
}
func validSortOrder(value int) bool { return value >= 0 && value <= maxSortOrder }
func nameKey(value string) string   { return strings.ToLower(strings.TrimSpace(value)) }
