package testrun

import (
	"errors"
	"strings"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

var (
	ErrInvalid         = errors.New("test run invalid")
	ErrNotFound        = errors.New("test run not found")
	ErrVersionConflict = errors.New("test run version conflict")
	ErrStateConflict   = errors.New("test run state conflict")
)

type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusPassed    Status = "passed"
	StatusFailed    Status = "failed"
	StatusBlocked   Status = "blocked"
	StatusCancelled Status = "cancelled"
)

type TestRun struct {
	bun.BaseModel  `bun:"table:workspace_test_runs"`
	ID             guid.ID  `bun:"id,pk" json:"id"`
	TenantID       guid.ID  `bun:"tenant_id,notnull" json:"tenantId"`
	EntityID       guid.ID  `bun:"entity_id,notnull" json:"entityId"`
	OwnerID        guid.ID  `bun:"owner_id,notnull" json:"ownerId"`
	TestCaseID     guid.ID  `bun:"test_case_id,notnull" json:"testCaseId"`
	ExecutorID     guid.ID  `bun:"executor_id,notnull" json:"executorId"`
	WorkflowRunID  *guid.ID `bun:"workflow_run_id" json:"workflowRunId,omitempty"`
	Environment    string   `bun:"environment,notnull" json:"environment"`
	Status         Status   `bun:"status,notnull" json:"status"`
	StartedAt      int64    `bun:"started_at,notnull" json:"startedAt"`
	CompletedAt    int64    `bun:"completed_at,notnull" json:"completedAt"`
	ObservedResult string   `bun:"observed_result,notnull" json:"observedResult"`
	FailureSummary string   `bun:"failure_summary,notnull" json:"failureSummary"`
	CreatedAt      int64    `bun:"created_at,notnull" json:"createdAt"`
	CreatedBy      guid.ID  `bun:"created_by,notnull" json:"createdBy"`
	UpdatedAt      int64    `bun:"updated_at,notnull" json:"updatedAt"`
	UpdatedBy      guid.ID  `bun:"updated_by,notnull" json:"updatedBy"`
	Version        int64    `bun:"version,notnull" json:"version"`
}

type CreateInput struct {
	TestCaseID    guid.ID  `json:"testCaseId"`
	WorkflowRunID *guid.ID `json:"workflowRunId,omitempty"`
	Environment   string   `json:"environment"`
}

type UpdateInput struct {
	Status         Status  `json:"status"`
	ObservedResult *string `json:"observedResult,omitempty"`
	FailureSummary *string `json:"failureSummary,omitempty"`
	Version        int64   `json:"version"`
}

func validate(value *TestRun) error {
	value.Environment = strings.TrimSpace(value.Environment)
	value.ObservedResult = strings.TrimSpace(value.ObservedResult)
	value.FailureSummary = strings.TrimSpace(value.FailureSummary)
	if value.TestCaseID.Zero() || value.ExecutorID.Zero() || value.Environment == "" || len(value.Environment) > 300 || len(value.ObservedResult) > 65535 || len(value.FailureSummary) > 65535 {
		return ErrInvalid
	}
	if value.WorkflowRunID != nil && value.WorkflowRunID.Zero() {
		return ErrInvalid
	}
	switch value.Status {
	case StatusQueued, StatusRunning, StatusPassed, StatusFailed, StatusBlocked, StatusCancelled:
	default:
		return ErrInvalid
	}
	if value.Status == StatusFailed && value.FailureSummary == "" {
		return ErrInvalid
	}
	if terminal(value.Status) && value.CompletedAt <= 0 {
		return ErrInvalid
	}
	if value.Status == StatusRunning && value.StartedAt <= 0 {
		return ErrInvalid
	}
	return nil
}

func terminal(status Status) bool {
	return status == StatusPassed || status == StatusFailed || status == StatusBlocked || status == StatusCancelled
}
func validTransition(from, to Status) bool {
	return from == StatusQueued && (to == StatusRunning || to == StatusCancelled) || from == StatusRunning && terminal(to)
}
