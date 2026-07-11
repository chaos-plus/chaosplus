package docs

import (
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
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

func TestApply(t *testing.T) {
	cases := []struct {
		env          string
		multi        bool
		renderer     string
		docsDisabled bool
	}{
		{"all", true, "", true},
		{"tabs", true, "", true},
		{"scalar", false, huma.DocsRendererScalar, false},
		{"swagger", false, huma.DocsRendererSwaggerUI, false},
		{"stoplight", false, huma.DocsRendererStoplightElements, false},
		{"none", false, "", true},
		{"bogus", true, "", true},
	}

	for _, c := range cases {
		t.Run(c.env, func(t *testing.T) {
			config, multi := Apply(c.env, nil, huma.DefaultConfig("T", "1.0.0"))

			assert.Equal(t, c.multi, multi)
			if c.renderer != "" {
				assert.Equal(t, c.renderer, config.DocsRenderer)
			}
			if c.docsDisabled {
				assert.Equal(t, "", config.DocsPath)
			}
		})
	}
}
