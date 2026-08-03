package respx

import (
	"context"
	"fmt"
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

// localize translates the summary and client-safe validation details, including
// their field locations, into the request locale.
func (e *errorEnvelope) localize(ctx context.Context) any {
	summary := e.summary
	if summary == "" {
		summary = e.Message
	}
	msg := localizeToken(ctx, summary)
	if len(e.details) > 0 {
		parts := make([]string, 0, len(e.details))
		for _, detail := range e.details {
			localized := localizeToken(ctx, detail.message)
			if localized == detail.message {
				localized = i18n.TContext(ctx, "validation_invalid_value")
			}
			if detail.location != "" {
				localized = i18n.TContext(ctx, "validation_field", detail.location, localized)
			}
			parts = append(parts, localized)
		}
		msg += msgSep + strings.Join(parts, detailSep)
	}
	e.Message = msg
	return e
}

func localizeToken(ctx context.Context, token string) string {
	if key, ok := frameworkDetailKey[token]; ok {
		token = key
	} else if format, ok := strings.CutPrefix(token, "invalid date/time for format "); ok {
		token = validationMessage("validation_expected_datetime_format", format)
	} else if strings.HasPrefix(token, "invalid value:") {
		token = "validation_invalid_value"
	}
	parts := strings.Split(token, argSep)
	message := i18n.TContext(ctx, parts[0])
	if len(parts) == 1 || !strings.Contains(message, "%") {
		return message
	}
	args := make([]any, len(parts)-1)
	for index := range args {
		args[index] = parts[index+1]
	}
	return fmt.Sprintf(message, args...)
}

var frameworkDetailKey = map[string]string{
	"invalid integer":        "validation_expected_integer",
	"invalid float":          "validation_expected_number",
	"invalid floating value": "validation_expected_number",
	"invalid boolean":        "validation_expected_boolean",
	"invalid url.URL value":  "validation_expected_uri",
	"unparsable value":       "validation_invalid_value",
	"unsupported type":       "validation_invalid_value",
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
