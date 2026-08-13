package testcase

import (
	"context"
	"errors"
	"net/http"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/danielgtaylor/huma/v2"
)

var Actions = []authz.Action{
	{Resource: "workspace_testcase", Verb: "create", Scope: "entity", AllowedRelations: []string{"owner", "editor"}, DataScoped: true},
	{Resource: "workspace_testcase", Verb: "view", Scope: "entity", AllowedRelations: []string{"owner", "editor", "viewer"}, DataScoped: true, Menu: true},
	{Resource: "workspace_testcase", Verb: "update", Scope: "entity", AllowedRelations: []string{"owner", "editor"}, DataScoped: true},
	{Resource: "workspace_testcase", Verb: "delete", Scope: "entity", AllowedRelations: []string{"owner"}, DataScoped: true},
}

type idInput struct {
	ID guid.ParamID `path:"id"`
}
type listInput struct {
	Status        Status       `query:"status"`
	RequirementID guid.ParamID `query:"requirementId"`
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
	register(m, api, huma.Operation{OperationID: "workspace-test-case-list", Method: http.MethodGet, Path: "/api/test-cases", Summary: "List test cases", Tags: []string{"workspace-test-case"}}, "view", m.list)
	register(m, api, huma.Operation{OperationID: "workspace-test-case-create", Method: http.MethodPost, Path: "/api/test-cases", Summary: "Create a test case", Tags: []string{"workspace-test-case"}, DefaultStatus: http.StatusCreated}, "create", m.create)
	register(m, api, huma.Operation{OperationID: "workspace-test-case-get", Method: http.MethodGet, Path: "/api/test-cases/{id}", Summary: "Get a test case", Tags: []string{"workspace-test-case"}}, "view", m.get)
	register(m, api, huma.Operation{OperationID: "workspace-test-case-update", Method: http.MethodPatch, Path: "/api/test-cases/{id}", Summary: "Update a test case", Tags: []string{"workspace-test-case"}}, "update", m.update)
	register(m, api, huma.Operation{OperationID: "workspace-test-case-delete", Method: http.MethodDelete, Path: "/api/test-cases/{id}", Summary: "Delete a test case", Tags: []string{"workspace-test-case"}}, "delete", m.delete)
}
func register[I, O any](m *Module, api huma.API, operation huma.Operation, verb string, handler func(context.Context, *I) (*O, error)) {
	authz.RegisterEntity(m.registrar, api, operation, authz.Guard{Resource: "workspace_testcase", Verb: verb}, handler)
}
func (m *Module) list(ctx context.Context, input *listInput) (*body[[]TestCase], error) {
	var requirementID *guid.ID
	if id := guid.ID(input.RequirementID); !id.Zero() {
		requirementID = &id
	}
	values, err := m.service.List(ctx, input.Status, requirementID)
	if err != nil {
		return nil, apiError(err)
	}
	return &body[[]TestCase]{Body: values}, nil
}
func (m *Module) create(ctx context.Context, input *createInput) (*body[TestCase], error) {
	value, err := m.service.Create(ctx, input.Body)
	if err != nil {
		return nil, apiError(err)
	}
	return &body[TestCase]{Body: *value}, nil
}
func (m *Module) get(ctx context.Context, input *idInput) (*body[TestCase], error) {
	value, err := m.service.Get(ctx, guid.ID(input.ID))
	if err != nil {
		return nil, apiError(err)
	}
	return &body[TestCase]{Body: *value}, nil
}
func (m *Module) update(ctx context.Context, input *updateInput) (*body[TestCase], error) {
	value, err := m.service.Update(ctx, guid.ID(input.ID), input.Body)
	if err != nil {
		return nil, apiError(err)
	}
	return &body[TestCase]{Body: *value}, nil
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
		return huma.Error422UnprocessableEntity("workspace.testcase.invalid")
	case errors.Is(err, ErrNotFound):
		return huma.Error404NotFound("workspace.testcase.not_found")
	case errors.Is(err, ErrVersionConflict):
		return huma.Error409Conflict("workspace.testcase.version_conflict")
	case errors.Is(err, ErrStateConflict):
		return huma.Error409Conflict("workspace.testcase.state_conflict")
	default:
		return huma.Error500InternalServerError("workspace.testcase.unavailable")
	}
}
