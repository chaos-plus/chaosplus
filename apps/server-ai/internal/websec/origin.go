package websec

import (
	"net/http"
	"net/url"
	"os"
	"strings"
)

// OriginAllowed permits non-browser clients, same-origin browser connections,
// and origins explicitly listed in CONTROL_ALLOWED_ORIGINS.
func OriginAllowed(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	for _, allowed := range strings.Split(os.Getenv("CONTROL_ALLOWED_ORIGINS"), ",") {
		if strings.EqualFold(strings.TrimRight(strings.TrimSpace(allowed), "/"), strings.TrimRight(origin, "/")) {
			return true
		}
	}
	return false
}
