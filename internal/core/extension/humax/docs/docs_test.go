package docs

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/secure"
	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

// TestBuildersEmbedSpecURL proves the pages reference the spec URL passed in
// (derived from config.OpenAPIPath) rather than a hardcoded /openapi.json.
func TestBuildersEmbedSpecURL(t *testing.T) {
	const spec = "/custom/api-spec.json"

	builders := map[string]func(string) []byte{
		"scalar":     func(s string) []byte { return scalarHTML("test", s) },
		"swagger":    func(s string) []byte { return swaggerHTML("test", s) },
		"redoc":      func(s string) []byte { return redocHTML("test", s) },
		"stoplight":  func(s string) []byte { return stoplightHTML("test", s) },
		"openapi-ui": func(s string) []byte { return openapiUIHTML("test", s) },
		"wrapper":    func(s string) []byte { return docsWrapperHTML("test", s) },
	}

	for name, build := range builders {
		if !strings.Contains(string(build(spec)), spec) {
			t.Errorf("%s page does not reference spec URL %q", name, spec)
		}
	}
}

func TestRegisterAllowsDocsFramingOnly(t *testing.T) {
	r := chi.NewRouter()
	r.Use(secure.Headers(false))
	Register(r, huma.DefaultConfig("test", "1.0.0"), "test")
	r.Get("/api-test", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	for _, path := range []string{"/", "/docs", "/docs/scalar", "/docs/swagger", "/docs/redoc", "/docs/stoplight", "/docs/openapi-ui"} {
		t.Run(path, func(t *testing.T) {
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))

			assert.Equal(t, http.StatusOK, rr.Code)
			assert.Empty(t, rr.Header().Get("X-Frame-Options"))
		})
	}

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api-test", nil))
	assert.Equal(t, http.StatusNoContent, rr.Code)
	assert.Equal(t, "DENY", rr.Header().Get("X-Frame-Options"))
}
