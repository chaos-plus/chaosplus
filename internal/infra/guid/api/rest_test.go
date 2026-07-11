package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

func TestNextGUID_ReturnsStringEncodedID(t *testing.T) {
	g, err := guid.New(1)
	require.NoError(t, err)
	guid.SetDefault(g)

	_, api := humatest.New(t)
	RegisterREST(api)

	resp := api.Get("/guid")
	require.Equal(t, http.StatusOK, resp.Code)

	var body struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	// The id must cross the wire as a decimal string, not a bare JSON number.
	assert.Regexp(t, `^\d+$`, body.ID)
}
