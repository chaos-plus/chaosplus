package testrun

import (
	"context"
	"errors"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/danielgtaylor/huma/v2"
	"net/http"
)

var Actions = []authz.Action{{Resource: "workspace_testrun", Verb: "create", Scope: "entity", AllowedRelations: []string{"owner", "editor"}, DataScoped: true}, {Resource: "workspace_testrun", Verb: "view", Scope: "entity", AllowedRelations: []string{"owner", "editor", "viewer"}, DataScoped: true, Menu: true}, {Resource: "workspace_testrun", Verb: "update", Scope: "entity", AllowedRelations: []string{"owner", "editor"}, DataScoped: true}}

type idInput struct {
	ID guid.ParamID `path:"id"`
}
type listInput struct {
	TestCaseID guid.ParamID `query:"testCaseId"`
	Status     Status       `query:"status"`
}
type createInput struct {
	Body CreateInput
}
type updateInput struct {
	ID   guid.ParamID `path:"id"`
	Body UpdateInput
}
type body[T any] struct{ Body T }

func (m *Module) RegisterREST(api huma.API) {
	register(m, api, huma.Operation{OperationID: "workspace-test-run-list", Method: http.MethodGet, Path: "/api/test-runs", Summary: "List test runs", Tags: []string{"workspace-test-run"}}, "view", m.list)
	register(m, api, huma.Operation{OperationID: "workspace-test-run-create", Method: http.MethodPost, Path: "/api/test-runs", Summary: "Create a test run", Tags: []string{"workspace-test-run"}, DefaultStatus: http.StatusCreated}, "create", m.create)
	register(m, api, huma.Operation{OperationID: "workspace-test-run-get", Method: http.MethodGet, Path: "/api/test-runs/{id}", Summary: "Get a test run", Tags: []string{"workspace-test-run"}}, "view", m.get)
	register(m, api, huma.Operation{OperationID: "workspace-test-run-update", Method: http.MethodPatch, Path: "/api/test-runs/{id}", Summary: "Update a test run", Tags: []string{"workspace-test-run"}}, "update", m.update)
}
func register[I, O any](m *Module, api huma.API, operation huma.Operation, verb string, handler func(context.Context, *I) (*O, error)) {
	authz.RegisterEntity(m.registrar, api, operation, authz.Guard{Resource: "workspace_testrun", Verb: verb}, handler)
}
func (m *Module) list(ctx context.Context, input *listInput) (*body[[]TestRun], error) {
	var id *guid.ID
	if value := guid.ID(input.TestCaseID); !value.Zero() {
		id = &value
	}
	values, err := m.service.List(ctx, id, input.Status)
	if err != nil {
		return nil, apiError(err)
	}
	return &body[[]TestRun]{Body: values}, nil
}
func (m *Module) create(ctx context.Context, input *createInput) (*body[TestRun], error) {
	value, err := m.service.Create(ctx, input.Body)
	if err != nil {
		return nil, apiError(err)
	}
	return &body[TestRun]{Body: *value}, nil
}
func (m *Module) get(ctx context.Context, input *idInput) (*body[TestRun], error) {
	value, err := m.service.Get(ctx, guid.ID(input.ID))
	if err != nil {
		return nil, apiError(err)
	}
	return &body[TestRun]{Body: *value}, nil
}
func (m *Module) update(ctx context.Context, input *updateInput) (*body[TestRun], error) {
	value, err := m.service.Update(ctx, guid.ID(input.ID), input.Body)
	if err != nil {
		return nil, apiError(err)
	}
	return &body[TestRun]{Body: *value}, nil
}
func apiError(err error) error {
	switch {
	case errors.Is(err, ErrInvalid):
		return huma.Error422UnprocessableEntity("workspace.testrun.invalid")
	case errors.Is(err, ErrNotFound):
		return huma.Error404NotFound("workspace.testrun.not_found")
	case errors.Is(err, ErrVersionConflict):
		return huma.Error409Conflict("workspace.testrun.version_conflict")
	case errors.Is(err, ErrStateConflict):
		return huma.Error409Conflict("workspace.testrun.state_conflict")
	default:
		return huma.Error500InternalServerError("workspace.testrun.unavailable")
	}
}
