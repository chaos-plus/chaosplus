package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// counterNext returns a NextFunc yielding 1,2,3,... — enough to exercise the
// transport without the real generator.
func counterNext() NextFunc {
	var n int64
	return func() (int64, error) { return atomic.AddInt64(&n, 1), nil }
}

func failingNext() NextFunc {
	return func() (int64, error) { return 0, errors.New("not ready") }
}

func TestRegisterREST_SingleStringEncoded(t *testing.T) {
	_, a := humatest.New(t)
	RegisterREST(a, counterNext())

	resp := a.Get("/guid")
	require.Equal(t, http.StatusOK, resp.Code)
	var body struct {
		Code int    `json:"code"`
		Data string `json:"data"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Equal(t, 0, body.Code)
	assert.Regexp(t, `^\d+$`, body.Data, "id crosses the wire as a decimal string")
}

func TestRegisterREST_BatchDistinct(t *testing.T) {
	_, a := humatest.New(t)
	RegisterREST(a, counterNext())

	resp := a.Get("/guid/5")
	require.Equal(t, http.StatusOK, resp.Code)
	var body struct {
		Data []string `json:"data"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	require.Len(t, body.Data, 5)
	seen := map[string]struct{}{}
	for _, id := range body.Data {
		assert.Regexp(t, `^\d+$`, id)
		_, dup := seen[id]
		assert.False(t, dup, "batch ids must be unique")
		seen[id] = struct{}{}
	}
}

func TestRegisterREST_BatchRejectsOutOfRange(t *testing.T) {
	_, a := humatest.New(t)
	RegisterREST(a, counterNext())
	assert.Equal(t, http.StatusUnprocessableEntity, a.Get("/guid/0").Code)
	assert.Equal(t, http.StatusUnprocessableEntity, a.Get("/guid/10001").Code)
}

func TestRegisterREST_NotReady(t *testing.T) {
	_, a := humatest.New(t)
	RegisterREST(a, failingNext())
	assert.Equal(t, http.StatusServiceUnavailable, a.Get("/guid").Code)
	assert.Equal(t, http.StatusServiceUnavailable, a.Get("/guid/3").Code)
}
