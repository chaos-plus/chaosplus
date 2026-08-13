package objective

import (
	"context"
	"errors"
	"net/http"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/danielgtaylor/huma/v2"
)

var Actions = []authz.Action{
	{Resource: "workspace_objective", Verb: "create", Scope: "entity", AllowedRelations: []string{"owner", "editor"}, DataScoped: true},
	{Resource: "workspace_objective", Verb: "view", Scope: "entity", AllowedRelations: []string{"owner", "editor", "viewer"}, DataScoped: true, Menu: true},
	{Resource: "workspace_objective", Verb: "update", Scope: "entity", AllowedRelations: []string{"owner", "editor"}, DataScoped: true},
	{Resource: "workspace_objective", Verb: "delete", Scope: "entity", AllowedRelations: []string{"owner"}, DataScoped: true},
}

type idInput struct {
	ID guid.ParamID `path:"id"`
}
type listInput struct {
	Status Status `query:"status"`
}
type createInput struct {
	Body CreateInput
}
type updateInput struct {
	ID   guid.ParamID `path:"id"`
	Body UpdateInput
}
type deleteInput struct {
	ID      guid.ParamID `path:"id"`
	Version int64        `query:"version" minimum:"1"`
}
type body[T any] struct{ Body T }
type ok struct {
	OK bool `json:"ok"`
}

func (m *Module) RegisterREST(api huma.API) {
	register(m, api, huma.Operation{OperationID: "workspace-objective-list", Method: http.MethodGet, Path: "/api/objectives", Summary: "List objectives", Tags: []string{"workspace-objective"}}, "view", m.list)
	register(m, api, huma.Operation{OperationID: "workspace-objective-create", Method: http.MethodPost, Path: "/api/objectives", Summary: "Create an objective", Tags: []string{"workspace-objective"}, DefaultStatus: http.StatusCreated}, "create", m.create)
	register(m, api, huma.Operation{OperationID: "workspace-objective-get", Method: http.MethodGet, Path: "/api/objectives/{id}", Summary: "Get an objective", Tags: []string{"workspace-objective"}}, "view", m.get)
	register(m, api, huma.Operation{OperationID: "workspace-objective-update", Method: http.MethodPatch, Path: "/api/objectives/{id}", Summary: "Update an objective", Tags: []string{"workspace-objective"}}, "update", m.update)
	register(m, api, huma.Operation{OperationID: "workspace-objective-delete", Method: http.MethodDelete, Path: "/api/objectives/{id}", Summary: "Delete an objective", Tags: []string{"workspace-objective"}}, "delete", m.delete)
}

func register[I, O any](m *Module, api huma.API, operation huma.Operation, verb string, handler func(context.Context, *I) (*O, error)) {
	authz.RegisterEntity(m.registrar, api, operation, authz.Guard{Resource: "workspace_objective", Verb: verb}, handler)
}

func (m *Module) list(ctx context.Context, input *listInput) (*body[[]Objective], error) {
	value, err := m.service.List(ctx, input.Status)
	if err != nil {
		return nil, apiError(err)
	}
	return &body[[]Objective]{Body: value}, nil
}
func (m *Module) create(ctx context.Context, input *createInput) (*body[Objective], error) {
	value, err := m.service.Create(ctx, input.Body)
	if err != nil {
		return nil, apiError(err)
	}
	return &body[Objective]{Body: *value}, nil
}
func (m *Module) get(ctx context.Context, input *idInput) (*body[Objective], error) {
	value, err := m.service.Get(ctx, guid.ID(input.ID))
	if err != nil {
		return nil, apiError(err)
	}
	return &body[Objective]{Body: *value}, nil
}
func (m *Module) update(ctx context.Context, input *updateInput) (*body[Objective], error) {
	value, err := m.service.Update(ctx, guid.ID(input.ID), input.Body)
	if err != nil {
		return nil, apiError(err)
	}
	return &body[Objective]{Body: *value}, nil
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
		return huma.Error422UnprocessableEntity("workspace.objective.invalid")
	case errors.Is(err, ErrNotFound):
		return huma.Error404NotFound("workspace.objective.not_found")
	case errors.Is(err, ErrVersionConflict):
		return huma.Error409Conflict("workspace.objective.version_conflict")
	case errors.Is(err, ErrStateConflict):
		return huma.Error409Conflict("workspace.objective.state_conflict")
	default:
		return huma.Error500InternalServerError("workspace.objective.unavailable")
	}
}
