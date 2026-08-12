package attachment

import (
	"errors"
	"io"
	"mime"
	"path/filepath"
	"strings"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

const MaxUploadBytes int64 = 100 << 20

var (
	ErrInvalid         = errors.New("attachment invalid")
	ErrNotFound        = errors.New("attachment not found")
	ErrVersionConflict = errors.New("attachment version conflict")
	ErrUnavailable     = errors.New("attachment unavailable")
)

type ResourceType string
type Status string

const (
	ResourceRequirement  ResourceType = "requirement"
	ResourceTask         ResourceType = "task"
	ResourceObjective    ResourceType = "objective"
	ResourceConversation ResourceType = "conversation"

	StatusAvailable    Status = "available"
	StatusDeleting     Status = "deleting"
	StatusDeleteFailed Status = "delete_failed"
)

type Attachment struct {
	ID           guid.ID      `bun:"id,pk" json:"id"`
	TenantID     guid.ID      `bun:"tenant_id,notnull" json:"tenantId"`
	EntityID     guid.ID      `bun:"entity_id,notnull" json:"entityId"`
	OwnerID      guid.ID      `bun:"owner_id,notnull" json:"ownerId"`
	ResourceType ResourceType `bun:"resource_type,notnull" json:"resourceType"`
	ResourceID   guid.ID      `bun:"resource_id,notnull" json:"resourceId"`
	Filename     string       `bun:"filename,notnull" json:"filename"`
	ContentType  string       `bun:"content_type,notnull" json:"contentType"`
	SizeBytes    int64        `bun:"size_bytes,notnull" json:"sizeBytes"`
	Checksum     string       `bun:"checksum,notnull" json:"checksum"`
	ObjectKey    string       `bun:"object_key,notnull" json:"-"`
	Status       Status       `bun:"status,notnull" json:"status"`
	CreatedAt    int64        `bun:"created_at,notnull" json:"createdAt"`
	CreatedBy    guid.ID      `bun:"created_by,notnull" json:"createdBy"`
	UpdatedAt    int64        `bun:"updated_at,notnull" json:"updatedAt"`
	UpdatedBy    guid.ID      `bun:"updated_by,notnull" json:"updatedBy"`
	DeletedAt    int64        `bun:"deleted_at,notnull" json:"-"`
	DeletedBy    guid.ID      `bun:"deleted_by,notnull" json:"-"`
	Version      int64        `bun:"version,notnull" json:"version"`
}

type UploadInput struct {
	ResourceType ResourceType
	ResourceID   guid.ID
	Filename     string
	ContentType  string
	SizeBytes    int64
	Content      io.Reader
}

func validateUpload(input *UploadInput) error {
	input.Filename = filepath.Base(strings.TrimSpace(input.Filename))
	input.ContentType = strings.TrimSpace(input.ContentType)
	if input.ContentType == "" {
		input.ContentType = "application/octet-stream"
	}
	if input.ResourceID.Zero() || input.Content == nil || input.Filename == "" || input.Filename == "." || len(input.Filename) > 255 ||
		input.SizeBytes < 1 || input.SizeBytes > MaxUploadBytes || len(input.ContentType) > 127 {
		return ErrInvalid
	}
	if _, _, err := mime.ParseMediaType(input.ContentType); err != nil {
		return ErrInvalid
	}
	switch input.ResourceType {
	case ResourceRequirement, ResourceTask, ResourceObjective, ResourceConversation:
		return nil
	default:
		return ErrInvalid
	}
}
