// Package ratex provides a Redis-backed rate-limit middleware for the chi router.
// Limiting is expressed as independent dimensions (e.g. per-IP, per-account),
// each keyed by a pluggable extractor and enforced with a GCRA token bucket
// (github.com/go-redis/redis_rate). It fails open: when Redis is unavailable the
// request is allowed and a warning is logged, so a limiter outage never takes
// down the API. Exceeded requests get a localized 429 in the standard respx
// envelope, with Retry-After and X-RateLimit-* headers.
package ratex

import (
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/v9"

	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
)

// tooManyRequestsKey is the i18n key for the 429 message (see pkg/i18n/locales).
const tooManyRequestsKey = "too_many_requests"

// Dimension is one rate-limit axis. Key derives the bucket key from a request;
// returning "" skips this dimension for that request (e.g. an anonymous caller
// has no account id). Limit is the GCRA rate/burst/period.
type Dimension struct {
	Name  string
	Key   func(*http.Request) string
	Limit redis_rate.Limit
}

// Limiter is the chi middleware enforcing the configured dimensions against Redis.
type Limiter struct {
	rl     *redis_rate.Limiter
	prefix string
	dims   []Dimension
}

// New builds a Limiter over rdb. The universal client accepts standalone,
// sentinel, and cluster deployments alike. prefix namespaces every key so
// multiple apps can share one Redis instance.
func New(rdb redis.UniversalClient, prefix string, dims ...Dimension) *Limiter {
	return &Limiter{rl: redis_rate.NewLimiter(rdb), prefix: prefix, dims: dims}
}

// Limit builds a GCRA limit; burst defaults to rate when non-positive.
func Limit(rate int, period time.Duration, burst int) redis_rate.Limit {
	if burst <= 0 {
		burst = rate
	}
	return redis_rate.Limit{Rate: rate, Period: period, Burst: burst}
}

// IPKey extracts the client IP as a bucket key. chi's RealIP middleware sets
// r.RemoteAddr to the resolved client IP, so this reflects the real caller.
func IPKey(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// HeaderKey returns an extractor that reads the given request header (e.g. the
// account id header). Swap it for a context-based extractor once auth lands.
func HeaderKey(name string) func(*http.Request) string {
	return func(r *http.Request) string { return r.Header.Get(name) }
}

// Handler is the chi middleware. Each configured dimension is checked in order;
// the first to exceed its limit ends the request with a 429.
func (l *Limiter) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, d := range l.dims {
			id := d.Key(r)
			if id == "" {
				continue // dimension not applicable to this request
			}
			res, err := l.rl.Allow(r.Context(), l.prefix+":"+d.Name+":"+id, d.Limit)
			if err != nil {
				// Fail open: never let a limiter outage take down the API.
				slog.Warn("ratelimit: redis unavailable, allowing request", "dimension", d.Name, "error", err)
				continue
			}
			setRateHeaders(w, d.Limit, res)
			if res.Allowed == 0 {
				respx.WriteError(w, r, http.StatusTooManyRequests, tooManyRequestsKey, res.RetryAfter)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// setRateHeaders advertises the limit state to clients on the current dimension.
func setRateHeaders(w http.ResponseWriter, limit redis_rate.Limit, res *redis_rate.Result) {
	h := w.Header()
	h.Set("X-RateLimit-Limit", strconv.Itoa(limit.Rate))
	h.Set("X-RateLimit-Remaining", strconv.Itoa(res.Remaining))
	h.Set("X-RateLimit-Reset", strconv.Itoa(int(math.Ceil(res.ResetAfter.Seconds()))))
}
