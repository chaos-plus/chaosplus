package message

import (
	"context"
	"fmt"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/conversation/channel"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

type ChannelAccess interface {
	Get(context.Context, guid.ID) (*channel.Channel, error)
	IsMember(context.Context, guid.ID, guid.ID, channel.MemberKind) (bool, error)
}

type AttachmentDirectory interface {
	ValidateConversationReferences(context.Context, guid.ID, []guid.ID) error
}

type Publisher interface{ Publish(Message) }

type Service struct {
	repository  Repository
	channels    ChannelAccess
	attachments AttachmentDirectory
	publisher   Publisher
	nextID      func() (guid.ID, error)
}

func NewService(repository Repository, channels ChannelAccess, attachments AttachmentDirectory, publisher Publisher, nextID func() (guid.ID, error)) *Service {
	if repository == nil || channels == nil || attachments == nil || publisher == nil || nextID == nil {
		panic("message service requires repository, channel access, attachment directory, publisher, and id generator")
	}
	return &Service{repository: repository, channels: channels, attachments: attachments, publisher: publisher, nextID: nextID}
}

func (s *Service) Post(ctx context.Context, channelID guid.ID, input MessageCreateInput) (*Message, error) {
	payload, err := validateInput(&input)
	if err != nil || channelID.Zero() {
		return nil, ErrInvalid
	}
	if _, err := s.channels.Get(ctx, channelID); err != nil {
		return nil, ErrNotFound
	}
	principalID := authn.PrincipalIDFromContext(ctx)
	member, err := s.channels.IsMember(ctx, channelID, principalID, channel.MemberHuman)
	if err != nil {
		return nil, err
	}
	if !member {
		return nil, ErrForbidden
	}
	if err := s.attachments.ValidateConversationReferences(ctx, channelID, payload.AttachmentIDs); err != nil {
		return nil, ErrInvalid
	}
	messageID, err := s.nextID()
	if err != nil {
		return nil, fmt.Errorf("generate message id: %w", err)
	}
	eventID, err := s.nextID()
	if err != nil {
		return nil, fmt.Errorf("generate message event id: %w", err)
	}
	payloadJSON, err := encodePayload(payload)
	if err != nil {
		return nil, ErrInvalid
	}
	value := &Message{ID: messageID, ChannelID: channelID, AuthorID: principalID, AuthorKind: AuthorHuman, IdempotencyKey: input.IdempotencyKey, PayloadJSON: payloadJSON}
	if err := s.repository.Append(ctx, value, eventID); err != nil {
		return nil, err
	}
	s.publisher.Publish(*value)
	return value, nil
}

func (s *Service) List(ctx context.Context, channelID guid.ID, afterSeq int64, limit int) ([]Message, error) {
	if _, err := s.channels.Get(ctx, channelID); err != nil {
		return nil, ErrNotFound
	}
	principalID := authn.PrincipalIDFromContext(ctx)
	member, err := s.channels.IsMember(ctx, channelID, principalID, channel.MemberHuman)
	if err != nil {
		return nil, err
	}
	if !member {
		return nil, ErrForbidden
	}
	return s.repository.List(ctx, channelID, afterSeq, limit)
}
