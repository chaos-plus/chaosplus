package respx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chaos-plus/chaosplus/pkg/i18n"
	"github.com/stretchr/testify/assert"
)

func TestLocaleMiddleware(t *testing.T) {
	// capture records the canonical locale the middleware placed on the context.
	capture := func(dst *string) http.Handler {
		return Locale(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*dst = i18n.LocaleFromContext(r.Context())
		}))
	}

	tests := []struct {
		name       string
		query      string
		xLang      string
		acceptLang string
		wantLocale string
	}{
		{name: "query wins over header and accept-language", query: "zh-CN", xLang: "en-US", acceptLang: "ms-MY", wantLocale: "zh-CN"},
		{name: "x-lang wins over accept-language", xLang: "zh-CN", acceptLang: "en-US", wantLocale: "zh-CN"},
		{name: "accept-language used when no override", acceptLang: "zh-CN,en;q=0.9", wantLocale: "zh-CN"},
		{name: "language-prefix normalized", acceptLang: "zh-TW", wantLocale: "zh-CN"},
		{name: "unsupported falls back to base", acceptLang: "fr-FR", wantLocale: i18n.Base},
		{name: "empty falls back to base", wantLocale: i18n.Base},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			target := "/"
			if tc.query != "" {
				target += "?lang=" + tc.query
			}
			req := httptest.NewRequest(http.MethodGet, target, nil)
			if tc.xLang != "" {
				req.Header.Set("X-Lang", tc.xLang)
			}
			if tc.acceptLang != "" {
				req.Header.Set("Accept-Language", tc.acceptLang)
			}
			capture(&got).ServeHTTP(httptest.NewRecorder(), req)
			assert.Equal(t, tc.wantLocale, got)
		})
	}
}
