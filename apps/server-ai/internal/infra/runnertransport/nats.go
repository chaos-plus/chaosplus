package runnertransport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	machine "github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/machine/protocol"
	"github.com/nats-io/nats.go"
)

const (
	commandSubjectFormat = "chaos.runner.route.%s.%s.cmd"
	eventSubject         = "chaos.runner.event"
)

type NATS struct{ connection *nats.Conn }

func NewNATS(connection *nats.Conn) *NATS {
	if connection == nil {
		panic("runner transport requires NATS connection")
	}
	return &NATS{connection: connection}
}

func commandSubject(route machine.RouteLease) string {
	return fmt.Sprintf(commandSubjectFormat, route.HolderID, route.MachineID)
}

func (t *NATS) SubscribeCommands(ctx context.Context, route machine.RouteLease, handler machine.CommandHandler) (io.Closer, error) {
	subscription, err := t.connection.Subscribe(commandSubject(route), func(message *nats.Msg) {
		var envelope machine.CommandEnvelope
		if err := json.Unmarshal(message.Data, &envelope); err != nil || !envelope.Route.SameFence(route) {
			_ = message.Respond([]byte(`{"reply":{"ok":false,"error":"invalid command envelope"}}`))
			return
		}
		handler(envelope, func(reply machine.ReplyEnvelope) error {
			payload, err := json.Marshal(reply)
			if err != nil {
				return err
			}
			return message.Respond(payload)
		})
	})
	if err != nil {
		return nil, fmt.Errorf("subscribe runner commands: %w", err)
	}
	if err := t.connection.FlushWithContext(ctx); err != nil {
		_ = subscription.Unsubscribe()
		return nil, fmt.Errorf("activate runner command subscription: %w", err)
	}
	return subscriptionCloser{subscription}, nil
}

func (t *NATS) Request(ctx context.Context, route machine.RouteLease, command machine.Command) (machine.ReplyEnvelope, error) {
	payload, err := json.Marshal(machine.CommandEnvelope{Route: route, Command: command})
	if err != nil {
		return machine.ReplyEnvelope{}, fmt.Errorf("encode runner command: %w", err)
	}
	message, err := t.connection.RequestWithContext(ctx, commandSubject(route), payload)
	if err != nil {
		return machine.ReplyEnvelope{}, fmt.Errorf("request runner %s: %w", route.MachineID, err)
	}
	var reply machine.ReplyEnvelope
	if err := json.Unmarshal(message.Data, &reply); err != nil {
		return machine.ReplyEnvelope{}, fmt.Errorf("decode runner reply: %w", err)
	}
	if !reply.Route.SameFence(route) {
		return machine.ReplyEnvelope{}, fmt.Errorf("runner reply was fenced")
	}
	return reply, nil
}

func (t *NATS) PublishEvent(_ context.Context, envelope machine.EventEnvelope) error {
	payload, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("encode runner event: %w", err)
	}
	if err := t.connection.Publish(eventSubject, payload); err != nil {
		return fmt.Errorf("publish runner event: %w", err)
	}
	return nil
}

func (t *NATS) SubscribeEvents(ctx context.Context, handler machine.EventHandler) (io.Closer, error) {
	subscription, err := t.connection.Subscribe(eventSubject, func(message *nats.Msg) {
		var envelope machine.EventEnvelope
		if json.Unmarshal(message.Data, &envelope) == nil {
			handler(envelope)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("subscribe runner events: %w", err)
	}
	if err := t.connection.FlushWithContext(ctx); err != nil {
		_ = subscription.Unsubscribe()
		return nil, fmt.Errorf("activate runner event subscription: %w", err)
	}
	return subscriptionCloser{subscription}, nil
}

type subscriptionCloser struct{ subscription *nats.Subscription }

func (c subscriptionCloser) Close() error { return c.subscription.Unsubscribe() }

var _ machine.ClusterTransport = (*NATS)(nil)
