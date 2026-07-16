package authz

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/spicedbx"
)

const (
	guardMetadataKey  = "authz.guard"
	publicMetadataKey = "authz.public"
	GuardExtensionKey = "x-authz-permission"
	TenantHeader      = "X-Tenant-Id"
)

// PermissionChecker is the narrow SpiceDB capability needed on the request path.
type PermissionChecker interface {
	Check(context.Context, spicedbx.ObjectRef, string, spicedbx.SubjectRef, spicedbx.ZedToken) (bool, error)
}

type TokenVerifier interface {
	VerifyAuthorization(context.Context, string) (*authn.Claims, error)
}

// Registrar binds route declarations, OpenAPI metadata, authentication, and
// SpiceDB authorization from one call site.
type Registrar struct {
	registry *Registry
	verifier TokenVerifier
	checker  PermissionChecker
}

func NewRegistrar(registry *Registry, verifier TokenVerifier, checker PermissionChecker) *Registrar {
	if registry == nil {
		panic("authz registrar requires a registry")
	}
	if verifier == nil || checker == nil {
		panic("authz registrar requires both verifier and checker")
	}
	return &Registrar{registry: registry, verifier: verifier, checker: checker}
}

// NewDeclarationOnlyRegistrar creates a registrar that records Guard metadata
// without attaching enforcement. It is intentionally explicit and is only for
// tests and static route validation.
func NewDeclarationOnlyRegistrar(registry *Registry) *Registrar {
	if registry == nil {
		panic("authz declaration registrar requires a registry")
	}
	return &Registrar{registry: registry}
}

func (r *Registrar) Registry() *Registry {
	return r.registry
}

// Register declares and registers one guarded Huma operation.
func Register[I, O any](r *Registrar, api huma.API, op huma.Operation, guard Guard, handler func(context.Context, *I) (*O, error)) {
	if r == nil {
		panic("authz registrar is nil")
	}
	r.prepare(api, &op, guard)
	huma.Register(api, op, handler)
}

// RegisterPublic registers an operation that intentionally bypasses the
// SpiceDB Guard gateway. It may still perform authentication in its handler.
func RegisterPublic[I, O any](api huma.API, op huma.Operation, handler func(context.Context, *I) (*O, error)) {
	Public(&op)
	huma.Register(api, op, handler)
}

func (r *Registrar) prepare(api huma.API, op *huma.Operation, guard Guard) {
	if op.OperationID == "" {
		panic("guarded operation requires operation id")
	}
	if _, ok := r.registry.Find(guard.Code()); !ok {
		panic("guarded operation uses unknown permission: " + guard.Code())
	}
	if op.Metadata == nil {
		op.Metadata = map[string]any{}
	}
	op.Metadata[guardMetadataKey] = guard.Code()
	if op.Extensions == nil {
		op.Extensions = map[string]any{}
	}
	op.Extensions[GuardExtensionKey] = guard.Code()
	op.Errors = appendUniqueStatus(op.Errors, http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable)

	if r.verifier != nil {
		op.Middlewares = append(op.Middlewares, r.middleware(api, guard))
	}
}

func (r *Registrar) middleware(api huma.API, guard Guard) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		claims, err := r.verifier.VerifyAuthorization(ctx.Context(), ctx.Header("Authorization"))
		if err != nil {
			slog.Debug("authn token rejected", "operation", ctx.Operation().OperationID, "err", err)
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "unauthorized")
			return
		}
		tenantID := ctx.Header(TenantHeader)
		if tenantID == "" {
			_ = huma.WriteErr(api, ctx, http.StatusForbidden, "forbidden")
			return
		}
		allowed, err := r.checker.Check(
			ctx.Context(),
			spicedbx.ObjectRef{Type: "tenant", ID: tenantID},
			guard.Code(),
			claims.SubjectRef(),
			"",
		)
		if err != nil {
			slog.Error("authz check failed", "operation", ctx.Operation().OperationID, "permission", guard.Code(), "err", err)
			_ = huma.WriteErr(api, ctx, http.StatusServiceUnavailable, "authorization_unavailable")
			return
		}
		if !allowed {
			_ = huma.WriteErr(api, ctx, http.StatusForbidden, "forbidden")
			return
		}
		next(huma.WithContext(ctx, authn.WithClaims(ctx.Context(), claims)))
	}
}

// Public marks an operation as intentionally outside the SpiceDB Guard gateway.
// The route may still authenticate using another flow, such as /authn/me.
func Public(op *huma.Operation) {
	if op.Metadata == nil {
		op.Metadata = map[string]any{}
	}
	op.Metadata[publicMetadataKey] = true
}

func appendUniqueStatus(existing []int, values ...int) []int {
	for _, value := range values {
		found := false
		for _, current := range existing {
			if current == value {
				found = true
				break
			}
		}
		if !found {
			existing = append(existing, value)
		}
	}
	return existing
}

func guardOf(op *huma.Operation) (string, bool) {
	if op == nil || op.Metadata == nil {
		return "", false
	}
	code, ok := op.Metadata[guardMetadataKey].(string)
	return code, ok && code != ""
}

func isPublic(op *huma.Operation) bool {
	if op == nil || op.Metadata == nil {
		return false
	}
	value, _ := op.Metadata[publicMetadataKey].(bool)
	return value
}

func invalidGuardMessage(op *huma.Operation, code string) string {
	return fmt.Sprintf("%s uses undeclared permission %q", op.OperationID, code)
}
