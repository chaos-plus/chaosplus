package authz

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/policyx"
)

const (
	guardMetadataKey        = "authz.guard"
	tenantMemberMetadataKey = "authz.tenant-member"
	platformMetadataKey     = "authz.platform"
	publicMetadataKey       = "authz.public"
	GuardExtensionKey       = "x-authz-permission"
	TenantHeader            = "X-Tenant-Id"
	BearerScheme            = "bearerAuth"
	SessionScheme           = "sessionCookie"
	ClientBasicScheme       = "clientBasic"
	defaultCookieName       = "cp_session"
)

// PermissionChecker is the narrow local authorization capability needed on the request path.
type PermissionChecker interface {
	Check(ctx context.Context, tenantID, permission, subject string) (bool, error)
	CheckPlatform(ctx context.Context, permission, subject string) (bool, error)
}

type TokenVerifier interface {
	Authenticate(context.Context, string, string) (*authn.Claims, error)
}

type csrfValidator interface {
	ValidateCSRF(method, origin, cookieHeader, authorization string) error
}

type MembershipChecker interface {
	IsMemberActive(context.Context, string, string) (bool, error)
}

type sessionCookieNamer interface {
	SessionCookieName() string
}

// Registrar binds route declarations, OpenAPI metadata, authentication, and
// local authorization from one call site.
type Registrar struct {
	registry *Registry
	verifier TokenVerifier
	checker  PermissionChecker
	members  MembershipChecker
}

func NewRegistrar(registry *Registry, verifier TokenVerifier, checker PermissionChecker, members MembershipChecker) *Registrar {
	if registry == nil {
		panic("authz registrar requires a registry")
	}
	if verifier == nil || checker == nil || members == nil {
		panic("authz registrar requires verifier, permission checker, and membership checker")
	}
	return &Registrar{registry: registry, verifier: verifier, checker: checker, members: members}
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

func (r *Registrar) IsDeclarationOnly() bool {
	return r.verifier == nil && r.checker == nil
}

// Register declares and registers one guarded Huma operation.
func Register[I, O any](r *Registrar, api huma.API, op huma.Operation, guard Guard, handler func(context.Context, *I) (*O, error)) {
	if r == nil {
		panic("authz registrar is nil")
	}
	r.prepare(api, &op, guard)
	huma.Register(api, op, handler)
}

// RegisterTenantMember registers a self-service operation available to every
// authenticated active member of the selected tenant. The handler remains
// tenant scoped, but does not require an administrative permission.
func RegisterTenantMember[I, O any](r *Registrar, api huma.API, op huma.Operation, handler func(context.Context, *I) (*O, error)) {
	if r == nil {
		panic("authz registrar is nil")
	}
	if op.OperationID == "" {
		panic("tenant member operation requires operation id")
	}
	if op.Metadata == nil {
		op.Metadata = map[string]any{}
	}
	op.Metadata[tenantMemberMetadataKey] = true
	r.prepareAuthenticated(api, &op, true)
	if r.verifier != nil {
		op.Middlewares = append(op.Middlewares, r.middleware(api, nil))
	}
	huma.Register(api, op, handler)
}

// RegisterPlatform declares an authenticated platform operation. Platform
// routes never accept a tenant selector and authorize against platform grants.
func RegisterPlatform[I, O any](r *Registrar, api huma.API, op huma.Operation, guard Guard, handler func(context.Context, *I) (*O, error)) {
	if r == nil {
		panic("authz registrar is nil")
	}
	if op.OperationID == "" {
		panic("platform operation requires operation id")
	}
	action, ok := r.registry.Find(guard.Code())
	if !ok || action.Scope != "platform" {
		panic("platform operation uses non-platform permission: " + guard.Code())
	}
	if op.Metadata == nil {
		op.Metadata = map[string]any{}
	}
	op.Metadata[guardMetadataKey] = guard.Code()
	op.Metadata[platformMetadataKey] = true
	if op.Extensions == nil {
		op.Extensions = map[string]any{}
	}
	op.Extensions[GuardExtensionKey] = guard.Code()
	r.prepareAuthenticated(api, &op, false)
	if r.verifier != nil {
		op.Middlewares = append(op.Middlewares, r.platformMiddleware(api, guard))
	}
	huma.Register(api, op, handler)
}

// RegisterPublic registers an operation that intentionally bypasses the
// authorization Guard gateway. It may still perform authentication in its handler.
func RegisterPublic[I, O any](api huma.API, op huma.Operation, handler func(context.Context, *I) (*O, error)) {
	Public(&op)
	EnsureSecuritySchemes(api, defaultCookieName)
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
	r.prepareAuthenticated(api, op, true)

	if r.verifier != nil {
		op.Middlewares = append(op.Middlewares, r.middleware(api, &guard))
	}
}

func (r *Registrar) prepareAuthenticated(api huma.API, op *huma.Operation, tenantScoped bool) {
	op.Errors = appendUniqueStatus(op.Errors, http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable)
	cookieName := defaultCookieName
	if provider, ok := r.verifier.(sessionCookieNamer); ok && provider.SessionCookieName() != "" {
		cookieName = provider.SessionCookieName()
	}
	EnsureSecuritySchemes(api, cookieName)
	op.Security = UserSecurity()
	if tenantScoped {
		op.Parameters = append(op.Parameters, &huma.Param{
			Name:        TenantHeader,
			In:          "header",
			Description: "Tenant authorization boundary",
			Required:    true,
			Schema:      &huma.Schema{Type: huma.TypeString, MaxLength: intPointer(128)},
		})
	}
}

// EnsureSecuritySchemes declares the authentication mechanisms used across the API.
func EnsureSecuritySchemes(api huma.API, cookieName string) {
	components := api.OpenAPI().Components
	if components.SecuritySchemes == nil {
		components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	}
	components.SecuritySchemes[BearerScheme] = &huma.SecurityScheme{Type: "http", Scheme: "bearer", BearerFormat: "JWT"}
	components.SecuritySchemes[SessionScheme] = &huma.SecurityScheme{Type: "apiKey", In: "cookie", Name: cookieName}
	components.SecuritySchemes[ClientBasicScheme] = &huma.SecurityScheme{Type: "http", Scheme: "basic"}
}

// UserSecurity documents the two equivalent authentication mechanisms accepted
// by first-party management endpoints.
func UserSecurity() []map[string][]string {
	return []map[string][]string{{BearerScheme: {}}, {SessionScheme: {}}}
}

func BearerSecurity() []map[string][]string {
	return []map[string][]string{{BearerScheme: {}}}
}

func ClientSecurity() []map[string][]string {
	return []map[string][]string{{ClientBasicScheme: {}}}
}

func intPointer(value int) *int { return &value }

func (r *Registrar) middleware(api huma.API, guard *Guard) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		if validator, ok := r.verifier.(csrfValidator); ok {
			if err := validator.ValidateCSRF(ctx.Method(), ctx.Header("Origin"), ctx.Header("Cookie"), ctx.Header("Authorization")); err != nil {
				_ = huma.WriteErr(api, ctx, http.StatusForbidden, "csrf_rejected")
				return
			}
		}
		claims, err := r.verifier.Authenticate(ctx.Context(), ctx.Header("Authorization"), ctx.Header("Cookie"))
		if err != nil {
			slog.Debug("authn token rejected", "operation", ctx.Operation().OperationID, "err", err)
			if errors.Is(err, authn.ErrUnavailable) {
				_ = huma.WriteErr(api, ctx, http.StatusServiceUnavailable, "authentication_unavailable")
				return
			}
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "unauthorized")
			return
		}
		trusted := policyx.TrustedContext{
			ACR: claims.ACR, AMR: claims.AMR, ClientID: claims.ClientID, NetworkZone: claims.NetworkZone,
		}
		requestContext := policyx.WithTrustedContext(ctx.Context(), trusted)
		tenantID := ctx.Header(TenantHeader)
		if tenantID == "" {
			_ = huma.WriteErr(api, ctx, http.StatusForbidden, "forbidden")
			return
		}
		active, err := r.members.IsMemberActive(requestContext, tenantID, claims.Subject)
		if err != nil {
			slog.Error("tenant membership check failed", "operation", ctx.Operation().OperationID, "err", err)
			_ = huma.WriteErr(api, ctx, http.StatusServiceUnavailable, "authorization_unavailable")
			return
		}
		if !active {
			_ = huma.WriteErr(api, ctx, http.StatusForbidden, "inactive_tenant_membership")
			return
		}
		if guard != nil {
			allowed, err := r.checker.Check(requestContext, tenantID, guard.Code(), claims.Subject)
			if err != nil {
				slog.Error("authz check failed", "operation", ctx.Operation().OperationID, "permission", guard.Code(), "err", err)
				_ = huma.WriteErr(api, ctx, http.StatusServiceUnavailable, "authorization_unavailable")
				return
			}
			if !allowed {
				_ = huma.WriteErr(api, ctx, http.StatusForbidden, "forbidden")
				return
			}
		}
		next(huma.WithContext(ctx, authn.WithClaims(requestContext, claims)))
	}
}

func (r *Registrar) platformMiddleware(api huma.API, guard Guard) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		if validator, ok := r.verifier.(csrfValidator); ok {
			if err := validator.ValidateCSRF(ctx.Method(), ctx.Header("Origin"), ctx.Header("Cookie"), ctx.Header("Authorization")); err != nil {
				_ = huma.WriteErr(api, ctx, http.StatusForbidden, "csrf_rejected")
				return
			}
		}
		claims, err := r.verifier.Authenticate(ctx.Context(), ctx.Header("Authorization"), ctx.Header("Cookie"))
		if err != nil {
			if errors.Is(err, authn.ErrUnavailable) {
				_ = huma.WriteErr(api, ctx, http.StatusServiceUnavailable, "authentication_unavailable")
				return
			}
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "unauthorized")
			return
		}
		requestContext := policyx.WithTrustedContext(ctx.Context(), policyx.TrustedContext{
			ACR: claims.ACR, AMR: claims.AMR, ClientID: claims.ClientID, NetworkZone: claims.NetworkZone,
		})
		allowed, err := r.checker.CheckPlatform(requestContext, guard.Code(), claims.Subject)
		if err != nil {
			slog.Error("platform authz check failed", "operation", ctx.Operation().OperationID, "permission", guard.Code(), "err", err)
			_ = huma.WriteErr(api, ctx, http.StatusServiceUnavailable, "authorization_unavailable")
			return
		}
		if !allowed {
			_ = huma.WriteErr(api, ctx, http.StatusForbidden, "forbidden")
			return
		}
		next(huma.WithContext(ctx, authn.WithClaims(requestContext, claims)))
	}
}

// Public marks an operation as intentionally outside the authorization Guard gateway.
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

func isTenantMember(op *huma.Operation) bool {
	if op == nil || op.Metadata == nil {
		return false
	}
	value, _ := op.Metadata[tenantMemberMetadataKey].(bool)
	return value
}

// IsTenantMemberOperation reports whether an operation uses the authenticated
// active-member declaration. It is exposed for repository-wide OpenAPI gates.
func IsTenantMemberOperation(op *huma.Operation) bool {
	return isTenantMember(op)
}

// IsPlatformOperation reports whether an operation uses platform authorization.
func IsPlatformOperation(op *huma.Operation) bool {
	if op == nil || op.Metadata == nil {
		return false
	}
	value, _ := op.Metadata[platformMetadataKey].(bool)
	return value
}

func invalidGuardMessage(op *huma.Operation, code string) string {
	return fmt.Sprintf("%s uses undeclared permission %q", op.OperationID, code)
}
