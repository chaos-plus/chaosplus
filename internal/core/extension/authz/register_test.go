package authz

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/spicedbx"
)

type routeOutput struct {
	Body string
}

type fakeTokenVerifier struct {
	claims *authn.Claims
	err    error
}

func (f fakeTokenVerifier) VerifyAuthorization(context.Context, string) (*authn.Claims, error) {
	return f.claims, f.err
}

type recordingChecker struct {
	allowed    bool
	err        error
	resource   spicedbx.ObjectRef
	permission string
	subject    spicedbx.SubjectRef
}

func (f *recordingChecker) Check(_ context.Context, resource spicedbx.ObjectRef, permission string, subject spicedbx.SubjectRef, _ spicedbx.ZedToken) (bool, error) {
	f.resource = resource
	f.permission = permission
	f.subject = subject
	return f.allowed, f.err
}

func TestRegisterDeclaresGuard(t *testing.T) {
	_, api := humatest.New(t)
	registry := MustRegistry(Action{Resource: "store", Verb: "view"})
	registrar := NewDeclarationOnlyRegistrar(registry)

	Register(registrar, api, huma.Operation{
		OperationID: "view-store",
		Method:      http.MethodPost,
		Path:        "/stores/query",
	}, Guard{Resource: "store", Verb: "view"}, func(context.Context, *struct{}) (*routeOutput, error) {
		return &routeOutput{Body: "ok"}, nil
	})

	op := api.OpenAPI().Paths["/stores/query"].Post
	assert.Equal(t, "store_view", op.Extensions[GuardExtensionKey])
	assert.Contains(t, op.Errors, http.StatusUnauthorized)
	assert.Contains(t, op.Errors, http.StatusForbidden)
	assert.NoError(t, ValidateOperations(api, registry))
	assert.Equal(t, http.StatusOK, api.Post("/stores/query").Code)
}

func TestRegisterEnforcesAuthnAndAuthz(t *testing.T) {
	registry := MustRegistry(Action{Resource: "store", Verb: "view"})
	guard := Guard{Resource: "store", Verb: "view"}

	t.Run("unauthenticated", func(t *testing.T) {
		_, api := humatest.New(t)
		registrar := NewRegistrar(registry, fakeTokenVerifier{err: errors.New("bad token")}, &recordingChecker{})
		registerTestRoute(api, registrar, guard)
		assert.Equal(t, http.StatusUnauthorized, api.Get("/guarded").Code)
	})

	t.Run("missing tenant", func(t *testing.T) {
		_, api := humatest.New(t)
		registrar := NewRegistrar(registry, fakeTokenVerifier{claims: &authn.Claims{Subject: "u1"}}, &recordingChecker{allowed: true})
		registerTestRoute(api, registrar, guard)
		assert.Equal(t, http.StatusForbidden, api.Get("/guarded", "Authorization: Bearer token").Code)
	})

	t.Run("denied", func(t *testing.T) {
		_, api := humatest.New(t)
		registrar := NewRegistrar(registry, fakeTokenVerifier{claims: &authn.Claims{Subject: "u1"}}, &recordingChecker{})
		registerTestRoute(api, registrar, guard)
		assert.Equal(t, http.StatusForbidden, api.Get("/guarded", "Authorization: Bearer token", TenantHeader+": t1").Code)
	})

	t.Run("backend unavailable", func(t *testing.T) {
		_, api := humatest.New(t)
		registrar := NewRegistrar(registry, fakeTokenVerifier{claims: &authn.Claims{Subject: "u1"}}, &recordingChecker{err: errors.New("down")})
		registerTestRoute(api, registrar, guard)
		assert.Equal(t, http.StatusServiceUnavailable, api.Get("/guarded", "Authorization: Bearer token", TenantHeader+": t1").Code)
	})

	t.Run("allowed", func(t *testing.T) {
		_, api := humatest.New(t)
		checker := &recordingChecker{allowed: true}
		registrar := NewRegistrar(registry, fakeTokenVerifier{claims: &authn.Claims{Subject: "u1"}}, checker)
		registerTestRoute(api, registrar, guard)
		assert.Equal(t, http.StatusOK, api.Get("/guarded", "Authorization: Bearer token", TenantHeader+": t1").Code)
		assert.Equal(t, "tenant:t1", checker.resource.String())
		assert.Equal(t, "store_view", checker.permission)
		assert.Equal(t, "user:u1", checker.subject.String())
	})
}

func TestOperationGate(t *testing.T) {
	registry := MustRegistry(Action{Resource: "store", Verb: "update"})

	t.Run("rejects any unguarded operation", func(t *testing.T) {
		_, api := humatest.New(t)
		huma.Register(api, huma.Operation{OperationID: "view-store", Method: http.MethodGet, Path: "/stores/{id}"}, func(context.Context, *struct{}) (*routeOutput, error) {
			return &routeOutput{}, nil
		})
		err := ValidateOperations(api, registry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "GET /stores/{id}")
	})

	t.Run("allows explicit public mutation", func(t *testing.T) {
		_, api := humatest.New(t)
		RegisterPublic(api, huma.Operation{OperationID: "oidc-callback", Method: http.MethodPost, Path: "/oidc/callback"}, func(context.Context, *struct{}) (*routeOutput, error) { return &routeOutput{}, nil })
		assert.NoError(t, ValidateOperations(api, registry))
	})
}

func TestRegistrarValidation(t *testing.T) {
	registry := MustRegistry(Action{Resource: "store", Verb: "view"})
	assert.Panics(t, func() { NewRegistrar(nil, fakeTokenVerifier{}, &recordingChecker{}) })
	assert.Panics(t, func() { NewDeclarationOnlyRegistrar(nil) })
	assert.Panics(t, func() { NewRegistrar(registry, nil, nil) })
	assert.Panics(t, func() { NewRegistrar(registry, fakeTokenVerifier{}, nil) })
	assert.Panics(t, func() {
		_, api := humatest.New(t)
		Register(NewDeclarationOnlyRegistrar(registry), api, huma.Operation{OperationID: "bad", Method: http.MethodGet, Path: "/bad"}, Guard{Resource: "missing", Verb: "view"}, func(context.Context, *struct{}) (*routeOutput, error) {
			return &routeOutput{}, nil
		})
	})
}

func registerTestRoute(api huma.API, registrar *Registrar, guard Guard) {
	Register(registrar, api, huma.Operation{OperationID: "guarded", Method: http.MethodGet, Path: "/guarded"}, guard, func(context.Context, *struct{}) (*routeOutput, error) {
		return &routeOutput{Body: "ok"}, nil
	})
}
