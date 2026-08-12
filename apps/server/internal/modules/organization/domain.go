package organization

import (
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
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
	ID        guid.ID   `json:"id"`
	TenantID  guid.ID   `json:"tenant_id"`
	ParentID  guid.ID   `json:"parent_id,omitempty"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	SortOrder int       `json:"sort_order"`
	Depth     int       `json:"depth"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateDepartment struct {
	ParentID  guid.ID
	Name      string
	Status    string
	SortOrder int
}

type UpdateDepartment struct {
	ParentID  *guid.ID
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

func normalizeCreate(tenantID guid.ID, input CreateDepartment) (guid.ID, CreateDepartment, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Status == "" {
		input.Status = StatusActive
	}
	if tenantID.Zero() || !validOptionalID(input.ParentID) || !validName(input.Name) || !validStatus(input.Status) || !validSortOrder(input.SortOrder) {
		return 0, CreateDepartment{}, ErrInvalid
	}
	return tenantID, input, nil
}

func normalizeUpdate(tenantID, id guid.ID, input UpdateDepartment) (guid.ID, guid.ID, UpdateDepartment, error) {
	if tenantID.Zero() || id.Zero() || input.Version < 1 || (input.ParentID == nil && input.Name == nil && input.Status == nil && input.SortOrder == nil) {
		return 0, 0, UpdateDepartment{}, ErrInvalid
	}
	if input.ParentID != nil && !validOptionalID(*input.ParentID) {
		return 0, 0, UpdateDepartment{}, ErrInvalid
	}
	if input.Name != nil {
		value := strings.TrimSpace(*input.Name)
		if !validName(value) {
			return 0, 0, UpdateDepartment{}, ErrInvalid
		}
		input.Name = &value
	}
	if input.Status != nil && !validStatus(*input.Status) {
		return 0, 0, UpdateDepartment{}, ErrInvalid
	}
	if input.SortOrder != nil && !validSortOrder(*input.SortOrder) {
		return 0, 0, UpdateDepartment{}, ErrInvalid
	}
	return tenantID, id, input, nil
}

func validID(value guid.ID) bool         { return value > 0 }
func validOptionalID(value guid.ID) bool { return value == 0 || value > 0 }
func validName(value string) bool {
	return value != "" && len(value) <= 128 && strings.IndexFunc(value, unicode.IsControl) < 0
}
func validStatus(value string) bool {
	return value == StatusActive || value == StatusDisabled
}
func validSortOrder(value int) bool { return value >= 0 && value <= maxSortOrder }
func nameKey(value string) string   { return strings.ToLower(strings.TrimSpace(value)) }
