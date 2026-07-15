package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
)

type Service interface {
	PermissionCatalog(context.Context) []authz.Action
	SpiceDBSchema(context.Context) string
	ScopeModel(context.Context) []ScopeNode
	MenuCatalog(context.Context) []MenuItem
}

type ScopeNode struct {
	Type       string `json:"type" doc:"SpiceDB object type"`
	ParentType string `json:"parent_type,omitempty" doc:"parent object type"`
	Relation   string `json:"relation" doc:"relation used to connect to parent or administer"`
	Label      string `json:"label" doc:"display label"`
}

type MenuItem struct {
	ID             string     `json:"id"`
	Label          string     `json:"label"`
	Path           string     `json:"path,omitempty"`
	PermissionCode string     `json:"permission_code"`
	Children       []MenuItem `json:"children,omitempty"`
}

type schemaOutput struct {
	Schema string `json:"schema"`
}

// RegisterREST mounts IAM discovery endpoints for the management UI.
func RegisterREST(a huma.API, svc Service) {
	huma.Register(a, huma.Operation{
		OperationID: "iam-permission-catalog",
		Method:      http.MethodGet,
		Path:        "/iam/permission-catalog",
		Summary:     "List declared permissions",
		Tags:        []string{"iam"},
	}, func(ctx context.Context, _ *struct{}) (*respx.Body[[]authz.Action], error) {
		return respx.OK(ctx, svc.PermissionCatalog(ctx)), nil
	})

	huma.Register(a, huma.Operation{
		OperationID: "iam-spicedb-schema",
		Method:      http.MethodGet,
		Path:        "/iam/spicedb/schema",
		Summary:     "Return the generated SpiceDB schema",
		Tags:        []string{"iam"},
	}, func(ctx context.Context, _ *struct{}) (*respx.Body[schemaOutput], error) {
		return respx.OK(ctx, schemaOutput{Schema: svc.SpiceDBSchema(ctx)}), nil
	})

	huma.Register(a, huma.Operation{
		OperationID: "iam-scope-model",
		Method:      http.MethodGet,
		Path:        "/iam/scope-model",
		Summary:     "List platform tenant merchant store scope model",
		Tags:        []string{"iam"},
	}, func(ctx context.Context, _ *struct{}) (*respx.Body[[]ScopeNode], error) {
		return respx.OK(ctx, svc.ScopeModel(ctx)), nil
	})

	huma.Register(a, huma.Operation{
		OperationID: "iam-menu-catalog",
		Method:      http.MethodGet,
		Path:        "/iam/menu-catalog",
		Summary:     "List menu metadata bound to permission codes",
		Tags:        []string{"iam"},
	}, func(ctx context.Context, _ *struct{}) (*respx.Body[[]MenuItem], error) {
		return respx.OK(ctx, svc.MenuCatalog(ctx)), nil
	})
}
