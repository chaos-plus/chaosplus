package geoip

import (
	"net"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

// IPv4 selection ranks (higher wins). The self lookup emits a single IPv4
// address and prefers a public one, matching "prefer the public IPv4" policy:
// public > private > loopback > everything else (link-local, unspecified, ...).
const (
	rankOther    = 0
	rankLoopback = 1
	rankPrivate  = 2
	rankPublic   = 3
)

// detectClientIPv4 inspects the forwarding headers and the transport peer and
// returns the single best IPv4 address for the caller in dotted-quad form.
// Only IPv4 is emitted: an IPv6 loopback (::1, what a browser uses when it hits
// localhost) collapses to 127.0.0.1 so the self lookup stays usable on dev
// boxes, and any other IPv6 candidate is ignored. Returns "" when no IPv4
// candidate exists.
func detectClientIPv4(ctx huma.Context) string {
	best := ""
	bestRank := -1
	for _, cand := range clientIPCandidates(ctx) {
		v4 := normalizeIPv4(cand)
		if v4 == "" {
			continue
		}
		if r := rankIPv4(v4); r > bestRank {
			best, bestRank = v4, r
		}
	}
	return best
}

// clientIPCandidates gathers raw address strings from X-Forwarded-For (left to
// right, so the original client comes first), then X-Real-IP, then the
// transport remote address. Order only breaks ties: at equal rank the earliest
// candidate wins.
func clientIPCandidates(ctx huma.Context) []string {
	var out []string
	if xff := ctx.Header("X-Forwarded-For"); xff != "" {
		for _, part := range strings.Split(xff, ",") {
			if p := strings.TrimSpace(part); p != "" {
				out = append(out, p)
			}
		}
	}
	if xr := strings.TrimSpace(ctx.Header("X-Real-IP")); xr != "" {
		out = append(out, xr)
	}
	if host := hostOnly(ctx.RemoteAddr()); host != "" {
		out = append(out, host)
	}
	return out
}

// isIPv4 reports whether s is a valid dotted-quad IPv4 address (x.x.x.x). It
// rejects IPv6 (including IPv4-mapped forms like ::ffff:1.2.3.4) so the lookup
// path param is strictly IPv4.
func isIPv4(s string) bool {
	ip := net.ParseIP(strings.TrimSpace(s))
	return ip != nil && ip.To4() != nil && !strings.Contains(s, ":")
}

// normalizeIPv4 returns the dotted-quad form of an address, or "" when it is not
// usable as IPv4. IPv4-mapped IPv6 (::ffff:a.b.c.d) collapses to a.b.c.d, IPv6
// loopback maps to 127.0.0.1, and any other IPv6 is rejected.
func normalizeIPv4(s string) string {
	ip := net.ParseIP(strings.TrimSpace(s))
	if ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	if ip.IsLoopback() {
		return "127.0.0.1"
	}
	return ""
}

// rankIPv4 scores a (valid) IPv4 address by class so the selector can prefer
// public addresses. Private is checked before global unicast because RFC1918
// addresses also report as global unicast.
func rankIPv4(s string) int {
	ip := net.ParseIP(s)
	switch {
	case ip == nil:
		return rankOther
	case ip.IsLoopback():
		return rankLoopback
	case ip.IsPrivate():
		return rankPrivate
	case ip.IsGlobalUnicast():
		return rankPublic
	default:
		return rankOther
	}
}
