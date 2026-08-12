// Package workspace owns the collaboration workspace bounded context.
package workspace

import (
	"errors"
	"strings"
)

var (
	ErrInvalid  = errors.New("invalid workspace input")
	ErrNotFound = errors.New("workspace resource not found")
	ErrConflict = errors.New("workspace resource conflict")
)

type WorkItem struct {
	ID            string  `json:"id"`
	Type          string  `json:"type"`
	Title         string  `json:"title"`
	Description   string  `json:"description"`
	Status        string  `json:"status"`
	ParentID      string  `json:"parentId"`
	EstimateHours float64 `json:"estimateHours"`
	SpentHours    float64 `json:"spentHours"`
	Progress      int     `json:"progress"`
	WorkflowRunID string  `json:"workflowRunId"`
	AssigneeAgent string  `json:"assigneeAgent"`
	ChannelID     string  `json:"channelId"`
	EntityID      string  `json:"entityId"`
	CreatedAt     int64   `json:"createdAt"`
	UpdatedAt     int64   `json:"updatedAt"`
}

type WorkItemPatch struct {
	Type          *string  `json:"type"`
	Title         *string  `json:"title"`
	Description   *string  `json:"description"`
	Status        *string  `json:"status"`
	ParentID      *string  `json:"parentId"`
	EstimateHours *float64 `json:"estimateHours"`
	SpentHours    *float64 `json:"spentHours"`
	Progress      *int     `json:"progress"`
	WorkflowRunID *string  `json:"workflowRunId"`
	AssigneeAgent *string  `json:"assigneeAgent"`
	ChannelID     *string  `json:"channelId"`
}

type Attachment struct {
	ID        string `json:"id"`
	OwnerType string `json:"ownerType"`
	OwnerID   string `json:"ownerId"`
	Filename  string `json:"filename"`
	Mime      string `json:"mime"`
	SizeBytes int64  `json:"sizeBytes"`
	StorePath string `json:"-"`
	EntityID  string `json:"entityId"`
	CreatedAt int64  `json:"createdAt"`
}

type Okr struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Objective  string `json:"objective"`
	Period     string `json:"period"`
	KeyResults string `json:"keyResults"`
	EntityID   string `json:"entityId"`
	CreatedAt  int64  `json:"createdAt"`
	UpdatedAt  int64  `json:"updatedAt"`
}

func normalizeWorkItem(item WorkItem) (WorkItem, error) {
	item.Title = strings.TrimSpace(item.Title)
	item.Type = strings.TrimSpace(item.Type)
	item.Status = strings.TrimSpace(item.Status)
	if item.Title == "" || len(item.Title) > 300 || item.EstimateHours < 0 || item.SpentHours < 0 || item.Progress < 0 || item.Progress > 100 {
		return WorkItem{}, ErrInvalid
	}
	if item.Type == "" {
		item.Type = "task"
	}
	if !oneOf(item.Type, "requirement", "task", "test", "bug") {
		return WorkItem{}, ErrInvalid
	}
	if item.Status == "" {
		item.Status = "open"
	}
	if !oneOf(item.Status, "open", "in_progress", "review", "done") {
		return WorkItem{}, ErrInvalid
	}
	return item, nil
}

func applyWorkItemPatch(item WorkItem, patch WorkItemPatch) (WorkItem, error) {
	if patch.Type != nil {
		item.Type = *patch.Type
	}
	if patch.Title != nil {
		item.Title = *patch.Title
	}
	if patch.Description != nil {
		item.Description = *patch.Description
	}
	if patch.Status != nil {
		item.Status = *patch.Status
	}
	if patch.ParentID != nil {
		if *patch.ParentID == item.ID {
			return WorkItem{}, ErrInvalid
		}
		item.ParentID = *patch.ParentID
	}
	if patch.EstimateHours != nil {
		item.EstimateHours = *patch.EstimateHours
	}
	if patch.SpentHours != nil {
		item.SpentHours = *patch.SpentHours
	}
	if patch.Progress != nil {
		item.Progress = *patch.Progress
	}
	if patch.WorkflowRunID != nil {
		item.WorkflowRunID = *patch.WorkflowRunID
	}
	if patch.AssigneeAgent != nil {
		item.AssigneeAgent = *patch.AssigneeAgent
	}
	if patch.ChannelID != nil {
		item.ChannelID = *patch.ChannelID
	}
	return normalizeWorkItem(item)
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
