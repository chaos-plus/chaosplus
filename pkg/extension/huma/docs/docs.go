// Package docs serves API documentation UIs for a huma API running on GoFr.
//
// It exposes either huma's single built-in renderer or a tabbed page that hosts
// all five renderers (Scalar, Swagger UI, ReDoc, Stoplight, openapi-ui) at once,
// each also reachable at its own /docs/<name> path. The choice is driven by the
// DOCS_RENDERER config value (env var or configs/.env), so it can change without
// a rebuild. Spec URLs are derived from config.OpenAPIPath, not hardcoded.
package docs

import (
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/http/response"
)

// RendererEnv is the config key that selects the docs UI.
const RendererEnv = "DOCS_RENDERER"

// htmlContentType is used for every custom docs page served here.
const htmlContentType = "text/html; charset=utf-8"

// Apply selects the documentation UI from the RendererEnv config value (env var
// or configs/.env). Supported values are case-insensitive:
//
//	all          tabbed page exposing all 5 renderers at once (default)
//	             (also: multi, tabs)
//	scalar       huma built-in single renderer — Scalar
//	swagger-ui   huma built-in single renderer — Swagger UI (also: swaggerui, swagger)
//	stoplight    huma built-in single renderer — Stoplight Elements (also: elements)
//	none         disable docs entirely (also: off, disabled, false)
//
// It returns the (copied, not mutated) config plus a bool that is true when the
// tabbed multi-renderer UI should be registered with Register. In multi and none
// modes huma's own /docs route is disabled so we can serve our own (or nothing).
// An unknown value logs a warning and falls back to the tabbed page, so a typo
// can never make huma panic at startup.
func Apply(app *gofr.App, config huma.Config) (huma.Config, bool) {
	raw := app.Config.GetOrDefault(RendererEnv, "all")

	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "all", "multi", "tabs":
		config.DocsPath = "" // we serve our own tabbed /docs instead
		return config, true
	case "scalar":
		config.DocsRenderer = huma.DocsRendererScalar
		return config, false
	case "swagger-ui", "swaggerui", "swagger":
		config.DocsRenderer = huma.DocsRendererSwaggerUI
		return config, false
	case "stoplight", "elements":
		config.DocsRenderer = huma.DocsRendererStoplightElements
		return config, false
	case "none", "off", "disabled", "false":
		config.DocsPath = ""
		return config, false
	default:
		app.Logger().Warnf("unknown %s %q, falling back to the tabbed docs page", RendererEnv, raw)
		config.DocsPath = ""
		return config, true
	}
}

// Register registers the tabbed docs page and each standalone renderer page as
// GoFr routes. The spec URLs are derived from config.OpenAPIPath (the same value
// huma uses to serve /openapi.json and /openapi.yaml), so custom OpenAPI paths
// keep working. Pages are served as raw HTML and never touch huma. Call this
// only when Apply reports multi mode.
func Register(app *gofr.App, config huma.Config) {
	specJSON := config.OpenAPIPath + ".json"
	specYAML := config.OpenAPIPath + ".yaml"

	pages := []struct {
		path string
		html func() []byte
	}{
		{"/docs", func() []byte { return docsWrapperHTML(specJSON) }},
		{"/docs/scalar", func() []byte { return scalarHTML(specJSON) }},
		{"/docs/swagger", func() []byte { return swaggerHTML(specJSON) }},
		{"/docs/redoc", func() []byte { return redocHTML(specJSON) }},
		{"/docs/stoplight", func() []byte { return stoplightHTML(specYAML) }},
		{"/docs/openapi-ui", func() []byte { return openapiUIHTML(specJSON) }},
	}

	for _, page := range pages {
		html := page.html // capture per iteration
		app.GET(page.path, func(*gofr.Context) (any, error) {
			return response.File{Content: html(), ContentType: htmlContentType}, nil
		})
	}
}
