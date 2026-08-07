// Package metrics exposes a Prometheus /metrics endpoint and an HTTP middleware
// that records request count, latency, and status-code distribution.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	chi "github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	requestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "chaosplus_http_requests_total", Help: "Total HTTP requests."},
		[]string{"method", "path", "status"},
	)
	requestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "chaosplus_http_request_duration_seconds",
			Help:    "HTTP request latency in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)
)

func init() {
	prometheus.MustRegister(requestsTotal, requestDuration)
}

func chiRoutePattern(r *http.Request) string {
	if ctx := chi.RouteContext(r.Context()); ctx != nil {
		return ctx.RoutePattern()
	}
	return ""
}

// Handler returns an http.Handler that serves Prometheus text metrics.
func Handler() http.Handler {
	return promhttp.Handler()
}

// Middleware records request count and duration keyed by method + matched route
// pattern (from chi's RoutePattern). Mount after chi router so RoutePattern works.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		pattern := chiRoutePattern(r)
		if pattern == "" {
			pattern = r.URL.Path
		}
		status := strconv.Itoa(ww.Status())
		requestsTotal.WithLabelValues(r.Method, pattern, status).Inc()
		requestDuration.WithLabelValues(r.Method, pattern).Observe(time.Since(start).Seconds())
	})
}
