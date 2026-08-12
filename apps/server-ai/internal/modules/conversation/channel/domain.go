package channel

import (
	"errors"
	"strings"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

type Status string
type MemberKind string
type MemberRole string

const (
	StatusActive   Status = "active"
	StatusArchived Status = "archived"

	MemberHuman MemberKind = "human"
	MemberAgent MemberKind = "agent"

	RoleOwner  MemberRole = "owner"
	RoleMember MemberRole = "member"
)

var (
	ErrInvalid         = errors.New("channel invalid")
	ErrNotFound        = errors.New("channel not found")
	ErrForbidden       = errors.New("channel operation forbidden")
	ErrVersionConflict = errors.New("channel version conflict")
	ErrMemberConflict  = errors.New("channel member conflict")
)

type Channel struct {
	bun.BaseModel `bun:"table:conversation_channels"`
	ID            guid.ID `bun:"id,pk" json:"id"`
	TenantID      guid.ID `bun:"tenant_id,notnull" json:"tenantId"`
	EntityID      guid.ID `bun:"entity_id,notnull" json:"entityId"`
	ProjectID     guid.ID `bun:"project_id,notnull" json:"projectId"`
	OwnerID       guid.ID `bun:"owner_id,notnull" json:"ownerId"`
	Name          string  `bun:"name,notnull" json:"name"`
	Topic         string  `bun:"topic,notnull" json:"topic"`
	Status        Status  `bun:"status,notnull" json:"status"`
	CreatedAt     int64   `bun:"created_at,notnull" json:"createdAt"`
	CreatedBy     guid.ID `bun:"created_by,notnull" json:"createdBy"`
	UpdatedAt     int64   `bun:"updated_at,notnull" json:"updatedAt"`
	UpdatedBy     guid.ID `bun:"updated_by,notnull" json:"updatedBy"`
	DeletedAt     int64   `bun:"deleted_at,notnull" json:"-"`
	DeletedBy     guid.ID `bun:"deleted_by,notnull" json:"-"`
	Version       int64   `bun:"version,notnull" json:"version"`
}

type Member struct {
	bun.BaseModel `bun:"table:conversation_channel_members"`
	ID            guid.ID    `bun:"id,pk" json:"id"`
	TenantID      guid.ID    `bun:"tenant_id,notnull" json:"tenantId"`
	EntityID      guid.ID    `bun:"entity_id,notnull" json:"entityId"`
	ChannelID     guid.ID    `bun:"channel_id,notnull" json:"channelId"`
	MemberID      guid.ID    `bun:"member_id,notnull" json:"memberId"`
	Kind          MemberKind `bun:"kind,notnull" json:"kind"`
	Role          MemberRole `bun:"role,notnull" json:"role"`
	JoinedAt      int64      `bun:"joined_at,notnull" json:"joinedAt"`
	JoinedBy      guid.ID    `bun:"joined_by,notnull" json:"joinedBy"`
	RemovedAt     int64      `bun:"removed_at,notnull" json:"removedAt"`
	RemovedBy     guid.ID    `bun:"removed_by,notnull" json:"removedBy"`
	Version       int64      `bun:"version,notnull" json:"version"`
}

type ChannelCreateInput struct {
	ProjectID guid.ID `json:"projectId"`
	Name      string  `json:"name"`
	Topic     string  `json:"topic,omitempty"`
}

type ChannelUpdateInput struct {
	Name    *string `json:"name,omitempty"`
	Topic   *string `json:"topic,omitempty"`
	Status  *Status `json:"status,omitempty"`
	Version int64   `json:"version"`
}

type AddMemberInput struct {
	MemberID guid.ID    `json:"memberId"`
	Kind     MemberKind `json:"kind"`
}

func validateChannel(value *Channel) error {
	value.Name = strings.TrimSpace(value.Name)
	value.Topic = strings.TrimSpace(value.Topic)
	if value.ProjectID.Zero() || value.Name == "" || len(value.Name) > 120 || len(value.Topic) > 1000 {
		return ErrInvalid
	}
	if value.Status == "" {
		value.Status = StatusActive
	}
	if value.Status != StatusActive && value.Status != StatusArchived {
		return ErrInvalid
	}
	return nil
}
