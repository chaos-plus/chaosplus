package secure

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func serve(t *testing.T, hsts bool) *httptest.ResponseRecorder {
	t.Helper()
	h := Headers(hsts)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	return rr
}

func TestHeaders_Baseline(t *testing.T) {
	h := serve(t, false).Header()
	assert.Equal(t, "nosniff", h.Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", h.Get("X-Frame-Options"))
	assert.Equal(t, "strict-origin-when-cross-origin", h.Get("Referrer-Policy"))
	assert.Equal(t, "same-origin", h.Get("Cross-Origin-Opener-Policy"))
	assert.Empty(t, h.Get("Strict-Transport-Security"), "no HSTS unless enabled")
}

func TestHeaders_HSTSWhenEnabled(t *testing.T) {
	assert.Contains(t, serve(t, true).Header().Get("Strict-Transport-Security"), "max-age=31536000")
}
