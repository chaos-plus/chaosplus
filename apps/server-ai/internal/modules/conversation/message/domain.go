package message

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

type AuthorKind string

const (
	AuthorHuman    AuthorKind = "human"
	AuthorAgent    AuthorKind = "agent"
	AuthorWorkflow AuthorKind = "workflow"
)

var (
	ErrInvalid   = errors.New("message invalid")
	ErrNotFound  = errors.New("message channel not found")
	ErrForbidden = errors.New("message author is not a channel member")
	ErrConflict  = errors.New("message idempotency conflict")
)

type Message struct {
	bun.BaseModel  `bun:"table:conversation_messages"`
	Seq            int64           `bun:"seq,notnull" json:"seq"`
	ID             guid.ID         `bun:"id,pk" json:"id"`
	EventID        guid.ID         `bun:"event_id,notnull,unique" json:"eventId"`
	TenantID       guid.ID         `bun:"tenant_id,notnull" json:"tenantId"`
	EntityID       guid.ID         `bun:"entity_id,notnull" json:"entityId"`
	ChannelID      guid.ID         `bun:"channel_id,notnull" json:"channelId"`
	AuthorID       guid.ID         `bun:"author_id,notnull" json:"authorId"`
	AuthorKind     AuthorKind      `bun:"author_kind,notnull" json:"authorKind"`
	IdempotencyKey string          `bun:"idempotency_key,notnull" json:"-"`
	Payload        json.RawMessage `bun:"-" json:"payload"`
	PayloadJSON    string          `bun:"payload_json,notnull" json:"-"`
	CreatedAt      int64           `bun:"created_at,notnull" json:"createdAt"`
}

type Event struct {
	bun.BaseModel  `bun:"table:conversation_message_events"`
	Seq            int64   `bun:"seq,pk,autoincrement"`
	ID             guid.ID `bun:"id,notnull,unique"`
	TenantID       guid.ID `bun:"tenant_id,notnull"`
	EntityID       guid.ID `bun:"entity_id,notnull"`
	ChannelID      guid.ID `bun:"channel_id,notnull"`
	ActorID        guid.ID `bun:"actor_id,notnull"`
	Type           string  `bun:"type,notnull"`
	IdempotencyKey string  `bun:"idempotency_key,notnull"`
	PayloadJSON    string  `bun:"payload_json,notnull"`
	CreatedAt      int64   `bun:"created_at,notnull"`
}

type Payload struct {
	Text          string    `json:"text,omitempty"`
	AttachmentIDs []guid.ID `json:"attachmentIds,omitempty"`
	RunID         guid.ID   `json:"runId,omitempty"`
	ReviewNode    string    `json:"reviewNode,omitempty"`
}

type CreateInput struct {
	IdempotencyKey string    `json:"idempotencyKey"`
	Text           string    `json:"text,omitempty"`
	AttachmentIDs  []guid.ID `json:"attachmentIds,omitempty"`
}

func validateInput(input *CreateInput) (Payload, error) {
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.Text = strings.TrimSpace(input.Text)
	if input.IdempotencyKey == "" || len(input.IdempotencyKey) > 128 || len(input.Text) > 65535 || (input.Text == "" && len(input.AttachmentIDs) == 0) || len(input.AttachmentIDs) > 20 {
		return Payload{}, ErrInvalid
	}
	seen := make(map[guid.ID]struct{}, len(input.AttachmentIDs))
	for _, id := range input.AttachmentIDs {
		if id.Zero() {
			return Payload{}, ErrInvalid
		}
		if _, exists := seen[id]; exists {
			return Payload{}, ErrInvalid
		}
		seen[id] = struct{}{}
	}
	return Payload{Text: input.Text, AttachmentIDs: input.AttachmentIDs}, nil
}

func hydrate(value *Message) error {
	value.Payload = json.RawMessage(value.PayloadJSON)
	if !json.Valid(value.Payload) {
		return ErrInvalid
	}
	return nil
}
