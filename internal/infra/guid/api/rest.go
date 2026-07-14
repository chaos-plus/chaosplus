package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
)

// maxBatchGUIDs bounds a single GET /guid/{count} request. Keep the "maximum" tag
// on batchInput.Count in sync with this value (struct tags must be literals).
const maxBatchGUIDs = 10000

// batchInput is the request for GET /guid/{count}; huma rejects out-of-range
// counts with a 422 before the handler runs.
type batchInput struct {
	Count int `path:"count" minimum:"1" maximum:"10000" doc:"how many ids to generate"`
}

// RegisterREST mounts the guid HTTP endpoints, minting ids via next. Ids are
// string-encoded so they survive JavaScript's 2^53 safe-integer limit.
func RegisterREST(a huma.API, next NextFunc) {
	huma.Register(a, huma.Operation{
		OperationID: "next-guid",
		Method:      http.MethodGet,
		Path:        "/guid",
		Summary:     "Generate a globally-unique id",
		Description: "Returns a new Sonyflake id, string-encoded so it survives " +
			"JavaScript's 2^53 safe-integer limit without losing precision.",
		Tags: []string{"guid"},
	}, func(ctx context.Context, _ *struct{}) (*respx.Body[string], error) {
		id, err := next()
		if err != nil {
			return nil, huma.Error503ServiceUnavailable("guid_not_ready", err)
		}
		return respx.OK(ctx, strconv.FormatInt(id, 10)), nil
	})

	huma.Register(a, huma.Operation{
		OperationID: "next-guid-batch",
		Method:      http.MethodGet,
		Path:        "/guid/{count}",
		Summary:     "Generate a batch of globally-unique ids",
		Description: fmt.Sprintf("Returns `count` new Sonyflake ids (1..%d), each "+
			"string-encoded like GET /guid.", maxBatchGUIDs),
		Tags: []string{"guid"},
	}, func(ctx context.Context, in *batchInput) (*respx.Body[[]string], error) {
		ids := make([]string, 0, in.Count)
		for range in.Count {
			id, err := next()
			if err != nil {
				return nil, huma.Error503ServiceUnavailable("guid_not_ready", err)
			}
			ids = append(ids, strconv.FormatInt(id, 10))
		}
		return respx.OK(ctx, ids), nil
	})
}
