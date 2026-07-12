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

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
)

// htmlContentType is used for every custom docs page served here.
const htmlContentType = "text/html; charset=utf-8"

// Register mounts the tabbed docs page and each standalone renderer page on the
// chi router. The spec URLs are derived from config.OpenAPIPath (the same value
// huma uses to serve /openapi.json and /openapi.yaml), so custom OpenAPI paths
// keep working. Pages are served as raw HTML and never touch huma. Call this
// only when Apply reports multi mode.
func Register(r chi.Router, config huma.Config, name string) huma.Config {

	config.DocsPath = ""

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
	return config
}
