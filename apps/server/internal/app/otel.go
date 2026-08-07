package app

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// otelHandler enriches slog records with trace_id and span_id from the active
// OpenTelemetry span before delegating to the underlying handler.
type otelHandler struct {
	next slog.Handler
}

func newOtelHandler(next slog.Handler) slog.Handler {
	return &otelHandler{next: next}
}

func (h *otelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *otelHandler) Handle(ctx context.Context, r slog.Record) error {
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		r.AddAttrs(
			slog.String("trace_id", span.SpanContext().TraceID().String()),
			slog.String("span_id", span.SpanContext().SpanID().String()),
		)
	}
	return h.next.Handle(ctx, r)
}

func (h *otelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &otelHandler{next: h.next.WithAttrs(attrs)}
}

func (h *otelHandler) WithGroup(name string) slog.Handler {
	return &otelHandler{next: h.next.WithGroup(name)}
}
