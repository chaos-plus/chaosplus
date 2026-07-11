// Package docs serves API documentation UIs for a huma API mounted on a chi
// router.
//
// It exposes either huma's single built-in renderer or a tabbed page that hosts
// all five renderers (Scalar, Swagger UI, ReDoc, Stoplight, openapi-ui) at once,
// each also reachable at its own /docs/<name> path. The choice is driven by the
// DOCS_RENDERER value (env var or configs/.env), so it can change without a
// rebuild. Spec URLs are derived from config.OpenAPIPath, not hardcoded.
package docs

import (
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
)

// htmlContentType is used for every custom docs page served here.
const htmlContentType = "text/html; charset=utf-8"

// warner is the minimal logging surface Apply needs, so docs stays decoupled
// from any concrete logger. *logx.Logger satisfies it.
type warner interface {
	Warnf(format string, args ...any)
}

// Apply selects the documentation UI from the given renderer value (typically
// the DOCS_RENDERER env var). Supported values are case-insensitive:
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
// An unknown value logs a warning (when logger is non-nil) and falls back to the
// tabbed page, so a typo can never make huma panic at startup.
func Apply(renderer string, logger warner, config huma.Config) (huma.Config, bool) {
	switch strings.ToLower(strings.TrimSpace(renderer)) {
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
		if logger != nil {
			logger.Warnf("unknown docs renderer %q, falling back to the tabbed docs page", renderer)
		}
		config.DocsPath = ""
		return config, true
	}
}

// Register mounts the tabbed docs page and each standalone renderer page on the
// chi router. The spec URLs are derived from config.OpenAPIPath (the same value
// huma uses to serve /openapi.json and /openapi.yaml), so custom OpenAPI paths
// keep working. Pages are served as raw HTML and never touch huma. Call this
// only when Apply reports multi mode.
func Register(r chi.Router, config huma.Config, name string) {
	specJSON := config.OpenAPIPath + ".json"
	specYAML := config.OpenAPIPath + ".yaml"

	pages := []struct {
		path string
		html func() []byte
	}{
		{"/docs", func() []byte { return docsWrapperHTML(name, specJSON) }},
		{"/docs/scalar", func() []byte { return scalarHTML(name, specJSON) }},
		{"/docs/swagger", func() []byte { return swaggerHTML(name, specJSON) }},
		{"/docs/redoc", func() []byte { return redocHTML(name, specJSON) }},
		{"/docs/stoplight", func() []byte { return stoplightHTML(name, specYAML) }},
		{"/docs/openapi-ui", func() []byte { return openapiUIHTML(name, specJSON) }},
	}

	for _, page := range pages {
		html := page.html // capture per iteration
		r.Get(page.path, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", htmlContentType)
			_, _ = w.Write(html())
		})
	}
}
