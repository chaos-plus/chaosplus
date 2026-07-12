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

// OK wraps data in a success envelope (code 0, message "ok").
func OK[T any](ctx context.Context, data T) *Body[T] {
	return &Body[T]{Body: Envelope[T]{Message: "ok", Meta: metaOf(ctx, nil), Data: data}}
}

// List wraps data in a success envelope carrying pagination meta.
func List[T any](ctx context.Context, data T, page Page) *Body[T] {
	return &Body[T]{Body: Envelope[T]{Message: "ok", Meta: metaOf(ctx, &page), Data: data}}
}

// metaOf builds response meta from the request start time stored by Timing. The
// elapsed time is measured up to here (handler completion), before the response
// is serialized — that is the price of carrying the duration in the body.
func metaOf(ctx context.Context, page *Page) Meta {
	start := startOf(ctx)
	return Meta{RequestAt: start, ElapsedMS: time.Since(start).Milliseconds(), Page: page}
}

func startOf(ctx context.Context) time.Time {
	if t, ok := ctx.Value(startKey{}).(time.Time); ok {
		return t
	}
	return time.Now()
}
