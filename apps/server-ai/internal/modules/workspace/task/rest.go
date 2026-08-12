package task

import (
	"context"
	"errors"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/danielgtaylor/huma/v2"
	"net/http"
)

var Actions = []authz.Action{{Resource: "workspace_task", Verb: "create", Scope: "entity", AllowedRelations: []string{"owner", "editor"}, DataScoped: true}, {Resource: "workspace_task", Verb: "view", Scope: "entity", AllowedRelations: []string{"owner", "editor", "viewer"}, DataScoped: true, Menu: true}, {Resource: "workspace_task", Verb: "update", Scope: "entity", AllowedRelations: []string{"owner", "editor"}, DataScoped: true}, {Resource: "workspace_task", Verb: "delete", Scope: "entity", AllowedRelations: []string{"owner"}, DataScoped: true}, {Resource: "workspace_task", Verb: "execute", Scope: "entity", AllowedRelations: []string{"owner", "editor"}, DataScoped: true}}

type entityInput struct{}
type idInput struct {
	entityInput
	ID guid.ParamID `path:"id"`
}
type listInput struct {
	entityInput
	Kind          Kind     `query:"kind"`
	Status        Status   `query:"status"`
	RequirementID guid.ParamID `query:"requirementId"`
}
type createInput struct {
	entityInput
	Body CreateInput
}
type updateInput struct {
	entityInput
	ID guid.ParamID `path:"id"`
	Body UpdateInput
}
type deleteInput struct {
	entityInput
	ID guid.ParamID `path:"id"`
	Version int64 `query:"version" minimum:"1"`
}
type executeInput struct {
	entityInput
	ID guid.ParamID `path:"id"`
	Body struct {
		Version int64 `json:"version" minimum:"1"`
	}
}
type body[T any] struct{ Body T }
type ok struct {
	OK bool `json:"ok"`
}
type execution struct {
	RunID guid.ID `json:"runId"`
}

func (m *Module) RegisterREST(api huma.API) {
	register(m, api, huma.Operation{OperationID: "workspace-task-list", Method: http.MethodGet, Path: "/api/tasks", Summary: "List tasks", Tags: []string{"workspace-task"}}, "view", m.list)
	register(m, api, huma.Operation{OperationID: "workspace-task-create", Method: http.MethodPost, Path: "/api/tasks", Summary: "Create a task", Tags: []string{"workspace-task"}, DefaultStatus: http.StatusCreated}, "create", m.create)
	register(m, api, huma.Operation{OperationID: "workspace-task-get", Method: http.MethodGet, Path: "/api/tasks/{id}", Summary: "Get a task", Tags: []string{"workspace-task"}}, "view", m.get)
	register(m, api, huma.Operation{OperationID: "workspace-task-update", Method: http.MethodPatch, Path: "/api/tasks/{id}", Summary: "Update a task", Tags: []string{"workspace-task"}}, "update", m.update)
	register(m, api, huma.Operation{OperationID: "workspace-task-delete", Method: http.MethodDelete, Path: "/api/tasks/{id}", Summary: "Delete a task", Tags: []string{"workspace-task"}}, "delete", m.delete)
	register(m, api, huma.Operation{OperationID: "workspace-task-execution-create", Method: http.MethodPost, Path: "/api/tasks/{id}/executions", Summary: "Create a task workflow execution", Tags: []string{"workspace-task"}, DefaultStatus: http.StatusCreated}, "execute", m.execute)
}
func register[I, O any](m *Module, api huma.API, op huma.Operation, verb string, h func(context.Context, *I) (*O, error)) {
	authz.RegisterEntity(m.registrar, api, op, authz.Guard{Resource: "workspace_task", Verb: verb}, h)
}
func (m *Module) list(c context.Context, i *listInput) (*body[[]Task], error) {
	var requirementID *guid.ID
	if !guid.ID(i.RequirementID).Zero() {
		id := guid.ID(i.RequirementID)
		requirementID = &id
	}
	v, e := m.service.List(c, i.Kind, i.Status, requirementID)
	if e != nil {
		return nil, apiError(e)
	}
	return &body[[]Task]{Body: v}, nil
}
func (m *Module) create(c context.Context, i *createInput) (*body[Task], error) {
	v, e := m.service.Create(c, i.Body)
	if e != nil {
		return nil, apiError(e)
	}
	return &body[Task]{Body: *v}, nil
}
func (m *Module) get(c context.Context, i *idInput) (*body[Task], error) {
	v, e := m.service.Get(c, guid.ID(i.ID))
	if e != nil {
		return nil, apiError(e)
	}
	return &body[Task]{Body: *v}, nil
}
func (m *Module) update(c context.Context, i *updateInput) (*body[Task], error) {
	v, e := m.service.Update(c, guid.ID(i.ID), i.Body)
	if e != nil {
		return nil, apiError(e)
	}
	return &body[Task]{Body: *v}, nil
}
func (m *Module) delete(c context.Context, i *deleteInput) (*body[ok], error) {
	if e := m.service.Delete(c, guid.ID(i.ID), i.Version); e != nil {
		return nil, apiError(e)
	}
	return &body[ok]{Body: ok{OK: true}}, nil
}
func (m *Module) execute(c context.Context, i *executeInput) (*body[execution], error) {
	id, e := m.service.Execute(c, guid.ID(i.ID), i.Body.Version)
	if e != nil {
		return nil, apiError(e)
	}
	return &body[execution]{Body: execution{RunID: id}}, nil
}
func apiError(e error) error {
	switch {
	case errors.Is(e, ErrInvalid):
		return huma.Error422UnprocessableEntity("workspace.task.invalid")
	case errors.Is(e, ErrNotFound):
		return huma.Error404NotFound("workspace.task.not_found")
	case errors.Is(e, ErrVersionConflict):
		return huma.Error409Conflict("workspace.task.version_conflict")
	case errors.Is(e, ErrStateConflict):
		return huma.Error409Conflict("workspace.task.state_conflict")
	default:
		return huma.Error500InternalServerError("workspace.task.unavailable")
	}
}
