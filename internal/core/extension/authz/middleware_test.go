package authz

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/spicedbx"
)

type fakeChecker struct {
	allowed bool
	err     error
}

func (f fakeChecker) Check(context.Context, spicedbx.ObjectRef, string, spicedbx.SubjectRef, spicedbx.ZedToken) (bool, error) {
	return f.allowed, f.err
}
func (f fakeChecker) WriteRelationships(context.Context, []spicedbx.Relationship) (spicedbx.ZedToken, error) {
	return "", nil
}
func (f fakeChecker) LookupResources(context.Context, string, string, spicedbx.SubjectRef) ([]string, error) {
	return nil, nil
}
func (f fakeChecker) LookupSubjects(context.Context, spicedbx.ObjectRef, string, string) ([]spicedbx.SubjectRef, error) {
	return nil, nil
}

func TestTenantMiddleware(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := TenantMiddleware(fakeChecker{allowed: true}, Guard{Resource: "store", Verb: "view"}, nil)(next)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(TenantHeader, "t1")
	req = req.WithContext(authn.WithClaims(req.Context(), &authn.Claims{Subject: "u1"}))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestTenantMiddlewareRejects(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	rec := httptest.NewRecorder()
	TenantMiddleware(fakeChecker{allowed: true}, Guard{Resource: "store", Verb: "view"}, nil)(next).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(authn.WithClaims(req.Context(), &authn.Claims{Subject: "u1"}))
	rec = httptest.NewRecorder()
	TenantMiddleware(fakeChecker{allowed: true}, Guard{Resource: "store", Verb: "view"}, nil)(next).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	req.Header.Set(TenantHeader, "t1")
	rec = httptest.NewRecorder()
	TenantMiddleware(fakeChecker{err: errors.New("spicedb down")}, Guard{Resource: "store", Verb: "view"}, nil)(next).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}
