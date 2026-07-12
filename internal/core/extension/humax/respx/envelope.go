// Package respx provides a uniform HTTP response envelope for huma APIs: every
// response — success or error — is shaped as {code, message, meta, data}.
// Success bodies are built with OK/List; error responses are unified by Install,
// which replaces huma.NewError so built-in errors (validation, 4xx, 5xx) render
// the same envelope.
package respx

import "time"

// Envelope is the uniform response body. Data carries the payload and is null on
// error. Code partitions the status space:
//
//	0                  success
//	1..999             HTTP status code (set on error responses)
//	1000..99999        reserved
//	100000..99999999   business codes, grouped per domain (assigned later)
type Envelope[T any] struct {
	Code    int    `json:"code" doc:"0 = success; 1..999 = HTTP status; 100000+ = business code"`
	Message string `json:"message" doc:"human-readable status; \"ok\" on success"`
	Meta    Meta   `json:"meta" doc:"request metadata"`
	Data    T      `json:"data" doc:"response payload; null on error"`
}

// Meta carries per-request metadata. Page is set only for list responses.
type Meta struct {
	RequestAt time.Time `json:"request_at" doc:"when the server received the request"`
	ElapsedMS int64     `json:"elapsed_ms" doc:"server processing time in milliseconds"`
	Page      *Page     `json:"page,omitempty" doc:"pagination; present on list responses only"`
}

// Page describes a slice of a larger collection.
type Page struct {
	Offset int   `json:"offset" doc:"items skipped before this page"`
	Limit  int   `json:"limit" doc:"maximum items requested"`
	Count  int   `json:"count" doc:"items returned in this response"`
	Total  int64 `json:"total" doc:"total items available across all pages"`
}

// Body is the huma output wrapper. Use it as an operation's output type so the
// generated OpenAPI schema reflects the real envelope:
//
//	func handler(ctx context.Context, in *In) (*respx.Body[Thing], error)
type Body[T any] struct {
	Body Envelope[T]
}
