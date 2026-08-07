// Package secure provides a small chi middleware that sets conservative security
// response headers. It is transport-only (no dependency on the app), so any chi
// router can mount it.
package secure

import "net/http"

// hstsValue is a 1-year HSTS policy including subdomains. Only sent when HSTS is
// enabled, which must be the case only when the API is served over HTTPS.
const hstsValue = "max-age=31536000; includeSubDomains"

// Headers returns a middleware that sets baseline security headers on every
// response. When hsts is true it also sends Strict-Transport-Security; enable
// that only behind TLS, since sending it over plain HTTP would wrongly pin
// clients to HTTPS.
func Headers(hsts bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			if hsts {
				h.Set("Strict-Transport-Security", hstsValue)
			}
			next.ServeHTTP(w, r)
		})
	}
}
