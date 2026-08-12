package message

import (
	"context"
	"errors"
	"testing"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/conversation/channel"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

func TestSQLiteMessageEventProjectionLifecycle(t *testing.T) {
	db, err := bunxtest.Memory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := authn.WithClaims(context.Background(), &authn.Claims{TenantID: 11, EntityID: 21, PrincipalID: 31})
	if err := channel.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	next := guid.ID(100)
	nextID := func() (guid.ID, error) {
		next++
		return next, nil
	}
	channelRepository := channel.NewRepository(db, nextID)
	conversationChannel := &channel.Channel{ProjectID: 41, Name: "Delivery", Status: channel.StatusActive}
	if err := channelRepository.Create(ctx, conversationChannel, new(channel.Member)); err != nil {
		t.Fatal(err)
	}
	repository := NewRepository(db, nextID)
	payload, err := encodePayload(Payload{Text: "ship it"})
	if err != nil {
		t.Fatal(err)
	}
	first := &Message{ID: 201, ChannelID: conversationChannel.ID, AuthorID: 31, AuthorKind: AuthorHuman, IdempotencyKey: "request-1", PayloadJSON: payload}
	if err := repository.Append(ctx, first, 301); err != nil {
		t.Fatal(err)
	}
	if first.Seq < 1 || first.EventID != 301 || string(first.Payload) == "" {
		t.Fatalf("first message = %+v", first)
	}
	retry := &Message{ID: 202, ChannelID: conversationChannel.ID, AuthorID: 31, AuthorKind: AuthorHuman, IdempotencyKey: "request-1", PayloadJSON: payload}
	if err := repository.Append(ctx, retry, 302); err != nil {
		t.Fatal(err)
	}
	if retry.ID != first.ID || retry.EventID != first.EventID || retry.Seq != first.Seq {
		t.Fatalf("idempotent retry = %+v, want original %+v", retry, first)
	}
	tamperedPayload, _ := encodePayload(Payload{Text: "different"})
	tampered := &Message{ID: 203, ChannelID: conversationChannel.ID, AuthorID: 31, AuthorKind: AuthorHuman, IdempotencyKey: "request-1", PayloadJSON: tamperedPayload}
	if err := repository.Append(ctx, tampered, 303); !errors.Is(err, ErrConflict) {
		t.Fatalf("tampered retry = %v", err)
	}
	if _, err := db.NewDelete().Table("conversation_messages").Where("1 = 1").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repository.RebuildProjectionIfInconsistent(ctx); err != nil {
		t.Fatal(err)
	}
	items, err := repository.List(ctx, conversationChannel.ID, 0, 10)
	if err != nil || len(items) != 1 || items[0].ID != first.ID || items[0].PayloadJSON != payload {
		t.Fatalf("rebuilt messages = (%+v, %v)", items, err)
	}
}
