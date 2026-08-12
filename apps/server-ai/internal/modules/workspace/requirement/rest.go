package requirement

import (
	"context"
	"errors"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/danielgtaylor/huma/v2"
	"net/http"
)

var Actions = []authz.Action{
	{Resource: "workspace_requirement", Verb: "create", Scope: "entity", AllowedRelations: []string{"owner", "editor"}, DataScoped: true},
	{Resource: "workspace_requirement", Verb: "view", Scope: "entity", AllowedRelations: []string{"owner", "editor", "viewer"}, DataScoped: true, Menu: true},
	{Resource: "workspace_requirement", Verb: "update", Scope: "entity", AllowedRelations: []string{"owner", "editor"}, DataScoped: true},
	{Resource: "workspace_requirement", Verb: "delete", Scope: "entity", AllowedRelations: []string{"owner"}, DataScoped: true},
}

type entityInput struct{}
type idInput struct {
	entityInput
	ID guid.ID `path:"id"`
}
type listInput struct {
	entityInput
	Status   Status   `query:"status"`
	ParentID *guid.ID `query:"parentId"`
}
type createInput struct {
	entityInput
	Body CreateInput
}
type updateInput struct {
	idInput
	Body UpdateInput
}
type deleteInput struct {
	idInput
	Version int64 `query:"version" minimum:"1"`
}
type body[T any] struct{ Body T }
type ok struct {
	OK bool `json:"ok"`
}

func (m *Module) RegisterREST(api huma.API) {
	register(m, api, huma.Operation{OperationID: "workspace-requirement-list", Method: http.MethodGet, Path: "/api/requirements", Summary: "List requirements", Tags: []string{"workspace-requirement"}}, "view", m.list)
	register(m, api, huma.Operation{OperationID: "workspace-requirement-create", Method: http.MethodPost, Path: "/api/requirements", Summary: "Create a requirement", Tags: []string{"workspace-requirement"}, DefaultStatus: http.StatusCreated}, "create", m.create)
	register(m, api, huma.Operation{OperationID: "workspace-requirement-get", Method: http.MethodGet, Path: "/api/requirements/{id}", Summary: "Get a requirement", Tags: []string{"workspace-requirement"}}, "view", m.get)
	register(m, api, huma.Operation{OperationID: "workspace-requirement-update", Method: http.MethodPatch, Path: "/api/requirements/{id}", Summary: "Update a requirement", Tags: []string{"workspace-requirement"}}, "update", m.update)
	register(m, api, huma.Operation{OperationID: "workspace-requirement-delete", Method: http.MethodDelete, Path: "/api/requirements/{id}", Summary: "Delete a requirement", Tags: []string{"workspace-requirement"}}, "delete", m.delete)
}
func register[I, O any](m *Module, api huma.API, op huma.Operation, verb string, handler func(context.Context, *I) (*O, error)) {
	authz.RegisterEntity(m.registrar, api, op, authz.Guard{Resource: "workspace_requirement", Verb: verb}, handler)
}
func (m *Module) list(ctx context.Context, in *listInput) (*body[[]Requirement], error) {
	v, e := m.service.List(ctx, in.Status, in.ParentID)
	if e != nil {
		return nil, apiError(e)
	}
	return &body[[]Requirement]{Body: v}, nil
}
func (m *Module) create(ctx context.Context, in *createInput) (*body[Requirement], error) {
	v, e := m.service.Create(ctx, in.Body)
	if e != nil {
		return nil, apiError(e)
	}
	return &body[Requirement]{Body: *v}, nil
}
func (m *Module) get(ctx context.Context, in *idInput) (*body[Requirement], error) {
	v, e := m.service.Get(ctx, in.ID)
	if e != nil {
		return nil, apiError(e)
	}
	return &body[Requirement]{Body: *v}, nil
}
func (m *Module) update(ctx context.Context, in *updateInput) (*body[Requirement], error) {
	v, e := m.service.Update(ctx, in.ID, in.Body)
	if e != nil {
		return nil, apiError(e)
	}
	return &body[Requirement]{Body: *v}, nil
}
func (m *Module) delete(ctx context.Context, in *deleteInput) (*body[ok], error) {
	if e := m.service.Delete(ctx, in.ID, in.Version); e != nil {
		return nil, apiError(e)
	}
	return &body[ok]{Body: ok{OK: true}}, nil
}
func apiError(err error) error {
	switch {
	case errors.Is(err, ErrInvalid):
		return huma.Error422UnprocessableEntity("workspace.requirement.invalid")
	case errors.Is(err, ErrNotFound):
		return huma.Error404NotFound("workspace.requirement.not_found")
	case errors.Is(err, ErrVersionConflict):
		return huma.Error409Conflict("workspace.requirement.version_conflict")
	default:
		return huma.Error500InternalServerError("workspace.requirement.unavailable")
	}
}
