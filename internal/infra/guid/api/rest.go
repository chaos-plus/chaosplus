// Package api exposes the guid generator over transport-layer APIs.
package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

// RegisterREST mounts the guid HTTP endpoints on the given huma API.
func RegisterREST(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "next-guid",
		Method:      http.MethodGet,
		Path:        "/guid",
		Summary:     "Generate a globally-unique id",
		Description: "Returns a new Sonyflake id. It is string-encoded so it survives " +
			"JavaScript's 2^53 safe-integer limit without losing precision.",
		Tags: []string{"guid"},
	}, nextGUID)
}

// NextOutput is the response body for GET /guid.
type NextOutput struct {
	Body struct {
		ID guid.ID `json:"id" doc:"the generated snowflake id (decimal string)"`
	}
}

// nextGUID returns a single new id from the process-wide generator.
func nextGUID(_ context.Context, _ *struct{}) (*NextOutput, error) {
	id, err := guid.Next()
	if err != nil {
		// The generator is installed at startup once a worker id is leased; if it
		// is missing, the service isn't ready to mint ids yet.
		return nil, huma.Error503ServiceUnavailable("guid generator not ready", err)
	}
	out := &NextOutput{}
	out.Body.ID = guid.ID(id)
	return out, nil
}
