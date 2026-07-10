package docs

import (
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"

	"gofr.dev/pkg/gofr"
)

// TestBuildersEmbedSpecURL proves the pages reference the spec URL passed in
// (derived from config.OpenAPIPath) rather than a hardcoded /openapi.json.
func TestBuildersEmbedSpecURL(t *testing.T) {
	const spec = "/custom/api-spec.json"

	builders := map[string]func(string) []byte{
		"scalar":     scalarHTML,
		"swagger":    swaggerHTML,
		"redoc":      redocHTML,
		"stoplight":  stoplightHTML,
		"openapi-ui": openapiUIHTML,
		"wrapper":    docsWrapperHTML,
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
			t.Setenv("METRICS_PORT", "0")
			t.Setenv(RendererEnv, c.env)
			app := gofr.New()

			config, multi := Apply(app, huma.DefaultConfig("T", "1.0.0"))

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
