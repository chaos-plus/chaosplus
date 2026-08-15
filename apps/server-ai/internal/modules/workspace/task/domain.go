package task

import (
	"errors"
	"strings"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

var (
	ErrInvalid         = errors.New("task invalid")
	ErrNotFound        = errors.New("task not found")
	ErrVersionConflict = errors.New("task version conflict")
	ErrStateConflict   = errors.New("task state conflict")
)

type Status string
type Priority string

const (
	StatusOpen       Status = "open"
	StatusInProgress Status = "in_progress"
	StatusReview     Status = "review"
	StatusDone       Status = "done"
	StatusCancelled  Status = "cancelled"

	PriorityHighest Priority = "highest"
	PriorityHigh    Priority = "high"
	PriorityMedium  Priority = "medium"
	PriorityLow     Priority = "low"
	PriorityLowest  Priority = "lowest"
)

type Task struct {
	bun.BaseModel `bun:"table:workspace_tasks"`
	ID            guid.ID  `bun:"id,pk" json:"id"`
	TenantID      guid.ID  `bun:"tenant_id,notnull" json:"tenantId"`
	EntityID      guid.ID  `bun:"entity_id,notnull" json:"entityId"`
	OwnerID       guid.ID  `bun:"owner_id,notnull" json:"ownerId"`
	RequirementID *guid.ID `bun:"requirement_id" json:"requirementId,omitempty"`
	ParentID      *guid.ID `bun:"parent_id" json:"parentId,omitempty"`
	Title         string   `bun:"title,notnull" json:"title"`
	Description   string   `bun:"description,notnull" json:"description"`
	Status        Status   `bun:"status,notnull" json:"status"`
	Priority      Priority `bun:"priority,notnull" json:"priority"`
	DueAt         int64    `bun:"due_at,notnull" json:"dueAt"`
	EstimateMS    int64    `bun:"estimate_ms,notnull" json:"estimateMs"`
	SpentMS       int64    `bun:"spent_ms,notnull" json:"spentMs"`
	Progress      int      `bun:"progress,notnull" json:"progress"`
	WorkflowRunID *guid.ID `bun:"workflow_run_id" json:"workflowRunId,omitempty"`
	WorkflowID    *guid.ID `bun:"workflow_id" json:"workflowId,omitempty"`
	ProjectID     *guid.ID `bun:"project_id" json:"projectId,omitempty"`
	Workspace     string   `bun:"workspace,notnull" json:"workspace"`
	AssigneeID    *guid.ID `bun:"assignee_id" json:"assigneeId,omitempty"`
	ChannelID     *guid.ID `bun:"channel_id" json:"channelId,omitempty"`
	CreatedAt     int64    `bun:"created_at,notnull" json:"createdAt"`
	CreatedBy     guid.ID  `bun:"created_by,notnull" json:"createdBy"`
	UpdatedAt     int64    `bun:"updated_at,notnull" json:"updatedAt"`
	UpdatedBy     guid.ID  `bun:"updated_by,notnull" json:"updatedBy"`
	DeletedAt     int64    `bun:"deleted_at,notnull" json:"-"`
	DeletedBy     guid.ID  `bun:"deleted_by,notnull" json:"-"`
	Version       int64    `bun:"version,notnull" json:"version"`
}

type CreateInput struct {
	RequirementID *guid.ID `json:"requirementId,omitempty"`
	ParentID      *guid.ID `json:"parentId,omitempty"`
	Title         string   `json:"title"`
	Description   string   `json:"description" required:"false"`
	Priority      Priority `json:"priority" required:"false"`
	DueAt         int64    `json:"dueAt" required:"false"`
	EstimateMS    int64    `json:"estimateMs" required:"false"`
	AssigneeID    *guid.ID `json:"assigneeId,omitempty"`
	ChannelID     *guid.ID `json:"channelId,omitempty"`
	WorkflowID    *guid.ID `json:"workflowId,omitempty"`
	ProjectID     *guid.ID `json:"projectId,omitempty"`
	Workspace     string   `json:"workspace,omitempty"`
}

type UpdateInput struct {
	Title       *string   `json:"title,omitempty"`
	Description *string   `json:"description,omitempty"`
	Status      *Status   `json:"status,omitempty"`
	Priority    *Priority `json:"priority,omitempty"`
	DueAt       *int64    `json:"dueAt,omitempty"`
	EstimateMS  *int64    `json:"estimateMs,omitempty"`
	AssigneeID  *guid.ID  `json:"assigneeId,omitempty"`
	OwnerID     *guid.ID  `json:"ownerId,omitempty"`
	WorkflowID  *guid.ID  `json:"workflowId,omitempty"`
	ProjectID   *guid.ID  `json:"projectId,omitempty"`
	Workspace   *string   `json:"workspace,omitempty"`
	Version     int64     `json:"version"`
}

func validate(value *Task) error {
	value.Title = strings.TrimSpace(value.Title)
	value.Description = strings.TrimSpace(value.Description)
	value.Workspace = strings.TrimSpace(value.Workspace)
	if value.Title == "" || len(value.Title) > 300 || len(value.Description) > 65535 || value.DueAt < 0 || value.EstimateMS < 0 || value.SpentMS < 0 || value.Progress < 0 || value.Progress > 100 {
		return ErrInvalid
	}
	if len(value.Workspace) > 1024 || (value.WorkflowID == nil) != (value.ProjectID == nil) || (value.WorkflowID == nil && value.Workspace != "") || (value.WorkflowID != nil && value.Workspace == "") {
		return ErrInvalid
	}
	if value.Status == "" {
		value.Status = StatusOpen
	}
	if value.Priority == "" {
		value.Priority = PriorityMedium
	}
	switch value.Status {
	case StatusOpen, StatusInProgress, StatusReview, StatusDone, StatusCancelled:
	default:
		return ErrInvalid
	}
	switch value.Priority {
	case PriorityHighest, PriorityHigh, PriorityMedium, PriorityLow, PriorityLowest:
	default:
		return ErrInvalid
	}
	return nil
}
