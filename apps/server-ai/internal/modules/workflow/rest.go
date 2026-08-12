package workflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/gorilla/websocket"
)

var Actions = []authz.Action{
	{Resource: "workflow", Verb: "create", Scope: "entity", Summary: "create workflow definitions and runs", AllowedRelations: []string{"owner", "editor"}, DataScoped: true},
	{Resource: "workflow", Verb: "view", Scope: "entity", Summary: "view workflow definitions and runs", AllowedRelations: []string{"owner", "editor", "viewer"}, DataScoped: true, Menu: true},
	{Resource: "workflow", Verb: "update", Scope: "entity", Summary: "control workflow runs and approvals", AllowedRelations: []string{"owner", "editor"}, DataScoped: true},
	{Resource: "workflow", Verb: "delete", Scope: "entity", Summary: "delete workflow definitions", AllowedRelations: []string{"owner"}, DataScoped: true},
}

type workflowEntityInput struct{}
type runIDInput struct {
	workflowEntityInput
	ID guid.ParamID `path:"id"`
}
type runApprovalInput struct {
	workflowEntityInput
	ID   guid.ParamID `path:"id"`
	Node string       `path:"node" minLength:"1" maxLength:"255"`
	Body struct {
		Approve  bool      `json:"approve"`
		Reason   string    `json:"reason" maxLength:"8192"`
		Feedback *Feedback `json:"feedback,omitempty"`
	}
}
type launchRunInput struct {
	workflowEntityInput
	Body LaunchRequest
}
type workflowIDInput struct {
	workflowEntityInput
	ID guid.ParamID `path:"id"`
}
type createWorkflowInput struct {
	workflowEntityInput
	Body struct {
		Key      string          `json:"key" minLength:"1" maxLength:"255"`
		Revision int64           `json:"revision" minimum:"1"`
		Name     string          `json:"name" minLength:"1" maxLength:"255"`
		Def      json.RawMessage `json:"def"`
	}
}
type workflowBody[T any] struct{ Body T }
type workflowOK struct {
	OK bool `json:"ok"`
}
type runCreated struct {
	RunID guid.ID `json:"runId"`
}
type runSummary struct {
	ID        guid.ID   `json:"id"`
	Status    RunStatus `json:"status"`
	Nodes     int       `json:"nodes"`
	CreatedAt int64     `json:"createdAt"`
}
type runDetail struct {
	ID     guid.ID      `json:"id"`
	Status RunStatus    `json:"status"`
	Def    *WorkflowDef `json:"def"`
}

func (m *Module) RegisterREST(api huma.API) {
	registerWorkflow(m, api, huma.Operation{OperationID: "workflow-run-launch", Method: http.MethodPost, Path: "/api/runs", Summary: "Launch a workflow run", Tags: []string{"workflow"}, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusUnprocessableEntity}}, "create", m.launchRun)
	registerWorkflow(m, api, huma.Operation{OperationID: "workflow-run-list", Method: http.MethodGet, Path: "/api/runs", Summary: "List workflow runs", Tags: []string{"workflow"}}, "view", m.listRuns)
	registerWorkflow(m, api, huma.Operation{OperationID: "workflow-run-get", Method: http.MethodGet, Path: "/api/runs/{id}", Summary: "Get a workflow run", Tags: []string{"workflow"}, Errors: []int{http.StatusNotFound}}, "view", m.getRun)
	registerWorkflow(m, api, huma.Operation{OperationID: "workflow-run-pause", Method: http.MethodPost, Path: "/api/runs/{id}/pause", Summary: "Pause a workflow run", Tags: []string{"workflow"}, Errors: []int{http.StatusNotFound, http.StatusConflict}}, "update", m.pauseRun)
	registerWorkflow(m, api, huma.Operation{OperationID: "workflow-run-resume", Method: http.MethodPost, Path: "/api/runs/{id}/resume", Summary: "Resume a workflow run", Tags: []string{"workflow"}, Errors: []int{http.StatusNotFound, http.StatusConflict}}, "update", m.resumeRun)
	registerWorkflow(m, api, huma.Operation{OperationID: "workflow-run-cancel", Method: http.MethodPost, Path: "/api/runs/{id}/cancel", Summary: "Cancel a workflow run", Tags: []string{"workflow"}, Errors: []int{http.StatusNotFound, http.StatusConflict}}, "update", m.cancelRun)
	registerWorkflow(m, api, huma.Operation{OperationID: "workflow-run-approval", Method: http.MethodPost, Path: "/api/runs/{id}/approvals/{node}", Summary: "Resolve a workflow approval", Tags: []string{"workflow"}, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity}}, "update", m.approveRun)
	m.registerEventWebSocket(api)
	registerWorkflow(m, api, huma.Operation{OperationID: "workflow-definition-list", Method: http.MethodGet, Path: "/api/workflows", Summary: "List workflow definitions", Tags: []string{"workflow"}}, "view", m.listWorkflows)
	registerWorkflow(m, api, huma.Operation{OperationID: "workflow-definition-create", Method: http.MethodPost, Path: "/api/workflows", Summary: "Create an immutable workflow revision", Tags: []string{"workflow"}, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusConflict, http.StatusUnprocessableEntity}}, "create", m.createWorkflow)
	registerWorkflow(m, api, huma.Operation{OperationID: "workflow-definition-delete", Method: http.MethodDelete, Path: "/api/workflows/{id}", Summary: "Delete a workflow definition", Tags: []string{"workflow"}, Errors: []int{http.StatusNotFound}}, "delete", m.deleteWorkflow)
}

func registerWorkflow[I, O any](m *Module, api huma.API, op huma.Operation, verb string, handler func(context.Context, *I) (*O, error)) {
	authz.RegisterEntity(m.registrar, api, op, authz.Guard{Resource: "workflow", Verb: verb}, handler)
}

func (m *Module) launchRun(ctx context.Context, input *launchRunInput) (*workflowBody[runCreated], error) {
	run, err := m.manager.Launch(ctx, input.Body)
	if err != nil {
		return nil, workflowValidationError(err)
	}
	return &workflowBody[runCreated]{Body: runCreated{RunID: run.ID}}, nil
}

func (m *Module) listRuns(ctx context.Context, _ *workflowEntityInput) (*workflowBody[[]runSummary], error) {
	entityID := authn.EntityIDFromContext(ctx)
	out := make([]runSummary, 0)
	seen := map[guid.ID]bool{}
	for _, run := range m.manager.ListFor(entityID) {
		seen[run.ID] = true
		out = append(out, runSummary{ID: run.ID, Status: run.Status(), Nodes: len(run.Def.Nodes), CreatedAt: run.created.UTC().UnixMilli()})
	}
	stored, err := m.repository.ListRunDefinitions(ctx, 0, 500)
	if err != nil {
		return nil, workflowAPIError(err)
	}
	for _, run := range stored {
		if !seen[run.ID] {
			out = append(out, runSummary{ID: run.ID, Status: run.Status, Nodes: nodeCountFromSnapshot(run.DefJSON), CreatedAt: run.CreatedAt})
		}
	}
	return &workflowBody[[]runSummary]{Body: out}, nil
}

func (m *Module) getRun(ctx context.Context, input *runIDInput) (*workflowBody[runDetail], error) {
	if run, ok := m.manager.GetFor(guid.ID(input.ID), authn.EntityIDFromContext(ctx)); ok {
		return &workflowBody[runDetail]{Body: runDetail{ID: run.ID, Status: run.Status(), Def: run.Def}}, nil
	}
	stored, err := m.repository.GetRunDef(ctx, guid.ID(input.ID))
	if err != nil {
		return nil, workflowAPIError(err)
	}
	var def WorkflowDef
	if json.Unmarshal([]byte(stored.DefJSON), &def) != nil {
		return nil, huma.Error500InternalServerError("workflow.unavailable")
	}
	return &workflowBody[runDetail]{Body: runDetail{ID: stored.ID, Status: stored.Status, Def: &def}}, nil
}

func (m *Module) pauseRun(ctx context.Context, input *runIDInput) (*workflowBody[workflowOK], error) {
	return m.controlRun(ctx, input, m.manager.Pause)
}
func (m *Module) resumeRun(ctx context.Context, input *runIDInput) (*workflowBody[workflowOK], error) {
	return m.controlRun(ctx, input, m.manager.Resume)
}
func (m *Module) cancelRun(ctx context.Context, input *runIDInput) (*workflowBody[workflowOK], error) {
	return m.controlRun(ctx, input, m.manager.Cancel)
}
func (m *Module) controlRun(ctx context.Context, input *runIDInput, action func(guid.ID) error) (*workflowBody[workflowOK], error) {
	if _, ok := m.manager.GetFor(guid.ID(input.ID), authn.EntityIDFromContext(ctx)); !ok {
		return nil, huma.Error404NotFound("workflow.run_not_found")
	}
	if err := action(guid.ID(input.ID)); err != nil {
		return nil, huma.Error409Conflict("workflow.run_state_conflict")
	}
	return &workflowBody[workflowOK]{Body: workflowOK{OK: true}}, nil
}

func (m *Module) approveRun(ctx context.Context, input *runApprovalInput) (*workflowBody[workflowOK], error) {
	if _, ok := m.manager.GetFor(guid.ID(input.ID), authn.EntityIDFromContext(ctx)); !ok {
		return nil, huma.Error404NotFound("workflow.run_not_found")
	}
	if !input.Body.Approve {
		if input.Body.Feedback == nil || input.Body.Feedback.Validate() != nil {
			return nil, huma.Error422UnprocessableEntity("workflow.feedback_invalid")
		}
	}
	if err := m.manager.Approve(guid.ID(input.ID), input.Node, input.Body.Approve, input.Body.Reason, input.Body.Feedback); err != nil {
		return nil, huma.Error409Conflict("workflow.approval_conflict")
	}
	return &workflowBody[workflowOK]{Body: workflowOK{OK: true}}, nil
}

func (m *Module) listWorkflows(ctx context.Context, _ *workflowEntityInput) (*workflowBody[[]WorkflowDefModel], error) {
	items, err := m.repository.ListWorkflows(ctx)
	if err != nil {
		return nil, workflowAPIError(err)
	}
	for i := range items {
		var value any
		if json.Unmarshal([]byte(items[i].DefJSON), &value) != nil {
			return nil, huma.Error500InternalServerError("workflow.unavailable")
		}
		items[i].DefRaw = value
	}
	return &workflowBody[[]WorkflowDefModel]{Body: items}, nil
}

func (m *Module) createWorkflow(ctx context.Context, input *createWorkflowInput) (*workflowBody[WorkflowDefModel], error) {
	var definition WorkflowDef
	if len(input.Body.Def) == 0 || json.Unmarshal(input.Body.Def, &definition) != nil || definition.Validate() != nil {
		return nil, huma.Error422UnprocessableEntity("workflow.definition_invalid")
	}
	id, err := m.nextID()
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("workflow.id_unavailable")
	}
	model := WorkflowDefModel{ID: id, WorkflowKey: input.Body.Key, Revision: input.Body.Revision, Name: input.Body.Name, DefJSON: string(input.Body.Def), DefRaw: any(definition)}
	if err := m.repository.SaveWorkflow(ctx, model); err != nil {
		return nil, workflowAPIError(err)
	}
	return &workflowBody[WorkflowDefModel]{Body: model}, nil
}

func (m *Module) deleteWorkflow(ctx context.Context, input *workflowIDInput) (*workflowBody[workflowOK], error) {
	if _, err := m.repository.GetWorkflow(ctx, guid.ID(input.ID)); err != nil {
		return nil, workflowAPIError(err)
	}
	if err := m.repository.DeleteWorkflow(ctx, guid.ID(input.ID)); err != nil {
		return nil, workflowAPIError(err)
	}
	return &workflowBody[workflowOK]{Body: workflowOK{OK: true}}, nil
}

func (m *Module) registerEventWebSocket(api huma.API) {
	op := huma.Operation{OperationID: "workflow-run-events", Method: http.MethodGet, Path: "/api/runs/{id}/events", Summary: "Stream workflow run events", Tags: []string{"workflow"}, Parameters: []*huma.Param{{Name: "id", In: "path", Required: true, Schema: guid.ID(0).Schema(api.OpenAPI().Components.Schemas)}}, Errors: []int{http.StatusBadRequest, http.StatusNotFound}}
	authz.RegisterEntityAdapter(m.registrar, api, op, authz.Guard{Resource: "workflow", Verb: "view"}, func(hctx huma.Context) {
		id, err := guid.Parse(hctx.Param("id"))
		if err != nil {
			_ = huma.WriteErr(api, hctx, http.StatusBadRequest, "workflow.run_id_invalid")
			return
		}
		run, ok := m.manager.GetFor(id, authn.EntityIDFromContext(hctx.Context()))
		if !ok {
			_ = huma.WriteErr(api, hctx, http.StatusNotFound, "workflow.run_not_found")
			return
		}
		request, writer := humachi.Unwrap(hctx)
		upgrader := websocket.Upgrader{CheckOrigin: m.origin.Allows}
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		sub, history, unsubscribe := run.subscribe()
		defer unsubscribe()
		for _, event := range history {
			if conn.WriteJSON(event) != nil {
				return
			}
		}
		for {
			select {
			case <-request.Context().Done():
				return
			case event := <-sub:
				if conn.WriteJSON(event) != nil {
					return
				}
			}
		}
	})
}

func workflowValidationError(error) error {
	return huma.Error422UnprocessableEntity("workflow.run_invalid")
}
func workflowAPIError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return huma.Error404NotFound("workflow.not_found")
	}
	return huma.Error500InternalServerError("workflow.unavailable")
}
