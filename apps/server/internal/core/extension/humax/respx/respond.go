package respx

import (
	"context"
	"net/http"
	"time"
)

// startKey keys the request start time stored by Timing.
type startKey struct{}

// Timing is a chi middleware that records the request start time so OK and List
// can report request_at and elapsed_ms in the response meta. Mount it early, so
// the measured time covers as much of the request as possible.
func Timing(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), startKey{}, time.Now())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// OK wraps data in a success envelope (code 0). Message carries the i18n key
// "success"; LocalizeMessage resolves it to the request locale at serialize time.
func OK[T any](ctx context.Context, data T) *Body[T] {
	return &Body[T]{Body: Envelope[T]{Message: "success", Meta: metaOf(ctx, nil), Data: data}}
}

// List wraps data in a success envelope carrying pagination meta. Message carries
// the i18n key "success" (see OK).
func List[T any](ctx context.Context, data T, page Page) *Body[T] {
	return &Body[T]{Body: Envelope[T]{Message: "success", Meta: metaOf(ctx, &page), Data: data}}
}

// metaOf builds response meta from the request start time stored by Timing. The
// elapsed time is measured up to here (handler completion), before the response
// is serialized — that is the price of carrying the duration in the body.
//
// RequestAt is emitted in UTC (RFC3339 "Z"): timestamps are UTC end to end and
// only converted to a display timezone by the client. start keeps its monotonic
// reading so ElapsedMS stays accurate; only the wall-clock RequestAt is UTC-normalized.
func metaOf(ctx context.Context, page *Page) Meta {
	start := startOf(ctx)
	return Meta{RequestAt: start.UTC(), ElapsedMS: float64(time.Since(start).Nanoseconds()) / 1e6, Page: page}
}

func startOf(ctx context.Context) time.Time {
	if t, ok := ctx.Value(startKey{}).(time.Time); ok {
		return t
	}
	return time.Now()
}
