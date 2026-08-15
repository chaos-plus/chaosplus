package objective

import (
	"errors"
	"regexp"
	"strings"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

var (
	ErrInvalid         = errors.New("objective invalid")
	ErrNotFound        = errors.New("objective not found")
	ErrVersionConflict = errors.New("objective version conflict")
	ErrStateConflict   = errors.New("objective state conflict")
)

var decimalPattern = regexp.MustCompile(`^(?:0|[1-9][0-9]{0,13})(?:\.[0-9]{1,6})?$`)

type Status string

const (
	StatusDraft     Status = "draft"
	StatusActive    Status = "active"
	StatusCompleted Status = "completed"
	StatusCancelled Status = "cancelled"
)

type Objective struct {
	bun.BaseModel `bun:"table:workspace_objectives"`
	ID            guid.ID     `bun:"id,pk" json:"id"`
	TenantID      guid.ID     `bun:"tenant_id,notnull" json:"tenantId"`
	EntityID      guid.ID     `bun:"entity_id,notnull" json:"entityId"`
	OwnerID       guid.ID     `bun:"owner_id,notnull" json:"ownerId"`
	Title         string      `bun:"title,notnull" json:"title"`
	Description   string      `bun:"description,notnull" json:"description"`
	PeriodStart   int64       `bun:"period_start,notnull" json:"periodStart"`
	PeriodEnd     int64       `bun:"period_end,notnull" json:"periodEnd"`
	Status        Status      `bun:"status,notnull" json:"status"`
	KeyResults    []KeyResult `bun:"-" json:"keyResults"`
	CreatedAt     int64       `bun:"created_at,notnull" json:"createdAt"`
	CreatedBy     guid.ID     `bun:"created_by,notnull" json:"createdBy"`
	UpdatedAt     int64       `bun:"updated_at,notnull" json:"updatedAt"`
	UpdatedBy     guid.ID     `bun:"updated_by,notnull" json:"updatedBy"`
	DeletedAt     int64       `bun:"deleted_at,notnull" json:"-"`
	DeletedBy     guid.ID     `bun:"deleted_by,notnull" json:"-"`
	Version       int64       `bun:"version,notnull" json:"version"`
}

type KeyResult struct {
	bun.BaseModel `bun:"table:workspace_key_results"`
	ID            guid.ID `bun:"id,pk" json:"id"`
	TenantID      guid.ID `bun:"tenant_id,notnull" json:"tenantId"`
	EntityID      guid.ID `bun:"entity_id,notnull" json:"entityId"`
	OwnerID       guid.ID `bun:"owner_id,notnull" json:"ownerId"`
	ObjectiveID   guid.ID `bun:"objective_id,notnull" json:"objectiveId"`
	Title         string  `bun:"title,notnull" json:"title"`
	TargetValue   string  `bun:"target_value,notnull" json:"targetValue"`
	CurrentValue  string  `bun:"current_value,notnull" json:"currentValue"`
	Unit          string  `bun:"unit,notnull" json:"unit"`
	CreatedAt     int64   `bun:"created_at,notnull" json:"createdAt"`
	CreatedBy     guid.ID `bun:"created_by,notnull" json:"createdBy"`
	UpdatedAt     int64   `bun:"updated_at,notnull" json:"updatedAt"`
	UpdatedBy     guid.ID `bun:"updated_by,notnull" json:"updatedBy"`
	DeletedAt     int64   `bun:"deleted_at,notnull" json:"-"`
	DeletedBy     guid.ID `bun:"deleted_by,notnull" json:"-"`
	Version       int64   `bun:"version,notnull" json:"version"`
}

type KeyResultInput struct {
	ID           *guid.ID `json:"id,omitempty"`
	Title        string   `json:"title"`
	TargetValue  string   `json:"targetValue"`
	CurrentValue string   `json:"currentValue"`
	Unit         string   `json:"unit"`
}

type CreateInput struct {
	Title       string           `json:"title"`
	Description string           `json:"description" required:"false"`
	PeriodStart int64            `json:"periodStart"`
	PeriodEnd   int64            `json:"periodEnd"`
	KeyResults  []KeyResultInput `json:"keyResults" required:"false"`
}

type UpdateInput struct {
	Title       *string           `json:"title,omitempty"`
	Description *string           `json:"description,omitempty"`
	PeriodStart *int64            `json:"periodStart,omitempty"`
	PeriodEnd   *int64            `json:"periodEnd,omitempty"`
	Status      *Status           `json:"status,omitempty"`
	KeyResults  *[]KeyResultInput `json:"keyResults,omitempty"`
	OwnerID     *guid.ID          `json:"ownerId,omitempty"`
	Version     int64             `json:"version"`
}

func validate(value *Objective) error {
	value.Title = strings.TrimSpace(value.Title)
	value.Description = strings.TrimSpace(value.Description)
	if value.Title == "" || len(value.Title) > 300 || len(value.Description) > 65535 || value.PeriodStart <= 0 || value.PeriodEnd < value.PeriodStart {
		return ErrInvalid
	}
	if value.Status == "" {
		value.Status = StatusDraft
	}
	switch value.Status {
	case StatusDraft, StatusActive, StatusCompleted, StatusCancelled:
		return nil
	default:
		return ErrInvalid
	}
}

func validateKeyResults(inputs []KeyResultInput) error {
	if len(inputs) > 100 {
		return ErrInvalid
	}
	for i := range inputs {
		inputs[i].Title = strings.TrimSpace(inputs[i].Title)
		inputs[i].Unit = strings.TrimSpace(inputs[i].Unit)
		if inputs[i].Title == "" || len(inputs[i].Title) > 300 || len(inputs[i].Unit) > 32 ||
			!decimalPattern.MatchString(inputs[i].TargetValue) || !decimalPattern.MatchString(inputs[i].CurrentValue) {
			return ErrInvalid
		}
	}
	return nil
}
