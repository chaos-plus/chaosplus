package defect

import (
	"context"
	"errors"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/danielgtaylor/huma/v2"
	"net/http"
)

var Actions = []authz.Action{{Resource: "workspace_defect", Verb: "create", Scope: "entity", AllowedRelations: []string{"owner", "editor"}, DataScoped: true}, {Resource: "workspace_defect", Verb: "view", Scope: "entity", AllowedRelations: []string{"owner", "editor", "viewer"}, DataScoped: true, Menu: true}, {Resource: "workspace_defect", Verb: "update", Scope: "entity", AllowedRelations: []string{"owner", "editor"}, DataScoped: true}, {Resource: "workspace_defect", Verb: "delete", Scope: "entity", AllowedRelations: []string{"owner"}, DataScoped: true}}

type idInput struct {
	ID guid.ParamID `path:"id"`
}
type listInput struct {
	Status   Status   `query:"status"`
	Severity Severity `query:"severity"`
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
	register(m, api, huma.Operation{OperationID: "workspace-defect-list", Method: http.MethodGet, Path: "/api/defects", Summary: "List defects", Tags: []string{"workspace-defect"}}, "view", m.list)
	register(m, api, huma.Operation{OperationID: "workspace-defect-create", Method: http.MethodPost, Path: "/api/defects", Summary: "Create a defect", Tags: []string{"workspace-defect"}, DefaultStatus: http.StatusCreated}, "create", m.create)
	register(m, api, huma.Operation{OperationID: "workspace-defect-get", Method: http.MethodGet, Path: "/api/defects/{id}", Summary: "Get a defect", Tags: []string{"workspace-defect"}}, "view", m.get)
	register(m, api, huma.Operation{OperationID: "workspace-defect-update", Method: http.MethodPatch, Path: "/api/defects/{id}", Summary: "Update a defect", Tags: []string{"workspace-defect"}}, "update", m.update)
	register(m, api, huma.Operation{OperationID: "workspace-defect-delete", Method: http.MethodDelete, Path: "/api/defects/{id}", Summary: "Delete a defect", Tags: []string{"workspace-defect"}}, "delete", m.delete)
}
func register[I, O any](m *Module, api huma.API, operation huma.Operation, verb string, handler func(context.Context, *I) (*O, error)) {
	authz.RegisterEntity(m.registrar, api, operation, authz.Guard{Resource: "workspace_defect", Verb: verb}, handler)
}
func (m *Module) list(ctx context.Context, input *listInput) (*body[[]Defect], error) {
	values, err := m.service.List(ctx, input.Status, input.Severity)
	if err != nil {
		return nil, apiError(err)
	}
	return &body[[]Defect]{Body: values}, nil
}
func (m *Module) create(ctx context.Context, input *createInput) (*body[Defect], error) {
	value, err := m.service.Create(ctx, input.Body)
	if err != nil {
		return nil, apiError(err)
	}
	return &body[Defect]{Body: *value}, nil
}
func (m *Module) get(ctx context.Context, input *idInput) (*body[Defect], error) {
	value, err := m.service.Get(ctx, guid.ID(input.ID))
	if err != nil {
		return nil, apiError(err)
	}
	return &body[Defect]{Body: *value}, nil
}
func (m *Module) update(ctx context.Context, input *updateInput) (*body[Defect], error) {
	value, err := m.service.Update(ctx, guid.ID(input.ID), input.Body)
	if err != nil {
		return nil, apiError(err)
	}
	return &body[Defect]{Body: *value}, nil
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
		return huma.Error422UnprocessableEntity("workspace.defect.invalid")
	case errors.Is(err, ErrNotFound):
		return huma.Error404NotFound("workspace.defect.not_found")
	case errors.Is(err, ErrVersionConflict):
		return huma.Error409Conflict("workspace.defect.version_conflict")
	case errors.Is(err, ErrStateConflict):
		return huma.Error409Conflict("workspace.defect.state_conflict")
	default:
		return huma.Error500InternalServerError("workspace.defect.unavailable")
	}
}
