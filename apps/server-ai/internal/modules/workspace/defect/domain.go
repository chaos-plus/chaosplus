package defect

import (
	"errors"
	"strings"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

var (
	ErrInvalid         = errors.New("defect invalid")
	ErrNotFound        = errors.New("defect not found")
	ErrVersionConflict = errors.New("defect version conflict")
	ErrStateConflict   = errors.New("defect state conflict")
)

type Status string
type Severity string
type Priority string
type Resolution string

const (
	StatusOpen                Status     = "open"
	StatusTriaged             Status     = "triaged"
	StatusInProgress          Status     = "in_progress"
	StatusResolved            Status     = "resolved"
	StatusVerified            Status     = "verified"
	StatusClosed              Status     = "closed"
	StatusReopened            Status     = "reopened"
	StatusRejected            Status     = "rejected"
	SeverityBlocker           Severity   = "blocker"
	SeverityCritical          Severity   = "critical"
	SeverityMajor             Severity   = "major"
	SeverityMinor             Severity   = "minor"
	SeverityTrivial           Severity   = "trivial"
	PriorityHighest           Priority   = "highest"
	PriorityHigh              Priority   = "high"
	PriorityMedium            Priority   = "medium"
	PriorityLow               Priority   = "low"
	PriorityLowest            Priority   = "lowest"
	ResolutionFixed           Resolution = "fixed"
	ResolutionDuplicate       Resolution = "duplicate"
	ResolutionCannotReproduce Resolution = "cannot_reproduce"
	ResolutionWontFix         Resolution = "wont_fix"
	ResolutionByDesign        Resolution = "by_design"
)

type Defect struct {
	bun.BaseModel     `bun:"table:workspace_defects"`
	ID                guid.ID    `bun:"id,pk" json:"id"`
	TenantID          guid.ID    `bun:"tenant_id,notnull" json:"tenantId"`
	EntityID          guid.ID    `bun:"entity_id,notnull" json:"entityId"`
	OwnerID           guid.ID    `bun:"owner_id,notnull" json:"ownerId"`
	RequirementID     *guid.ID   `bun:"requirement_id" json:"requirementId,omitempty"`
	TaskID            *guid.ID   `bun:"task_id" json:"taskId,omitempty"`
	TestCaseID        *guid.ID   `bun:"test_case_id" json:"testCaseId,omitempty"`
	TestRunID         *guid.ID   `bun:"test_run_id" json:"testRunId,omitempty"`
	Title             string     `bun:"title,notnull" json:"title"`
	Description       string     `bun:"description,notnull" json:"description"`
	ReproductionSteps string     `bun:"reproduction_steps,notnull" json:"reproductionSteps"`
	ExpectedResult    string     `bun:"expected_result,notnull" json:"expectedResult"`
	ActualResult      string     `bun:"actual_result,notnull" json:"actualResult"`
	Severity          Severity   `bun:"severity,notnull" json:"severity"`
	Priority          Priority   `bun:"priority,notnull" json:"priority"`
	Status            Status     `bun:"status,notnull" json:"status"`
	Resolution        Resolution `bun:"resolution,notnull" json:"resolution"`
	ResolutionNote    string     `bun:"resolution_note,notnull" json:"resolutionNote"`
	AssigneeID        *guid.ID   `bun:"assignee_id" json:"assigneeId,omitempty"`
	CreatedAt         int64      `bun:"created_at,notnull" json:"createdAt"`
	CreatedBy         guid.ID    `bun:"created_by,notnull" json:"createdBy"`
	UpdatedAt         int64      `bun:"updated_at,notnull" json:"updatedAt"`
	UpdatedBy         guid.ID    `bun:"updated_by,notnull" json:"updatedBy"`
	DeletedAt         int64      `bun:"deleted_at,notnull" json:"-"`
	DeletedBy         guid.ID    `bun:"deleted_by,notnull" json:"-"`
	Version           int64      `bun:"version,notnull" json:"version"`
}
type CreateInput struct {
	RequirementID     *guid.ID `json:"requirementId,omitempty"`
	TaskID            *guid.ID `json:"taskId,omitempty"`
	TestCaseID        *guid.ID `json:"testCaseId,omitempty"`
	TestRunID         *guid.ID `json:"testRunId,omitempty"`
	Title             string   `json:"title"`
	Description       string   `json:"description" required:"false"`
	ReproductionSteps string   `json:"reproductionSteps"`
	ExpectedResult    string   `json:"expectedResult"`
	ActualResult      string   `json:"actualResult"`
	Severity          Severity `json:"severity" required:"false"`
	Priority          Priority `json:"priority" required:"false"`
	AssigneeID        *guid.ID `json:"assigneeId,omitempty"`
}
type UpdateInput struct {
	Title             *string     `json:"title,omitempty"`
	Description       *string     `json:"description,omitempty"`
	ReproductionSteps *string     `json:"reproductionSteps,omitempty"`
	ExpectedResult    *string     `json:"expectedResult,omitempty"`
	ActualResult      *string     `json:"actualResult,omitempty"`
	Severity          *Severity   `json:"severity,omitempty"`
	Priority          *Priority   `json:"priority,omitempty"`
	Status            *Status     `json:"status,omitempty"`
	Resolution        *Resolution `json:"resolution,omitempty"`
	ResolutionNote    *string     `json:"resolutionNote,omitempty"`
	AssigneeID        *guid.ID    `json:"assigneeId,omitempty"`
	OwnerID           *guid.ID    `json:"ownerId,omitempty"`
	Version           int64       `json:"version"`
}

func validate(value *Defect) error {
	value.Title = strings.TrimSpace(value.Title)
	value.Description = strings.TrimSpace(value.Description)
	value.ReproductionSteps = strings.TrimSpace(value.ReproductionSteps)
	value.ExpectedResult = strings.TrimSpace(value.ExpectedResult)
	value.ActualResult = strings.TrimSpace(value.ActualResult)
	value.ResolutionNote = strings.TrimSpace(value.ResolutionNote)
	if value.Title == "" || len(value.Title) > 300 || value.ReproductionSteps == "" || value.ExpectedResult == "" || value.ActualResult == "" || len(value.Description) > 65535 || len(value.ReproductionSteps) > 65535 || len(value.ExpectedResult) > 65535 || len(value.ActualResult) > 65535 || len(value.ResolutionNote) > 65535 {
		return ErrInvalid
	}
	if value.RequirementID == nil && value.TaskID == nil && value.TestCaseID == nil && value.TestRunID == nil {
		return ErrInvalid
	}
	for _, id := range []*guid.ID{value.RequirementID, value.TaskID, value.TestCaseID, value.TestRunID, value.AssigneeID} {
		if id != nil && id.Zero() {
			return ErrInvalid
		}
	}
	if value.Status == "" {
		value.Status = StatusOpen
	}
	if value.Severity == "" {
		value.Severity = SeverityMajor
	}
	if value.Priority == "" {
		value.Priority = PriorityMedium
	}
	switch value.Status {
	case StatusOpen, StatusTriaged, StatusInProgress, StatusResolved, StatusVerified, StatusClosed, StatusReopened, StatusRejected:
	default:
		return ErrInvalid
	}
	switch value.Severity {
	case SeverityBlocker, SeverityCritical, SeverityMajor, SeverityMinor, SeverityTrivial:
	default:
		return ErrInvalid
	}
	switch value.Priority {
	case PriorityHighest, PriorityHigh, PriorityMedium, PriorityLow, PriorityLowest:
	default:
		return ErrInvalid
	}
	if value.Status == StatusResolved || value.Status == StatusVerified || value.Status == StatusClosed {
		switch value.Resolution {
		case ResolutionFixed, ResolutionDuplicate, ResolutionCannotReproduce, ResolutionWontFix, ResolutionByDesign:
		default:
			return ErrInvalid
		}
		if value.ResolutionNote == "" {
			return ErrInvalid
		}
	} else if value.Resolution != "" || value.ResolutionNote != "" {
		return ErrInvalid
	}
	return nil
}
func validTransition(from, to Status) bool {
	if from == to {
		return true
	}
	switch from {
	case StatusOpen:
		return to == StatusTriaged || to == StatusInProgress || to == StatusRejected
	case StatusTriaged:
		return to == StatusInProgress || to == StatusRejected
	case StatusInProgress, StatusReopened:
		return to == StatusResolved || to == StatusRejected
	case StatusResolved:
		return to == StatusVerified || to == StatusReopened
	case StatusVerified:
		return to == StatusClosed || to == StatusReopened
	default:
		return false
	}
}
