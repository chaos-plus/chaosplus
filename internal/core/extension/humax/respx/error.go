package respx

import (
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// errorEnvelope is the error-response body. It mirrors Envelope but carries the
// structured huma error details in Data so field-level validation errors still
// reach the client. It implements huma.StatusError.
type errorEnvelope struct {
	status int

	Code    int           `json:"code" doc:"the HTTP status code (1..999); business codes are 100000+"`
	Message string        `json:"message" doc:"human-readable error summary"`
	Meta    Meta          `json:"meta" doc:"request metadata"`
	Data    []ErrorDetail `json:"data" doc:"per-field error details; null when none"`
}

// ErrorDetail is one structured error, mirroring huma.ErrorDetail.
type ErrorDetail struct {
	Message  string `json:"message" doc:"what went wrong"`
	Location string `json:"location,omitempty" doc:"where, e.g. 'path.count' or 'body.email'"`
	Value    any    `json:"value,omitempty" doc:"the offending value, echoed back"`
}

func (e *errorEnvelope) Error() string  { return e.Message }
func (e *errorEnvelope) GetStatus() int { return e.status }

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
			Message: msg,
			Meta:    Meta{RequestAt: time.Now()},
			Data:    toDetails(errs),
		}
	}
}

// toDetails converts huma's error details into ErrorDetail, preserving the
// location/value when present. Returns nil (JSON null) when there are none.
func toDetails(errs []error) []ErrorDetail {
	out := make([]ErrorDetail, 0, len(errs))
	for _, err := range errs {
		if err == nil {
			continue
		}
		d := ErrorDetail{Message: err.Error()}
		if de, ok := err.(huma.ErrorDetailer); ok {
			if ed := de.ErrorDetail(); ed != nil {
				d = ErrorDetail{Message: ed.Message, Location: ed.Location, Value: ed.Value}
			}
		}
		out = append(out, d)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
