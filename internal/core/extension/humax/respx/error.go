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
// error; the failure reason (including any field-level detail) is folded into
// Message. It implements huma.StatusError.
type errorEnvelope struct {
	status int

	Code    int    `json:"code" doc:"the HTTP status code (1..999); business codes are 100000+"`
	Message string `json:"message" doc:"human-readable error summary"`
	Meta    Meta   `json:"meta" doc:"request metadata"`
	Data    any    `json:"data" doc:"always null on error"`
}

func (e *errorEnvelope) Error() string  { return e.Message }
func (e *errorEnvelope) GetStatus() int { return e.status }

// Err returns a business error carried in the envelope: a business Code
// (100000+) and message, with meta filled from ctx. Return it from a handler and
// huma renders it as the standard error envelope. The transport status is 400 so
// the response stays under the documented error schema; the specific failure is
// conveyed by Code.
func Err(ctx context.Context, code int, message string) error {
	return &errorEnvelope{
		status:  http.StatusBadRequest,
		Code:    code,
		Message: message,
		Meta:    metaOf(ctx, nil),
	}
}

// Install replaces huma.NewError so every built-in error response (validation,
// 4xx, 5xx) is rendered as the uniform envelope. Call once at startup BEFORE any
// huma.Register: huma's defineErrors reflects NewError's return type, so calling
// Install first also makes the generated OpenAPI document the error envelope.
// Error responses carry request_at but leave elapsed_ms zero: huma builds errors
// without the request context that Timing populates. Code is the HTTP status
// (1..999); domain code mapping (100000+) can be layered on here later.
func Install() {
	huma.NewError = func(status int, msg string, errs ...error) huma.StatusError {
		return &errorEnvelope{
			status:  status,
			Code:    status,
			Message: messageOf(msg, errs),
			Meta:    Meta{RequestAt: time.Now()},
			Data:    nil,
		}
	}
}

// messageOf folds huma's field-level details into the summary so the reason
// survives even though the payload (data) is null. "validation failed" becomes
// "validation failed: path.ip not a valid IPv4 address (expected x.x.x.x)".
func messageOf(msg string, errs []error) string {
	parts := make([]string, 0, len(errs))
	for _, err := range errs {
		if err == nil {
			continue
		}
		text := err.Error()
		if de, ok := err.(huma.ErrorDetailer); ok {
			if ed := de.ErrorDetail(); ed != nil {
				text = ed.Message
				if ed.Location != "" {
					text = ed.Location + " " + ed.Message
				}
			}
		}
		if text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		return msg
	}
	return msg + ": " + strings.Join(parts, "; ")
}
