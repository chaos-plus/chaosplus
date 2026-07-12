package docs

import (
	"strings"
	"testing"
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
