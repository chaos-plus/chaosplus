package requirement

import (
	"errors"
	"strings"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

var (
	ErrInvalid         = errors.New("requirement invalid")
	ErrNotFound        = errors.New("requirement not found")
	ErrVersionConflict = errors.New("requirement version conflict")
)

type Status string

const (
	StatusDraft    Status = "draft"
	StatusApproved Status = "approved"
	StatusRejected Status = "rejected"
	StatusClosed   Status = "closed"
)

type Requirement struct {
	bun.BaseModel      `bun:"table:workspace_requirements"`
	ID                 guid.ID   `bun:"id,pk" json:"id"`
	TenantID           guid.ID   `bun:"tenant_id,notnull" json:"tenantId"`
	EntityID           guid.ID   `bun:"entity_id,notnull" json:"entityId"`
	OwnerID            guid.ID   `bun:"owner_id,notnull" json:"ownerId"`
	ParentID           *guid.ID  `bun:"parent_id" json:"parentId,omitempty"`
	Title              string    `bun:"title,notnull" json:"title"`
	Description        string    `bun:"description,notnull" json:"description"`
	AcceptanceCriteria string    `bun:"acceptance_criteria,notnull" json:"acceptanceCriteria"`
	Status             Status    `bun:"status,notnull" json:"status"`
	CreatedAt          int64     `bun:"created_at,notnull" json:"createdAt"`
	CreatedBy          guid.ID   `bun:"created_by,notnull" json:"createdBy"`
	UpdatedAt          int64     `bun:"updated_at,notnull" json:"updatedAt"`
	UpdatedBy          guid.ID   `bun:"updated_by,notnull" json:"updatedBy"`
	DeletedAt          int64     `bun:"deleted_at,notnull" json:"-"`
	DeletedBy          guid.ID   `bun:"deleted_by,notnull" json:"-"`
	Version            int64     `bun:"version,notnull" json:"version"`
	KeyResultIDs       []guid.ID `bun:"-" json:"keyResultIds"`
}

type keyResultLink struct {
	bun.BaseModel `bun:"table:workspace_requirement_key_results"`
	TenantID      guid.ID `bun:"tenant_id,pk"`
	EntityID      guid.ID `bun:"entity_id,pk"`
	RequirementID guid.ID `bun:"requirement_id,pk"`
	KeyResultID   guid.ID `bun:"key_result_id,pk"`
	CreatedAt     int64   `bun:"created_at,notnull"`
	CreatedBy     guid.ID `bun:"created_by,notnull"`
}

type CreateInput struct {
	ParentID           *guid.ID  `json:"parentId,omitempty"`
	Title              string    `json:"title"`
	Description        string    `json:"description" required:"false"`
	AcceptanceCriteria string    `json:"acceptanceCriteria" required:"false"`
	KeyResultIDs       []guid.ID `json:"keyResultIds" required:"false"`
}

type UpdateInput struct {
	Title              *string    `json:"title,omitempty"`
	Description        *string    `json:"description,omitempty"`
	AcceptanceCriteria *string    `json:"acceptanceCriteria,omitempty"`
	Status             *Status    `json:"status,omitempty"`
	OwnerID            *guid.ID   `json:"ownerId,omitempty"`
	KeyResultIDs       *[]guid.ID `json:"keyResultIds,omitempty"`
	Version            int64      `json:"version"`
}

func validate(value *Requirement) error {
	value.Title = strings.TrimSpace(value.Title)
	value.Description = strings.TrimSpace(value.Description)
	value.AcceptanceCriteria = strings.TrimSpace(value.AcceptanceCriteria)
	if value.Title == "" || len(value.Title) > 300 || len(value.Description) > 65535 || len(value.AcceptanceCriteria) > 65535 {
		return ErrInvalid
	}
	if value.Status == "" {
		value.Status = StatusDraft
	}
	switch value.Status {
	case StatusDraft, StatusApproved, StatusRejected, StatusClosed:
		return nil
	default:
		return ErrInvalid
	}
}
