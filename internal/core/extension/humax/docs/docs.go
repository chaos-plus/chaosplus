// Package docs serves API documentation UIs for a huma API mounted on a chi
// router. It mounts a tabbed page at /docs hosting all five renderers (Scalar,
// Swagger UI, ReDoc, Stoplight, openapi-ui), each also reachable at its own
// /docs/<name> path. Spec URLs are derived from config.OpenAPIPath, not
// hardcoded, and pages are served as raw HTML and never touch huma.
package docs

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
)

// htmlContentType is used for every custom docs page served here.
const htmlContentType = "text/html; charset=utf-8"

// Register mounts the tabbed docs page and each standalone renderer page on the
// chi router, and returns a copy of config with huma's built-in /docs disabled
// (DocsPath cleared) so it does not overwrite the tabbed page. The spec URLs are
// derived from config.OpenAPIPath (the same value huma uses to serve
// /openapi.json and /openapi.yaml), so custom OpenAPI paths keep working.
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
