package testcase

import (
	"errors"
	"strings"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

var (
	ErrInvalid         = errors.New("test case invalid")
	ErrNotFound        = errors.New("test case not found")
	ErrVersionConflict = errors.New("test case version conflict")
	ErrStateConflict   = errors.New("test case state conflict")
)

type Status string
type Priority string

const (
	StatusDraft   Status = "draft"
	StatusActive  Status = "active"
	StatusRetired Status = "retired"

	PriorityHighest Priority = "highest"
	PriorityHigh    Priority = "high"
	PriorityMedium  Priority = "medium"
	PriorityLow     Priority = "low"
	PriorityLowest  Priority = "lowest"
)

type TestCase struct {
	bun.BaseModel `bun:"table:workspace_test_cases"`
	ID            guid.ID  `bun:"id,pk" json:"id"`
	TenantID      guid.ID  `bun:"tenant_id,notnull" json:"tenantId"`
	EntityID      guid.ID  `bun:"entity_id,notnull" json:"entityId"`
	OwnerID       guid.ID  `bun:"owner_id,notnull" json:"ownerId"`
	RequirementID *guid.ID `bun:"requirement_id" json:"requirementId,omitempty"`
	Title         string   `bun:"title,notnull" json:"title"`
	Description   string   `bun:"description,notnull" json:"description"`
	Preconditions string   `bun:"preconditions,notnull" json:"preconditions"`
	Priority      Priority `bun:"priority,notnull" json:"priority"`
	Status        Status   `bun:"status,notnull" json:"status"`
	AssigneeID    *guid.ID `bun:"assignee_id" json:"assigneeId,omitempty"`
	Steps         []Step   `bun:"-" json:"steps"`
	CreatedAt     int64    `bun:"created_at,notnull" json:"createdAt"`
	CreatedBy     guid.ID  `bun:"created_by,notnull" json:"createdBy"`
	UpdatedAt     int64    `bun:"updated_at,notnull" json:"updatedAt"`
	UpdatedBy     guid.ID  `bun:"updated_by,notnull" json:"updatedBy"`
	DeletedAt     int64    `bun:"deleted_at,notnull" json:"-"`
	DeletedBy     guid.ID  `bun:"deleted_by,notnull" json:"-"`
	Version       int64    `bun:"version,notnull" json:"version"`
}

type Step struct {
	bun.BaseModel  `bun:"table:workspace_test_steps"`
	ID             guid.ID `bun:"id,pk" json:"id"`
	TenantID       guid.ID `bun:"tenant_id,notnull" json:"tenantId"`
	EntityID       guid.ID `bun:"entity_id,notnull" json:"entityId"`
	TestCaseID     guid.ID `bun:"test_case_id,notnull" json:"testCaseId"`
	Position       int     `bun:"position,notnull" json:"position"`
	Action         string  `bun:"action,notnull" json:"action"`
	ExpectedResult string  `bun:"expected_result,notnull" json:"expectedResult"`
	CreatedAt      int64   `bun:"created_at,notnull" json:"createdAt"`
	CreatedBy      guid.ID `bun:"created_by,notnull" json:"createdBy"`
	UpdatedAt      int64   `bun:"updated_at,notnull" json:"updatedAt"`
	UpdatedBy      guid.ID `bun:"updated_by,notnull" json:"updatedBy"`
	DeletedAt      int64   `bun:"deleted_at,notnull" json:"-"`
	DeletedBy      guid.ID `bun:"deleted_by,notnull" json:"-"`
	Version        int64   `bun:"version,notnull" json:"version"`
}

type StepInput struct {
	Action         string `json:"action"`
	ExpectedResult string `json:"expectedResult"`
}

type CreateInput struct {
	RequirementID *guid.ID    `json:"requirementId,omitempty"`
	Title         string      `json:"title"`
	Description   string      `json:"description" required:"false"`
	Preconditions string      `json:"preconditions" required:"false"`
	Priority      Priority    `json:"priority" required:"false"`
	AssigneeID    *guid.ID    `json:"assigneeId,omitempty"`
	Steps         []StepInput `json:"steps"`
}

type UpdateInput struct {
	Title         *string      `json:"title,omitempty"`
	Description   *string      `json:"description,omitempty"`
	Preconditions *string      `json:"preconditions,omitempty"`
	Priority      *Priority    `json:"priority,omitempty"`
	Status        *Status      `json:"status,omitempty"`
	AssigneeID    *guid.ID     `json:"assigneeId,omitempty"`
	OwnerID       *guid.ID     `json:"ownerId,omitempty"`
	Steps         *[]StepInput `json:"steps,omitempty"`
	Version       int64        `json:"version"`
}

func validate(value *TestCase, steps []StepInput) error {
	value.Title = strings.TrimSpace(value.Title)
	value.Description = strings.TrimSpace(value.Description)
	value.Preconditions = strings.TrimSpace(value.Preconditions)
	if value.Title == "" || len(value.Title) > 300 || len(value.Description) > 65535 || len(value.Preconditions) > 65535 || len(steps) > 100 || value.Status == StatusActive && len(steps) == 0 {
		return ErrInvalid
	}
	if value.Priority == "" {
		value.Priority = PriorityMedium
	}
	if value.Status == "" {
		value.Status = StatusDraft
	}
	switch value.Priority {
	case PriorityHighest, PriorityHigh, PriorityMedium, PriorityLow, PriorityLowest:
	default:
		return ErrInvalid
	}
	switch value.Status {
	case StatusDraft, StatusActive, StatusRetired:
	default:
		return ErrInvalid
	}
	for i := range steps {
		steps[i].Action = strings.TrimSpace(steps[i].Action)
		steps[i].ExpectedResult = strings.TrimSpace(steps[i].ExpectedResult)
		if steps[i].Action == "" || steps[i].ExpectedResult == "" || len(steps[i].Action) > 65535 || len(steps[i].ExpectedResult) > 65535 {
			return ErrInvalid
		}
	}
	return nil
}

func validTransition(from, to Status) bool {
	return from == to || from == StatusDraft && (to == StatusActive || to == StatusRetired) || from == StatusActive && to == StatusRetired
}
