package respx

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// errorEnvelope is the error-response body. It keeps the same shape as the
// success Envelope — {code, message, meta, data} — with Data always null on
// error. Message is "<key>" or "<key>: <detail>", where key is an i18n key (see
// statusKey) and detail is huma's English field-level reason; LocalizeMessage
// translates the key segment at serialize time and leaves detail intact. It
// implements huma.StatusError.
type errorEnvelope struct {
	status int

	Code    int    `json:"code" doc:"the HTTP status code (1..999); business codes are 100000+"`
	Message string `json:"message" doc:"human-readable error summary"`
	Meta    Meta   `json:"meta" doc:"request metadata"`
	Data    any    `json:"data" doc:"always null on error"`
}

// msgSep separates the summary key from the field detail, and detailSep joins
// multiple field details, inside errorEnvelope.Message. localize splits on them
// to translate the summary and each detail token independently.
const (
	msgSep    = ": "
	detailSep = "; "
)

func (e *errorEnvelope) Error() string  { return e.Message }
func (e *errorEnvelope) GetStatus() int { return e.status }

// Err returns a business error carried in the envelope: a business Code
// (100000+) and an i18n message key, with meta filled from ctx. Return it from a
// handler and huma renders it as the standard error envelope; LocalizeMessage
// resolves key to the request locale at serialize time (unknown keys pass
// through unchanged). The transport status is 400 so the response stays under the
// documented error schema; the specific failure is conveyed by Code.
func Err(ctx context.Context, code int, key string) error {
	return &errorEnvelope{
		status:  http.StatusBadRequest,
		Code:    code,
		Message: key,
		Meta:    metaOf(ctx, nil),
	}
}

// phraseKey normalizes huma's built-in English error summaries to i18n keys so
// framework-generated errors (e.g. request validation) localize like the rest.
// App code should pass i18n keys directly to huma.ErrorXxx / respx.Err rather
// than rely on this map; only huma's own fixed phrases need normalizing here.
var phraseKey = map[string]string{
	"validation failed": "validation_failed",
}

// Install replaces huma.NewError so every built-in error response (validation,
// 4xx, 5xx) is rendered as the uniform envelope. Call once at startup BEFORE any
// huma.Register: huma's defineErrors reflects NewError's return type, so calling
// Install first also makes the generated OpenAPI document the error envelope.
// Message is "<summary>" or "<summary>: <detail>", where summary and each detail
// token are i18n keys (huma's fixed phrases normalized via phraseKey); LocalizeMessage
// translates each at serialize time and passes unknown keys through. RequestAt is
// emitted in UTC; elapsed_ms is zero because huma builds errors without the
// request context that Timing populates. Code is the HTTP status (1..999);
// domain code mapping (100000+) can be layered on here later.
func Install() {
	huma.NewError = func(status int, msg string, errs ...error) huma.StatusError {
		summary := msg
		if key, ok := phraseKey[msg]; ok {
			summary = key
		}
		if detail := detailOf(errs); detail != "" {
			summary = summary + msgSep + detail
		}
		return &errorEnvelope{
			status:  status,
			Code:    status,
			Message: summary,
			Meta:    Meta{RequestAt: time.Now().UTC()},
			Data:    nil,
		}
	}
}

// detailOf joins the field-level reasons from huma's error details so the reason
// survives even though the payload (data) is null. Each reason is an i18n key
// when the app authored it (localized at serialize time) or huma's English text
// otherwise (passed through). The field location is intentionally omitted from
// the human message. Returns "" when there are no details.
func detailOf(errs []error) string {
	parts := make([]string, 0, len(errs))
	for _, err := range errs {
		if err == nil {
			continue
		}
		text := err.Error()
		if de, ok := err.(huma.ErrorDetailer); ok {
			if ed := de.ErrorDetail(); ed != nil && ed.Message != "" {
				text = ed.Message
			}
		}
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, detailSep)
}
