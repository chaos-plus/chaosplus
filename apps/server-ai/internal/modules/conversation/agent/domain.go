package agent

import (
	"errors"
	"strings"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

type Kind string
type Status string

const (
	KindDigitalHuman Kind = "digital_human"
	KindExecutor     Kind = "executor"

	StatusRunning Status = "running"
	StatusStopped Status = "stopped"
	StatusRetired Status = "retired"
)

var (
	ErrInvalid         = errors.New("agent invalid")
	ErrNotFound        = errors.New("agent not found")
	ErrVersionConflict = errors.New("agent version conflict")
	ErrStateConflict   = errors.New("agent state conflict")
)

type Agent struct {
	ID           guid.ID `bun:"id,pk" json:"id"`
	TenantID     guid.ID `bun:"tenant_id,notnull" json:"tenantId"`
	EntityID     guid.ID `bun:"entity_id,notnull" json:"entityId"`
	OwnerID      guid.ID `bun:"owner_id,notnull" json:"ownerId"`
	MachineID    guid.ID `bun:"machine_id,notnull" json:"machineId"`
	Name         string  `bun:"name,notnull" json:"name"`
	Kind         Kind    `bun:"kind,notnull" json:"kind"`
	Runtime      string  `bun:"runtime,notnull" json:"runtime"`
	Model        string  `bun:"model,notnull" json:"model"`
	Provider     string  `bun:"provider,notnull" json:"provider"`
	SystemPrompt string  `bun:"system_prompt,notnull" json:"systemPrompt"`
	Description  string  `bun:"description,notnull" json:"description"`
	Status       Status  `bun:"status,notnull" json:"status"`
	HandoverDoc  string  `bun:"handover_doc,notnull" json:"handoverDoc"`
	RetiredAt    int64   `bun:"retired_at,notnull" json:"retiredAt"`
	CreatedAt    int64   `bun:"created_at,notnull" json:"createdAt"`
	CreatedBy    guid.ID `bun:"created_by,notnull" json:"createdBy"`
	UpdatedAt    int64   `bun:"updated_at,notnull" json:"updatedAt"`
	UpdatedBy    guid.ID `bun:"updated_by,notnull" json:"updatedBy"`
	DeletedAt    int64   `bun:"deleted_at,notnull" json:"-"`
	DeletedBy    guid.ID `bun:"deleted_by,notnull" json:"-"`
	Version      int64   `bun:"version,notnull" json:"version"`
}

type CreateInput struct {
	Name         string  `json:"name"`
	Kind         Kind    `json:"kind"`
	Runtime      string  `json:"runtime"`
	Model        string  `json:"model"`
	Provider     string  `json:"provider"`
	SystemPrompt string  `json:"systemPrompt"`
	Description  string  `json:"description"`
	MachineID    guid.ID `json:"machineId"`
}

type UpdateInput struct {
	Name         *string  `json:"name,omitempty"`
	Kind         *Kind    `json:"kind,omitempty"`
	Runtime      *string  `json:"runtime,omitempty"`
	Model        *string  `json:"model,omitempty"`
	Provider     *string  `json:"provider,omitempty"`
	SystemPrompt *string  `json:"systemPrompt,omitempty"`
	Description  *string  `json:"description,omitempty"`
	MachineID    *guid.ID `json:"machineId,omitempty"`
	Version      int64    `json:"version"`
}

func validate(value *Agent) error {
	value.Name = strings.TrimSpace(value.Name)
	value.Runtime = strings.TrimSpace(value.Runtime)
	value.Model = strings.TrimSpace(value.Model)
	value.Provider = strings.TrimSpace(value.Provider)
	value.SystemPrompt = strings.TrimSpace(value.SystemPrompt)
	value.Description = strings.TrimSpace(value.Description)
	if value.Name == "" || len(value.Name) > 200 || value.Runtime == "" || len(value.Runtime) > 64 ||
		len(value.Model) > 128 || len(value.Provider) > 128 || len(value.SystemPrompt) > 65535 || len(value.Description) > 4096 || value.MachineID.Zero() {
		return ErrInvalid
	}
	if value.Kind == "" {
		value.Kind = KindDigitalHuman
	}
	if value.Status == "" {
		value.Status = StatusStopped
	}
	if value.Kind != KindDigitalHuman && value.Kind != KindExecutor {
		return ErrInvalid
	}
	if value.Status != StatusRunning && value.Status != StatusStopped && value.Status != StatusRetired {
		return ErrInvalid
	}
	return nil
}
