package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func init() {
	// ponytail: reset between tests; caller must set per test.
	AuthToken = ""
}

func TestAuthMiddlewareNoTokenSetPassesAll(t *testing.T) {
	AuthToken = ""
	h := authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/api/runs", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("no auth token should pass, got %d", rec.Code)
	}
}

func TestAuthMiddlewareRejectsMissingToken(t *testing.T) {
	AuthToken = "secret"
	defer func() { AuthToken = "" }()
	h := authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))
	req := httptest.NewRequest("GET", "/api/runs", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("missing token should 401, got %d", rec.Code)
	}
}

func TestAuthMiddlewareAcceptsBearerToken(t *testing.T) {
	AuthToken = "secret"
	defer func() { AuthToken = "" }()
	var called bool
	h := authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/api/runs", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !called {
		t.Fatalf("valid bearer token should pass, got %d called=%v", rec.Code, called)
	}
}

func TestAuthMiddlewareAcceptsQueryParamToken(t *testing.T) {
	// WebSocket fallback: browsers can't set Authorization on upgrade.
	AuthToken = "secret"
	defer func() { AuthToken = "" }()
	var called bool
	h := authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/api/machines/ws?token=secret", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !called {
		t.Fatalf("query-param token should pass, got %d called=%v", rec.Code, called)
	}
}

func TestAuthMiddlewareRejectsWrongToken(t *testing.T) {
	AuthToken = "secret"
	defer func() { AuthToken = "" }()
	h := authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))
	req := httptest.NewRequest("GET", "/api/runs", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("wrong token should 401, got %d", rec.Code)
	}
}

func TestAuthMiddlewareRejectsNonBearerAuthHeader(t *testing.T) {
	AuthToken = "secret"
	defer func() { AuthToken = "" }()
	h := authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))
	req := httptest.NewRequest("GET", "/api/runs", nil)
	req.Header.Set("Authorization", "Basic secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("non-bearer auth should 401, got %d", rec.Code)
	}
}

func TestAuthMiddlewareSetsWWWAuthenticateHeader(t *testing.T) {
	AuthToken = "secret"
	defer func() { AuthToken = "" }()
	h := authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))
	req := httptest.NewRequest("GET", "/api/runs", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !strings.Contains(rec.Header().Get("WWW-Authenticate"), "Bearer") {
		t.Fatalf("401 must set WWW-Authenticate header, got %q", rec.Header())
	}
}
