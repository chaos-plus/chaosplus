package artifact

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/danielgtaylor/huma/v2"
)

var Actions = []authz.Action{
	{Resource: "artifact", Verb: "view", Scope: "entity", Summary: "view workflow artifacts", AllowedRelations: []string{"owner", "editor", "viewer"}, DataScoped: true, Menu: true},
	{Resource: "artifact", Verb: "update", Scope: "entity", Summary: "review and reconcile workflow artifacts", AllowedRelations: []string{"owner", "editor"}, DataScoped: true},
}

type artifactEntityInput struct{}

type listArtifactsInput struct {
	artifactEntityInput
	ProjectID guid.ID `query:"projectId" required:"false"`
	Status    string  `query:"status" enum:"valid,stale,invalid,orphaned" required:"false"`
}

type artifactIDInput struct {
	artifactEntityInput
	ID guid.ID `path:"id"`
}

type artifactBody[T any] struct{ Body T }
type artifactOK struct {
	OK bool `json:"ok"`
}

func (m *Module) RegisterREST(api huma.API) {
	authz.RegisterEntity(m.registrar, api, huma.Operation{OperationID: "artifact-list", Method: http.MethodGet, Path: "/api/artifacts", Summary: "List workflow artifacts", Tags: []string{"artifact"}}, authz.Guard{Resource: "artifact", Verb: "view"}, m.listArtifacts)
	authz.RegisterEntity(m.registrar, api, huma.Operation{OperationID: "artifact-force-valid", Method: http.MethodPost, Path: "/api/artifacts/{id}/force-valid", Summary: "Apply a human validation override", Tags: []string{"artifact"}, Errors: []int{http.StatusNotFound}}, authz.Guard{Resource: "artifact", Verb: "update"}, m.forceValid)
	authz.RegisterEntity(m.registrar, api, huma.Operation{OperationID: "artifact-reconcile", Method: http.MethodPost, Path: "/api/artifacts/reconcile", Summary: "Reconcile artifacts with runner storage", Tags: []string{"artifact"}}, authz.Guard{Resource: "artifact", Verb: "update"}, m.reconcile)
}

func (m *Module) listArtifacts(ctx context.Context, input *listArtifactsInput) (*artifactBody[[]Artifact], error) {
	items, err := m.repository.ListArtifacts(ctx, authn.EntityIDFromContext(ctx), input.ProjectID, input.Status)
	if err != nil {
		return nil, artifactAPIError(err)
	}
	return &artifactBody[[]Artifact]{Body: items}, nil
}

func (m *Module) forceValid(ctx context.Context, input *artifactIDInput) (*artifactBody[artifactOK], error) {
	if err := m.repository.ForceValidateArtifact(ctx, input.ID, authn.PrincipalIDFromContext(ctx)); err != nil {
		return nil, artifactAPIError(err)
	}
	return &artifactBody[artifactOK]{Body: artifactOK{OK: true}}, nil
}

func (m *Module) reconcile(ctx context.Context, _ *artifactEntityInput) (*artifactBody[ReconcileReport], error) {
	report, err := m.service.Reconcile(ctx)
	if err != nil {
		return nil, artifactAPIError(err)
	}
	return &artifactBody[ReconcileReport]{Body: report}, nil
}

func artifactAPIError(err error) error {
	if errors.Is(err, ErrArtifactNotFound) || errors.Is(err, sql.ErrNoRows) {
		return huma.Error404NotFound("artifact.not_found")
	}
	return huma.Error500InternalServerError("artifact.unavailable")
}
