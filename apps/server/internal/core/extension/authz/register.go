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
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

const (
	guardMetadataKey         = "authz.guard"
	tenantMemberMetadataKey  = "authz.tenant-member"
	authenticatedMetadataKey = "authz.authenticated"
	platformMetadataKey      = "authz.platform"
	publicMetadataKey        = "authz.public"
	GuardExtensionKey        = "x-authz-permission"
	TenantHeader             = "X-Tenant-Id"
	EntityHeader             = "X-Entity-Id"
	TenantQuery              = "tenant_id"
	EntityQuery              = "entity_id"
	BearerScheme             = "bearerAuth"
	SessionScheme            = "sessionCookie"
	ClientBasicScheme        = "clientBasic"
	defaultCookieName        = "cp_session"
)

// PermissionChecker is the narrow local authorization capability needed on the request path.
type PermissionChecker interface {
	Check(ctx context.Context, tenantID guid.ID, permission string, subject guid.ID) (bool, error)
	CheckPlatform(ctx context.Context, permission string, subject guid.ID) (bool, error)
}

// EntityPermissionChecker evaluates a permission at one tenant entity. It is
// implemented by the IAM owner and consumed by entity-scoped business routes.
type EntityPermissionChecker interface {
	CheckEntity(ctx context.Context, tenantID, entityID guid.ID, permission string, subject guid.ID) (bool, error)
}

type TokenVerifier interface {
	Authenticate(context.Context, string, string) (*authn.Claims, error)
}

type csrfValidator interface {
	ValidateCSRF(method, origin, cookieHeader, authorization string) error
}

type MembershipChecker interface {
	IsMemberActive(context.Context, guid.ID, guid.ID) (bool, error)
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

// RegisterEntity registers a guarded business operation whose selected entity
// is verified by IAM before the handler receives trusted snowflake claims.
func RegisterEntity[I, O any](r *Registrar, api huma.API, op huma.Operation, guard Guard, handler func(context.Context, *I) (*O, error)) {
	if r == nil {
		panic("authz registrar is nil")
	}
	if r.checker != nil {
		if _, ok := r.checker.(EntityPermissionChecker); !ok {
			panic("entity operation requires entity permission checker")
		}
	}
	r.prepare(api, &op, guard)
	op.Parameters = append(op.Parameters, &huma.Param{
		Name: EntityHeader, In: "header", Description: "Authorized entity resource boundary", Required: true,
		Schema: &huma.Schema{Type: huma.TypeString, Pattern: `^[1-9][0-9]*$`},
	})
	if r.verifier != nil {
		// Replace the tenant-only guard installed by prepare with the entity guard.
		op.Middlewares[len(op.Middlewares)-1] = r.entityMiddleware(api, guard)
	}
	huma.Register(api, op, handler)
}

// RegisterEntityAdapter registers an entity-authorized operation that needs
// direct access to the Huma transport context, such as WebSocket upgrades or
// streaming protocols. It executes the same global and entity authorization
// middleware chain as typed Huma handlers before invoking the raw handler.
func RegisterEntityAdapter(r *Registrar, api huma.API, op huma.Operation, guard Guard, handler func(huma.Context)) {
	if r == nil || api == nil || handler == nil {
		panic("entity adapter operation requires registrar, api, and handler")
	}
	if r.checker != nil {
		if _, ok := r.checker.(EntityPermissionChecker); !ok {
			panic("entity operation requires entity permission checker")
		}
	}
	r.prepare(api, &op, guard)
	op.Parameters = removeOperationParameter(op.Parameters, TenantHeader, "header")
	op.Parameters = append(op.Parameters,
		&huma.Param{Name: TenantQuery, In: "query", Description: "Tenant authorization selector", Required: true, Schema: &huma.Schema{Type: huma.TypeString, Pattern: `^[1-9][0-9]*$`}},
		&huma.Param{Name: EntityQuery, In: "query", Description: "Entity authorization selector", Required: true, Schema: &huma.Schema{Type: huma.TypeString, Pattern: `^[1-9][0-9]*$`}},
	)
	if r.verifier != nil {
		op.Middlewares[len(op.Middlewares)-1] = r.entityMiddlewareFor(api, guard, func(ctx huma.Context) (string, string) {
			return ctx.Query(TenantQuery), ctx.Query(EntityQuery)
		})
	}
	api.OpenAPI().AddOperation(&op)
	api.Adapter().Handle(&op, api.Middlewares().Handler(op.Middlewares.Handler(handler)))
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

// RegisterAuthenticated declares an authenticated operation that does NOT bind a
// tenant (no X-Tenant-Id required, no membership check). Claims are injected so
// the handler can resolve the caller. Use for personal/cross-tenant queries like
// "my tenants".
func RegisterAuthenticated[I, O any](r *Registrar, api huma.API, op huma.Operation, handler func(context.Context, *I) (*O, error)) {
	if r == nil {
		panic("authz registrar is nil")
	}
	if op.OperationID == "" {
		panic("authenticated operation requires operation id")
	}
	if op.Metadata == nil {
		op.Metadata = map[string]any{}
	}
	op.Metadata[authenticatedMetadataKey] = true
	r.prepareAuthenticated(api, &op, false)
	if r.verifier != nil {
		op.Middlewares = append(op.Middlewares, r.authenticatedOnly(api))
	}
	huma.Register(api, op, handler)
}

// authenticatedOnly verifies the session and injects claims without requiring a
// tenant selector or membership.
func (r *Registrar) authenticatedOnly(api huma.API) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		claims, err := r.verifier.Authenticate(ctx.Context(), ctx.Header("Authorization"), ctx.Header("Cookie"))
		if err != nil {
			slog.Debug("authn token rejected", "operation", ctx.Operation().OperationID, "err", err)
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "unauthorized")
			return
		}
		trusted := policyx.WithTrustedContext(ctx.Context(), policyx.TrustedContext{
			ACR: claims.ACR, AMR: claims.AMR, ClientID: claims.ClientID, NetworkZone: claims.NetworkZone,
		})
		next(huma.WithContext(ctx, authn.WithClaims(trusted, claims)))
	}
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
		tenantText := ctx.Header(TenantHeader)
		tenantID, tenantErr := guid.Parse(tenantText)
		if tenantErr != nil || tenantID.Zero() || claims.PrincipalID.Zero() {
			_ = huma.WriteErr(api, ctx, http.StatusForbidden, "forbidden")
			return
		}
		active, err := r.members.IsMemberActive(requestContext, tenantID, claims.PrincipalID)
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
			allowed, err := r.checker.Check(requestContext, tenantID, guard.Code(), claims.PrincipalID)
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

func (r *Registrar) entityMiddleware(api huma.API, guard Guard) func(huma.Context, func(huma.Context)) {
	return r.entityMiddlewareFor(api, guard, func(ctx huma.Context) (string, string) {
		return ctx.Header(TenantHeader), ctx.Header(EntityHeader)
	})
}

func (r *Registrar) entityMiddlewareFor(api huma.API, guard Guard, selectors func(huma.Context) (string, string)) func(huma.Context, func(huma.Context)) {
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
		tenantText, entityText := selectors(ctx)
		tenantID, tenantErr := guid.Parse(tenantText)
		entityID, entityErr := guid.Parse(entityText)
		principalID := claims.PrincipalID
		if principalID.Zero() {
			principalID, err = guid.Parse(claims.Subject)
		}
		if tenantErr != nil || entityErr != nil || err != nil || tenantID.Zero() || entityID.Zero() || principalID.Zero() {
			_ = huma.WriteErr(api, ctx, http.StatusForbidden, "forbidden")
			return
		}
		requestContext := policyx.WithTrustedContext(ctx.Context(), policyx.TrustedContext{
			ACR: claims.ACR, AMR: claims.AMR, ClientID: claims.ClientID, NetworkZone: claims.NetworkZone,
		})
		active, err := r.members.IsMemberActive(requestContext, tenantID, principalID)
		if err != nil {
			_ = huma.WriteErr(api, ctx, http.StatusServiceUnavailable, "authorization_unavailable")
			return
		}
		if !active {
			_ = huma.WriteErr(api, ctx, http.StatusForbidden, "inactive_tenant_membership")
			return
		}
		checker := r.checker.(EntityPermissionChecker)
		allowed, err := checker.CheckEntity(requestContext, tenantID, entityID, guard.Code(), principalID)
		if err != nil {
			_ = huma.WriteErr(api, ctx, http.StatusServiceUnavailable, "authorization_unavailable")
			return
		}
		if !allowed {
			_ = huma.WriteErr(api, ctx, http.StatusForbidden, "forbidden")
			return
		}
		trustedClaims := *claims
		trustedClaims.TenantID = tenantID
		trustedClaims.EntityID = entityID
		trustedClaims.PrincipalID = principalID
		next(huma.WithContext(ctx, authn.WithClaims(requestContext, &trustedClaims)))
	}
}

func removeOperationParameter(parameters []*huma.Param, name, location string) []*huma.Param {
	out := parameters[:0]
	for _, parameter := range parameters {
		if parameter != nil && parameter.Name == name && parameter.In == location {
			continue
		}
		out = append(out, parameter)
	}
	return out
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
		allowed, err := r.checker.CheckPlatform(requestContext, guard.Code(), claims.PrincipalID)
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

func isAuthenticated(op *huma.Operation) bool {
	value, _ := op.Metadata[authenticatedMetadataKey].(bool)
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
