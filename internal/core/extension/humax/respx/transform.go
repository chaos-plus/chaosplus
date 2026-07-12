package respx

import (
	"context"
	"strings"

	"github.com/chaos-plus/chaosplus/pkg/i18n"
	"github.com/danielgtaylor/huma/v2"
)

// localizer is implemented by every envelope whose Message is an i18n key. Both
// the success Envelope (value receiver) and errorEnvelope (pointer receiver)
// implement it, so LocalizeMessage can localize either without reflection.
type localizer interface {
	// localize resolves Message against the request locale carried by ctx and
	// returns the value to serialize.
	localize(ctx context.Context) any
}

// localize resolves the success Message key to the request locale. It operates on
// a copy (value receiver), leaving the caller's envelope untouched.
func (e Envelope[T]) localize(ctx context.Context) any {
	e.Message = i18n.TContext(ctx, e.Message)
	return e
}

// localize translates every key segment of Message ("<summary>" or
// "<summary>: <detail>[; <detail>...]") to the request locale: the summary and
// each detail token are resolved independently, and unknown keys (e.g. huma's
// English framework text) pass through. It is idempotent: on a second pass the
// already-translated segments are unknown keys and pass through unchanged.
func (e *errorEnvelope) localize(ctx context.Context) any {
	summary, detail, hasDetail := strings.Cut(e.Message, msgSep)
	msg := i18n.TContext(ctx, summary)
	if hasDetail {
		parts := strings.Split(detail, detailSep)
		for i, p := range parts {
			parts[i] = i18n.TContext(ctx, p)
		}
		msg = msg + msgSep + strings.Join(parts, detailSep)
	}
	e.Message = msg
	return e
}

// localizeBody localizes v when it is an envelope, otherwise returns it
// unchanged. Split out from LocalizeMessage so it is testable with a plain
// context.Context.
func localizeBody(ctx context.Context, v any) any {
	if lz, ok := v.(localizer); ok {
		return lz.localize(ctx)
	}
	return v
}

// LocalizeMessage is a huma response Transformer that translates the envelope
// Message into the request locale (derived from ctx by the Locale middleware)
// for every response — success, business error, and built-in framework error.
// Register it via config.Transformers before huma.Register. Non-envelope
// responses pass through untouched.
func LocalizeMessage(ctx huma.Context, status string, v any) (any, error) {
	return localizeBody(ctx.Context(), v), nil
}
