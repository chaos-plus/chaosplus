package secure

import (
	"net/http"
	"net/url"
	"strings"
)

// OriginPolicy validates browser Origin headers for HTTP upgrades such as
// WebSocket. Empty Origin is accepted for non-browser protocol clients. Browser
// origins must be same-origin or present in the explicit allowlist.
type OriginPolicy struct {
	allowed map[string]struct{}
}

// SameOriginPolicy rejects every cross-origin browser request while allowing
// same-origin browsers and protocol clients that do not send Origin.
func SameOriginPolicy() OriginPolicy { return OriginPolicy{allowed: map[string]struct{}{}} }

func NewOriginPolicy(origins []string) (OriginPolicy, error) {
	policy := OriginPolicy{allowed: make(map[string]struct{}, len(origins))}
	for _, raw := range origins {
		origin, err := canonicalOrigin(raw)
		if err != nil {
			return OriginPolicy{}, err
		}
		policy.allowed[origin] = struct{}{}
	}
	return policy, nil
}

func (p OriginPolicy) Allows(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	canonical, err := canonicalOrigin(origin)
	if err != nil {
		return false
	}
	u, _ := url.Parse(canonical)
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	_, ok := p.allowed[canonical]
	return ok
}

func canonicalOrigin(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", ErrInvalidOrigin
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host), nil
}
