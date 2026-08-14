package channel

import (
	"context"
	"errors"
	"net/http"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/danielgtaylor/huma/v2"
)

var Actions = []authz.Action{
	{Resource: "conversation_channel", Verb: "create", Scope: "entity", AllowedRelations: []string{"owner", "editor"}, DataScoped: true},
	{Resource: "conversation_channel", Verb: "view", Scope: "entity", AllowedRelations: []string{"owner", "editor", "viewer"}, DataScoped: true, Menu: true},
	{Resource: "conversation_channel", Verb: "update", Scope: "entity", AllowedRelations: []string{"owner", "editor"}, DataScoped: true},
	{Resource: "conversation_channel", Verb: "delete", Scope: "entity", AllowedRelations: []string{"owner"}, DataScoped: true},
}

type idInput struct {
	ID guid.ParamID `path:"id"`
}
type listInput struct {
	ProjectID guid.ParamID `query:"projectId" required:"false"`
}
type createInput struct {
	Body ChannelCreateInput
}
type updateInput struct {
	ID   guid.ParamID `path:"id"`
	Body ChannelUpdateInput
}
type deleteInput struct {
	ID      guid.ParamID `path:"id"`
	Version int64        `query:"version" minimum:"1"`
}
type memberInput struct {
	ID   guid.ParamID `path:"id"`
	Body AddMemberInput
}
type removeMemberInput struct {
	ID       guid.ParamID `path:"id"`
	MemberID guid.ParamID `path:"memberId"`
	Kind     MemberKind   `path:"kind" enum:"human,agent"`
	Version  int64        `query:"version" minimum:"1"`
}
type body[T any] struct{ Body T }
type ok struct {
	OK bool `json:"ok"`
}

func (m *Module) RegisterREST(api huma.API) {
	register(m, api, huma.Operation{OperationID: "conversation-channel-list", Method: http.MethodGet, Path: "/api/channels", Summary: "List conversation channels", Tags: []string{"conversation-channel"}}, "view", m.list)
	register(m, api, huma.Operation{OperationID: "conversation-channel-create", Method: http.MethodPost, Path: "/api/channels", Summary: "Create a conversation channel", Tags: []string{"conversation-channel"}, DefaultStatus: http.StatusCreated}, "create", m.create)
	register(m, api, huma.Operation{OperationID: "conversation-channel-get", Method: http.MethodGet, Path: "/api/channels/{id}", Summary: "Get a conversation channel", Tags: []string{"conversation-channel"}}, "view", m.get)
	register(m, api, huma.Operation{OperationID: "conversation-channel-update", Method: http.MethodPatch, Path: "/api/channels/{id}", Summary: "Update a conversation channel", Tags: []string{"conversation-channel"}}, "update", m.update)
	register(m, api, huma.Operation{OperationID: "conversation-channel-delete", Method: http.MethodDelete, Path: "/api/channels/{id}", Summary: "Delete a conversation channel", Tags: []string{"conversation-channel"}}, "delete", m.delete)
	register(m, api, huma.Operation{OperationID: "conversation-channel-member-list", Method: http.MethodGet, Path: "/api/channels/{id}/members", Summary: "List conversation channel members", Tags: []string{"conversation-channel"}}, "view", m.listMembers)
	register(m, api, huma.Operation{OperationID: "conversation-channel-member-add", Method: http.MethodPost, Path: "/api/channels/{id}/members", Summary: "Add a conversation channel member", Tags: []string{"conversation-channel"}, DefaultStatus: http.StatusCreated}, "update", m.addMember)
	register(m, api, huma.Operation{OperationID: "conversation-channel-member-remove", Method: http.MethodDelete, Path: "/api/channels/{id}/members/{memberId}/{kind}", Summary: "Remove a conversation channel member", Tags: []string{"conversation-channel"}}, "update", m.removeMember)
}

func register[I, O any](m *Module, api huma.API, operation huma.Operation, verb string, handler func(context.Context, *I) (*O, error)) {
	authz.RegisterEntity(m.registrar, api, operation, authz.Guard{Resource: "conversation_channel", Verb: verb}, handler)
}

func (m *Module) list(ctx context.Context, input *listInput) (*body[[]Channel], error) {
	items, err := m.service.List(ctx, guid.ID(input.ProjectID))
	if err != nil {
		return nil, apiError(err)
	}
	return &body[[]Channel]{Body: items}, nil
}
func (m *Module) create(ctx context.Context, input *createInput) (*body[Channel], error) {
	value, err := m.service.Create(ctx, input.Body)
	if err != nil {
		return nil, apiError(err)
	}
	return &body[Channel]{Body: *value}, nil
}
func (m *Module) get(ctx context.Context, input *idInput) (*body[Channel], error) {
	value, err := m.service.Get(ctx, guid.ID(input.ID))
	if err != nil {
		return nil, apiError(err)
	}
	return &body[Channel]{Body: *value}, nil
}
func (m *Module) update(ctx context.Context, input *updateInput) (*body[Channel], error) {
	value, err := m.service.Update(ctx, guid.ID(input.ID), input.Body)
	if err != nil {
		return nil, apiError(err)
	}
	return &body[Channel]{Body: *value}, nil
}
func (m *Module) delete(ctx context.Context, input *deleteInput) (*body[ok], error) {
	if err := m.service.Delete(ctx, guid.ID(input.ID), input.Version); err != nil {
		return nil, apiError(err)
	}
	return &body[ok]{Body: ok{OK: true}}, nil
}
func (m *Module) listMembers(ctx context.Context, input *idInput) (*body[[]Member], error) {
	items, err := m.service.ListMembers(ctx, guid.ID(input.ID))
	if err != nil {
		return nil, apiError(err)
	}
	return &body[[]Member]{Body: items}, nil
}
func (m *Module) addMember(ctx context.Context, input *memberInput) (*body[Member], error) {
	value, err := m.service.AddMember(ctx, guid.ID(input.ID), input.Body)
	if err != nil {
		return nil, apiError(err)
	}
	return &body[Member]{Body: *value}, nil
}
func (m *Module) removeMember(ctx context.Context, input *removeMemberInput) (*body[ok], error) {
	if err := m.service.RemoveMember(ctx, guid.ID(input.ID), guid.ID(input.MemberID), input.Kind, input.Version); err != nil {
		return nil, apiError(err)
	}
	return &body[ok]{Body: ok{OK: true}}, nil
}

func apiError(err error) error {
	switch {
	case errors.Is(err, ErrInvalid):
		return huma.Error422UnprocessableEntity("conversation.channel.invalid")
	case errors.Is(err, ErrNotFound):
		return huma.Error404NotFound("conversation.channel.not_found")
	case errors.Is(err, ErrForbidden):
		return huma.Error403Forbidden("conversation.channel.forbidden")
	case errors.Is(err, ErrVersionConflict), errors.Is(err, ErrMemberConflict):
		return huma.Error409Conflict("conversation.channel.conflict")
	default:
		return huma.Error500InternalServerError("conversation.channel.unavailable")
	}
}
