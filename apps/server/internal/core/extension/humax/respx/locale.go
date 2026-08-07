package respx

import (
	"net/http"

	"github.com/chaos-plus/chaosplus/pkg/i18n"
)

// langQuery and langHeader are the explicit per-request locale overrides that win
// over the browser's Accept-Language.
const (
	langQuery  = "lang"
	langHeader = "X-Lang"
)

// Locale is a chi middleware that resolves the request locale and stores its
// canonical form on the context for LocalizeMessage (and any handler) to use.
// Precedence: ?lang= query → X-Lang header → Accept-Language header. The raw
// value is normalized by i18n.Canonical, which falls back to i18n.Base when the
// input is empty or unsupported. Mount it before the huma API is served.
func Locale(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.URL.Query().Get(langQuery)
		if raw == "" {
			raw = r.Header.Get(langHeader)
		}
		if raw == "" {
			raw = r.Header.Get("Accept-Language")
		}
		ctx := i18n.WithLocale(r.Context(), i18n.Canonical(raw))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
