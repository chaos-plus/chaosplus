package guid

import (
	"context"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// registerREST mounts the guid HTTP endpoints on the given huma API. It is
// wired by Module.RegisterREST once the process-wide generator is installed.
func registerREST(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "next-guid",
		Method:      http.MethodGet,
		Path:        "/guid",
		Summary:     "Generate a globally-unique id",
		Description: "Returns a new Sonyflake id. It is string-encoded so it survives " +
			"JavaScript's 2^53 safe-integer limit without losing precision.",
		Tags: []string{"guid"},
	}, nextGUID)

	huma.Register(api, huma.Operation{
		OperationID: "next-guid-batch",
		Method:      http.MethodGet,
		Path:        "/guid/{count}",
		Summary:     "Generate a batch of globally-unique ids",
		Description: fmt.Sprintf("Returns `count` new Sonyflake ids (1..%d), each "+
			"string-encoded like GET /guid. The upper bound keeps a single request bounded.", maxBatchGUIDs),
		Tags: []string{"guid"},
	}, nextGUIDBatch)
}

// NextOutput is the response body for GET /guid.
type NextOutput struct {
	Body struct {
		ID ID `json:"id" doc:"the generated snowflake id (decimal string)"`
	}
}

// nextGUID returns a single new id from the process-wide generator.
func nextGUID(_ context.Context, _ *struct{}) (*NextOutput, error) {
	id, err := Next()
	if err != nil {
		// The generator is installed at startup once a worker id is leased; if it
		// is missing, the service isn't ready to mint ids yet.
		return nil, huma.Error503ServiceUnavailable("guid generator not ready", err)
	}
	out := &NextOutput{}
	out.Body.ID = ID(id)
	return out, nil
}

// maxBatchGUIDs bounds a single GET /guid/{count} request. Keep the "maximum"
// tag on BatchInput.Count in sync with this value (struct tags must be literals).
const maxBatchGUIDs = 10000

// BatchInput is the request for GET /guid/{count}. huma validates count against
// the schema bounds below and rejects out-of-range values with a 422 before the
// handler runs, so nextGUIDBatch can trust 1 <= Count <= maxBatchGUIDs.
type BatchInput struct {
	Count int `path:"count" minimum:"1" maximum:"10000" doc:"how many ids to generate"`
}

// BatchOutput is the response body for GET /guid/{count}.
type BatchOutput struct {
	Body struct {
		IDs []ID `json:"ids" doc:"the generated snowflake ids, in ascending order"`
	}
}

// nextGUIDBatch returns Count new ids from the process-wide generator.
func nextGUIDBatch(_ context.Context, in *BatchInput) (*BatchOutput, error) {
	out := &BatchOutput{}
	out.Body.IDs = make([]ID, 0, in.Count)
	for range in.Count {
		id, err := Next()
		if err != nil {
			return nil, huma.Error503ServiceUnavailable("guid generator not ready", err)
		}
		out.Body.IDs = append(out.Body.IDs, ID(id))
	}
	return out, nil
}
