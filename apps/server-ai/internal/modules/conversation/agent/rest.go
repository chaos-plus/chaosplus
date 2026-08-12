package agent

import (
	"context"
	"errors"
	"net/http"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/danielgtaylor/huma/v2"
)

var Actions = []authz.Action{
	{Resource: "conversation_agent", Verb: "create", Scope: "entity", AllowedRelations: []string{"owner", "editor"}, DataScoped: true},
	{Resource: "conversation_agent", Verb: "view", Scope: "entity", AllowedRelations: []string{"owner", "editor", "viewer"}, DataScoped: true, Menu: true},
	{Resource: "conversation_agent", Verb: "update", Scope: "entity", AllowedRelations: []string{"owner", "editor"}, DataScoped: true},
	{Resource: "conversation_agent", Verb: "delete", Scope: "entity", AllowedRelations: []string{"owner"}, DataScoped: true},
}

type entityInput struct{}
type idInput struct {
	entityInput
	ID guid.ParamID `path:"id"`
}
type createInput struct {
	entityInput
	Body AgentCreateInput
}
type updateInput struct {
	entityInput
	ID guid.ParamID `path:"id"`
	Body AgentUpdateInput
}
type statusInput struct {
	entityInput
	ID guid.ParamID `path:"id"`
	Body struct {
		Status  Status `json:"status"`
		Version int64  `json:"version" minimum:"1"`
	}
}
type retireInput struct {
	entityInput
	ID guid.ParamID `path:"id"`
	Body struct {
		HandoverDoc string `json:"handoverDoc" minLength:"1" maxLength:"65535"`
		Version     int64  `json:"version" minimum:"1"`
	}
}
type deleteInput struct {
	entityInput
	ID guid.ParamID `path:"id"`
	Version int64 `query:"version" minimum:"1"`
}
type body[T any] struct{ Body T }
type ok struct {
	OK bool `json:"ok"`
}

func (m *Module) RegisterREST(api huma.API) {
	register(m, api, huma.Operation{OperationID: "conversation-agent-list", Method: http.MethodGet, Path: "/api/agents", Summary: "List conversation agents", Tags: []string{"conversation-agent"}}, "view", m.list)
	register(m, api, huma.Operation{OperationID: "conversation-agent-create", Method: http.MethodPost, Path: "/api/agents", Summary: "Create a conversation agent", Tags: []string{"conversation-agent"}, DefaultStatus: http.StatusCreated}, "create", m.create)
	register(m, api, huma.Operation{OperationID: "conversation-agent-get", Method: http.MethodGet, Path: "/api/agents/{id}", Summary: "Get a conversation agent", Tags: []string{"conversation-agent"}}, "view", m.get)
	register(m, api, huma.Operation{OperationID: "conversation-agent-update", Method: http.MethodPatch, Path: "/api/agents/{id}", Summary: "Update a conversation agent", Tags: []string{"conversation-agent"}}, "update", m.update)
	register(m, api, huma.Operation{OperationID: "conversation-agent-status", Method: http.MethodPost, Path: "/api/agents/{id}/status", Summary: "Change a conversation agent status", Tags: []string{"conversation-agent"}}, "update", m.setStatus)
	register(m, api, huma.Operation{OperationID: "conversation-agent-retire", Method: http.MethodPost, Path: "/api/agents/{id}/retire", Summary: "Retire a conversation agent", Tags: []string{"conversation-agent"}}, "update", m.retire)
	register(m, api, huma.Operation{OperationID: "conversation-agent-delete", Method: http.MethodDelete, Path: "/api/agents/{id}", Summary: "Delete a retired conversation agent", Tags: []string{"conversation-agent"}}, "delete", m.delete)
}

func register[I, O any](m *Module, api huma.API, operation huma.Operation, verb string, handler func(context.Context, *I) (*O, error)) {
	authz.RegisterEntity(m.registrar, api, operation, authz.Guard{Resource: "conversation_agent", Verb: verb}, handler)
}

func (m *Module) list(ctx context.Context, _ *entityInput) (*body[[]Agent], error) {
	items, err := m.service.List(ctx)
	if err != nil {
		return nil, apiError(err)
	}
	return &body[[]Agent]{Body: items}, nil
}
func (m *Module) create(ctx context.Context, input *createInput) (*body[Agent], error) {
	value, err := m.service.Create(ctx, input.Body)
	if err != nil {
		return nil, apiError(err)
	}
	return &body[Agent]{Body: *value}, nil
}
func (m *Module) get(ctx context.Context, input *idInput) (*body[Agent], error) {
	value, err := m.service.Get(ctx, guid.ID(input.ID))
	if err != nil {
		return nil, apiError(err)
	}
	return &body[Agent]{Body: *value}, nil
}
func (m *Module) update(ctx context.Context, input *updateInput) (*body[Agent], error) {
	value, err := m.service.Update(ctx, guid.ID(input.ID), input.Body)
	if err != nil {
		return nil, apiError(err)
	}
	return &body[Agent]{Body: *value}, nil
}
func (m *Module) setStatus(ctx context.Context, input *statusInput) (*body[Agent], error) {
	value, err := m.service.SetStatus(ctx, guid.ID(input.ID), input.Body.Status, input.Body.Version)
	if err != nil {
		return nil, apiError(err)
	}
	return &body[Agent]{Body: *value}, nil
}
func (m *Module) retire(ctx context.Context, input *retireInput) (*body[Agent], error) {
	value, err := m.service.Retire(ctx, guid.ID(input.ID), input.Body.HandoverDoc, input.Body.Version)
	if err != nil {
		return nil, apiError(err)
	}
	return &body[Agent]{Body: *value}, nil
}
func (m *Module) delete(ctx context.Context, input *deleteInput) (*body[ok], error) {
	if err := m.service.Delete(ctx, guid.ID(input.ID), input.Version); err != nil {
		return nil, apiError(err)
	}
	return &body[ok]{Body: ok{OK: true}}, nil
}

func apiError(err error) error {
	switch {
	case errors.Is(err, ErrInvalid):
		return huma.Error422UnprocessableEntity("conversation.agent.invalid")
	case errors.Is(err, ErrNotFound):
		return huma.Error404NotFound("conversation.agent.not_found")
	case errors.Is(err, ErrVersionConflict):
		return huma.Error409Conflict("conversation.agent.version_conflict")
	case errors.Is(err, ErrStateConflict):
		return huma.Error409Conflict("conversation.agent.state_conflict")
	default:
		return huma.Error500InternalServerError("conversation.agent.unavailable")
	}
}
