package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
)

// PingFunc is the readiness check. A nil error means the service is ready.
type PingFunc func(ctx context.Context) error

// RegisterREST mounts the health endpoints.
func RegisterREST(a huma.API, ping PingFunc) {
	authz.RegisterPublic(a, huma.Operation{
		OperationID: "health-live",
		Method:      http.MethodGet,
		Path:        "/health",
		Summary:     "Liveness check",
		Description: "Returns 200 when the process is alive.",
		Tags:        []string{"health"},
	}, func(ctx context.Context, _ *struct{}) (*respx.Body[map[string]string], error) {
		return respx.OK(ctx, map[string]string{"status": "ok"}), nil
	})

	authz.RegisterPublic(a, huma.Operation{
		OperationID: "health-ready",
		Method:      http.MethodGet,
		Path:        "/ready",
		Summary:     "Readiness check",
		Description: "Returns 200 when the service is ready to serve traffic; 503 otherwise.",
		Tags:        []string{"health"},
	}, func(ctx context.Context, _ *struct{}) (*respx.Body[map[string]string], error) {
		if err := ping(ctx); err != nil {
			return nil, huma.Error503ServiceUnavailable("not_ready", err)
		}
		return respx.OK(ctx, map[string]string{"status": "ready"}), nil
	})
}
