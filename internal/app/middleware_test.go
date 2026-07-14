package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

// buildRouter mounts the security + CORS middleware from cfg onto a bare router
// with a trivial handler, mirroring how StartRestServer wires them.
func buildRouter(cfg Config) chi.Router {
	app := &App{cfg: cfg}
	r := chi.NewMux()
	app.useSecurity(r)
	app.useCors(r)
	r.Get("/", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	return r
}

func TestUseSecurity_SendsHeadersWhenEnabled(t *testing.T) {
	r := buildRouter(Config{Security: Security{Enabled: true, HSTS: true}})
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, "nosniff", rr.Header().Get("X-Content-Type-Options"))
	assert.Contains(t, rr.Header().Get("Strict-Transport-Security"), "max-age=")
}

func TestUseSecurity_DisabledSendsNothing(t *testing.T) {
	r := buildRouter(Config{Security: Security{Enabled: false}})
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Empty(t, rr.Header().Get("X-Content-Type-Options"))
}

func TestUseCors_PreflightAllowsOrigin(t *testing.T) {
	r := buildRouter(Config{Cors: Cors{Enabled: true}}) // defaults: all origins/methods/headers
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	assert.NotEmpty(t, rr.Header().Get("Access-Control-Allow-Origin"), "preflight is answered")
}

func TestUseCors_DisabledNoHeaders(t *testing.T) {
	r := buildRouter(Config{Cors: Cors{Enabled: false}})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	assert.Empty(t, rr.Header().Get("Access-Control-Allow-Origin"))
}
