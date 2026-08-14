package message

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/gorilla/websocket"
)

var Actions = []authz.Action{
	{Resource: "conversation_message", Verb: "create", Scope: "entity", AllowedRelations: []string{"owner", "editor", "viewer"}, DataScoped: true},
	{Resource: "conversation_message", Verb: "view", Scope: "entity", AllowedRelations: []string{"owner", "editor", "viewer"}, DataScoped: true},
}

type listInput struct {
	ChannelID guid.ParamID `path:"id"`
	AfterSeq  int64        `query:"afterSeq" minimum:"0"`
	Limit     int          `query:"limit" minimum:"1" maximum:"500" default:"100"`
}
type createInput struct {
	ChannelID guid.ParamID `path:"id"`
	Body      MessageCreateInput
}
type body[T any] struct{ Body T }

func (m *Module) RegisterREST(api huma.API) {
	register(m, api, huma.Operation{OperationID: "conversation-message-list", Method: http.MethodGet, Path: "/api/channels/{id}/messages", Summary: "List conversation messages", Tags: []string{"conversation-message"}}, "view", m.list)
	register(m, api, huma.Operation{OperationID: "conversation-message-create", Method: http.MethodPost, Path: "/api/channels/{id}/messages", Summary: "Post a conversation message", Tags: []string{"conversation-message"}, DefaultStatus: http.StatusCreated}, "create", m.create)
	m.registerEvents(api)
}

func register[I, O any](m *Module, api huma.API, operation huma.Operation, verb string, handler func(context.Context, *I) (*O, error)) {
	authz.RegisterEntity(m.registrar, api, operation, authz.Guard{Resource: "conversation_message", Verb: verb}, handler)
}

func (m *Module) list(ctx context.Context, input *listInput) (*body[[]Message], error) {
	items, err := m.service.List(ctx, guid.ID(input.ChannelID), input.AfterSeq, input.Limit)
	if err != nil {
		return nil, apiError(err)
	}
	return &body[[]Message]{Body: items}, nil
}

func (m *Module) create(ctx context.Context, input *createInput) (*body[Message], error) {
	value, err := m.service.Post(ctx, guid.ID(input.ChannelID), input.Body)
	if err != nil {
		return nil, apiError(err)
	}
	return &body[Message]{Body: *value}, nil
}

func (m *Module) registerEvents(api huma.API) {
	op := huma.Operation{OperationID: "conversation-message-events", Method: http.MethodGet, Path: "/api/channels/{id}/events", Summary: "Stream conversation messages", Tags: []string{"conversation-message"},
		Parameters: []*huma.Param{{Name: "id", In: "path", Required: true, Schema: guid.ID(0).Schema(api.OpenAPI().Components.Schemas)}, {Name: "afterSeq", In: "query", Required: false, Schema: &huma.Schema{Type: huma.TypeInteger, Minimum: floatPointer(0)}}}, Errors: []int{http.StatusBadRequest, http.StatusNotFound}}
	authz.RegisterEntityAdapter(m.registrar, api, op, authz.Guard{Resource: "conversation_message", Verb: "view"}, func(hctx huma.Context) {
		channelID, err := guid.Parse(hctx.Param("id"))
		if err != nil {
			_ = huma.WriteErr(api, hctx, http.StatusBadRequest, "conversation.message.invalid")
			return
		}
		afterSeq := int64(0)
		if raw := hctx.Query("afterSeq"); raw != "" {
			if _, err := fmt.Sscan(raw, &afterSeq); err != nil || afterSeq < 0 {
				_ = huma.WriteErr(api, hctx, http.StatusBadRequest, "conversation.message.invalid")
				return
			}
		}
		history, err := m.service.List(hctx.Context(), channelID, afterSeq, 500)
		if err != nil {
			_ = huma.WriteErr(api, hctx, statusCode(err), errorCode(err))
			return
		}
		stream, unsubscribe := m.hub.Subscribe(channelID)
		defer unsubscribe()
		request, writer := humachi.Unwrap(hctx)
		conn, err := (&websocket.Upgrader{CheckOrigin: m.origin.Allows}).Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		readDone := make(chan struct{})
		go func() {
			defer close(readDone)
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()
		seen := make(map[guid.ID]struct{}, len(history))
		for _, value := range history {
			seen[value.ID] = struct{}{}
			if writeMessage(conn, value) != nil {
				return
			}
		}
		for {
			select {
			case <-readDone:
				return
			case <-request.Context().Done():
				return
			case value := <-stream:
				if _, duplicate := seen[value.ID]; duplicate {
					continue
				}
				seen[value.ID] = struct{}{}
				if writeMessage(conn, value) != nil {
					return
				}
			}
		}
	})
}

func writeMessage(conn *websocket.Conn, value Message) error {
	_ = conn.SetWriteDeadline(time.Now().UTC().Add(10 * time.Second))
	return conn.WriteJSON(value)
}

func floatPointer(value float64) *float64 { return &value }

func statusCode(err error) int {
	switch {
	case errors.Is(err, ErrInvalid):
		return http.StatusUnprocessableEntity
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, ErrConflict):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

func errorCode(err error) string {
	switch {
	case errors.Is(err, ErrInvalid):
		return "conversation.message.invalid"
	case errors.Is(err, ErrNotFound):
		return "conversation.message.not_found"
	case errors.Is(err, ErrForbidden):
		return "conversation.message.forbidden"
	case errors.Is(err, ErrConflict):
		return "conversation.message.conflict"
	default:
		return "conversation.message.unavailable"
	}
}

func apiError(err error) error {
	switch statusCode(err) {
	case http.StatusUnprocessableEntity:
		return huma.Error422UnprocessableEntity(errorCode(err))
	case http.StatusNotFound:
		return huma.Error404NotFound(errorCode(err))
	case http.StatusForbidden:
		return huma.Error403Forbidden(errorCode(err))
	case http.StatusConflict:
		return huma.Error409Conflict(errorCode(err))
	default:
		return huma.Error500InternalServerError(errorCode(err))
	}
}
