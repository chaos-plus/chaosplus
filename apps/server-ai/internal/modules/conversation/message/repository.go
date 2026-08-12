package message

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

type Repository interface {
	Append(context.Context, *Message, guid.ID) error
	List(context.Context, guid.ID, int64, int) ([]Message, error)
}

type BunRepository struct {
	db     *bun.DB
	nextID func() (guid.ID, error)
}

type eventPayload struct {
	MessageID  guid.ID         `json:"messageId"`
	AuthorKind AuthorKind      `json:"authorKind"`
	Payload    json.RawMessage `json:"payload"`
}

func NewRepository(db *bun.DB, nextID func() (guid.ID, error)) *BunRepository {
	if db == nil || nextID == nil {
		panic("message repository requires database and id generator")
	}
	return &BunRepository{db: db, nextID: nextID}
}

func (r *BunRepository) Append(ctx context.Context, value *Message, eventID guid.ID) error {
	tenantID, entityID, principalID := authn.TenantIDFromContext(ctx), authn.EntityIDFromContext(ctx), authn.PrincipalIDFromContext(ctx)
	if tenantID.Zero() || entityID.Zero() || principalID.Zero() || value.ID.Zero() || eventID.Zero() || value.ChannelID.Zero() || value.AuthorID.Zero() || value.PayloadJSON == "" {
		return ErrInvalid
	}
	value.TenantID, value.EntityID, value.EventID = tenantID, entityID, eventID
	value.CreatedAt = time.Now().UTC().UnixMilli()
	eventBody, err := json.Marshal(eventPayload{MessageID: value.ID, AuthorKind: value.AuthorKind, Payload: json.RawMessage(value.PayloadJSON)})
	if err != nil {
		return ErrInvalid
	}
	event := Event{ID: eventID, TenantID: tenantID, EntityID: entityID, ChannelID: value.ChannelID, ActorID: principalID,
		Type: "CHANNEL_MESSAGE_POSTED", IdempotencyKey: value.IdempotencyKey, PayloadJSON: string(eventBody), CreatedAt: value.CreatedAt}
	err = r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		insert := tx.NewInsert().Model(&event)
		if r.db.Dialect().Name().String() == "pg" {
			insert = insert.Returning("seq")
		}
		result, err := insert.Exec(ctx)
		if err != nil {
			return fmt.Errorf("append conversation message event: %w", err)
		}
		if event.Seq == 0 {
			event.Seq, err = result.LastInsertId()
			if err != nil {
				return fmt.Errorf("resolve conversation message sequence: %w", err)
			}
		}
		value.Seq = event.Seq
		if _, err := tx.NewInsert().Model(value).Exec(ctx); err != nil {
			return fmt.Errorf("project conversation message: %w", err)
		}
		return nil
	})
	if err != nil {
		var existing Message
		lookupErr := r.db.NewSelect().Model(&existing).
			Where("tenant_id = ? AND entity_id = ? AND channel_id = ? AND idempotency_key = ?", tenantID, entityID, value.ChannelID, value.IdempotencyKey).Scan(ctx)
		if lookupErr == nil {
			if existing.AuthorID == value.AuthorID && existing.AuthorKind == value.AuthorKind && existing.PayloadJSON == value.PayloadJSON {
				*value = existing
				return hydrate(value)
			}
			return ErrConflict
		}
		return err
	}
	return hydrate(value)
}

func (r *BunRepository) RebuildProjectionIfInconsistent(ctx context.Context) error {
	events, err := r.db.NewSelect().Model((*Event)(nil)).Count(ctx)
	if err != nil {
		return fmt.Errorf("count conversation message events: %w", err)
	}
	messages, err := r.db.NewSelect().Model((*Message)(nil)).Count(ctx)
	if err != nil {
		return fmt.Errorf("count conversation message projection: %w", err)
	}
	if events == messages {
		return nil
	}
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		items := []Event{}
		if err := tx.NewSelect().Model(&items).Order("seq ASC").Scan(ctx); err != nil {
			return fmt.Errorf("load conversation message events: %w", err)
		}
		if _, err := tx.NewDelete().Table("conversation_messages").Where("1 = 1").Exec(ctx); err != nil {
			return fmt.Errorf("clear conversation message projection: %w", err)
		}
		for _, event := range items {
			var payload eventPayload
			if event.Type != "CHANNEL_MESSAGE_POSTED" || json.Unmarshal([]byte(event.PayloadJSON), &payload) != nil || payload.MessageID.Zero() || !json.Valid(payload.Payload) {
				return fmt.Errorf("invalid conversation message event %s", event.ID)
			}
			value := Message{Seq: event.Seq, ID: payload.MessageID, EventID: event.ID, TenantID: event.TenantID, EntityID: event.EntityID,
				ChannelID: event.ChannelID, AuthorID: event.ActorID, AuthorKind: payload.AuthorKind, IdempotencyKey: event.IdempotencyKey,
				PayloadJSON: string(payload.Payload), CreatedAt: event.CreatedAt}
			if _, err := tx.NewInsert().Model(&value).Exec(ctx); err != nil {
				return fmt.Errorf("rebuild conversation message %s: %w", value.ID, err)
			}
		}
		return nil
	})
}

func (r *BunRepository) List(ctx context.Context, channelID guid.ID, afterSeq int64, limit int) ([]Message, error) {
	tenantID, entityID := authn.TenantIDFromContext(ctx), authn.EntityIDFromContext(ctx)
	if tenantID.Zero() || entityID.Zero() || channelID.Zero() || afterSeq < 0 || limit < 1 || limit > 500 {
		return nil, ErrInvalid
	}
	items := []Message{}
	err := r.db.NewSelect().Model(&items).
		Where("tenant_id = ? AND entity_id = ? AND channel_id = ? AND seq > ?", tenantID, entityID, channelID, afterSeq).
		Order("seq ASC").Limit(limit).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return []Message{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list conversation messages: %w", err)
	}
	for index := range items {
		if err := hydrate(&items[index]); err != nil {
			return nil, fmt.Errorf("decode conversation message %s: %w", items[index].ID, err)
		}
	}
	return items, nil
}

func encodePayload(payload Payload) (string, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
