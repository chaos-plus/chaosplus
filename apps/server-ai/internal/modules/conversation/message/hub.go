package message

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/nats-io/nats.go"
)

const messageSubjectPrefix = "conversation.message."

type Hub struct {
	nc   *nats.Conn
	mu   sync.Mutex
	subs map[guid.ID]map[chan Message]struct{}
	sub  *nats.Subscription
}

func NewHub(nc *nats.Conn) *Hub {
	if nc == nil {
		panic("message hub requires NATS")
	}
	return &Hub{nc: nc, subs: make(map[guid.ID]map[chan Message]struct{})}
}

func (h *Hub) Start(ctx context.Context) error {
	sub, err := h.nc.Subscribe(messageSubjectPrefix+">", func(raw *nats.Msg) {
		var value Message
		if json.Unmarshal(raw.Data, &value) == nil && !value.ChannelID.Zero() && !value.ID.Zero() {
			h.publishLocal(value)
		}
	})
	if err != nil {
		return fmt.Errorf("subscribe conversation messages: %w", err)
	}
	activation, cancel := context.WithTimeout(ctx, activationTimeout)
	defer cancel()
	if err := h.nc.FlushWithContext(activation); err != nil {
		_ = sub.Unsubscribe()
		return fmt.Errorf("activate conversation message subscription: %w", err)
	}
	h.sub = sub
	go func() {
		<-ctx.Done()
		_ = sub.Unsubscribe()
	}()
	return nil
}

func (h *Hub) Stop(context.Context) error {
	if h.sub == nil {
		return nil
	}
	err := h.sub.Unsubscribe()
	h.sub = nil
	return err
}

func (h *Hub) Publish(value Message) {
	h.publishLocal(value)
	encoded, err := json.Marshal(value)
	if err == nil {
		_ = h.nc.Publish(messageSubjectPrefix+value.TenantID.String()+"."+value.EntityID.String()+"."+value.ChannelID.String(), encoded)
	}
}

func (h *Hub) Subscribe(channelID guid.ID) (<-chan Message, func()) {
	stream := make(chan Message, 128)
	h.mu.Lock()
	if h.subs[channelID] == nil {
		h.subs[channelID] = make(map[chan Message]struct{})
	}
	h.subs[channelID][stream] = struct{}{}
	h.mu.Unlock()
	return stream, func() {
		h.mu.Lock()
		delete(h.subs[channelID], stream)
		if len(h.subs[channelID]) == 0 {
			delete(h.subs, channelID)
		}
		h.mu.Unlock()
	}
}

func (h *Hub) publishLocal(value Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for stream := range h.subs[value.ChannelID] {
		select {
		case stream <- value:
		default:
		}
	}
}
