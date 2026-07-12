package guid

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNextGUID_ReturnsStringEncodedID(t *testing.T) {
	g, err := New(1)
	require.NoError(t, err)
	SetDefault(g)

	_, api := humatest.New(t)
	registerREST(api)

	resp := api.Get("/guid")
	require.Equal(t, http.StatusOK, resp.Code)

	var body struct {
		Code int    `json:"code"`
		Data string `json:"data"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Equal(t, 0, body.Code)
	// The id must cross the wire as a decimal string, not a bare JSON number.
	assert.Regexp(t, `^\d+$`, body.Data)
}

func TestNextGUIDBatch_ReturnsDistinctStringEncodedIDs(t *testing.T) {
	g, err := New(2)
	require.NoError(t, err)
	SetDefault(g)

	_, api := humatest.New(t)
	registerREST(api)

	resp := api.Get("/guid/5")
	require.Equal(t, http.StatusOK, resp.Code)

	var body struct {
		Code int      `json:"code"`
		Data []string `json:"data"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Equal(t, 0, body.Code)
	require.Len(t, body.Data, 5)

	seen := make(map[string]struct{}, len(body.Data))
	for _, id := range body.Data {
		assert.Regexp(t, `^\d+$`, id) // decimal string, not a bare JSON number
		_, dup := seen[id]
		assert.False(t, dup, "batch ids must be unique")
		seen[id] = struct{}{}
	}
}

func TestNextGUIDBatch_RejectsOutOfRangeCount(t *testing.T) {
	g, err := New(3)
	require.NoError(t, err)
	SetDefault(g)

	_, api := humatest.New(t)
	registerREST(api)

	// huma validates the path param against the schema bounds and rejects these
	// before the handler runs.
	assert.Equal(t, http.StatusUnprocessableEntity, api.Get("/guid/0").Code)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Get("/guid/10001").Code)
}
