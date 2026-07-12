package respx

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOK_SuccessEnvelope(t *testing.T) {
	got := OK(context.Background(), "payload")

	assert.Equal(t, 0, got.Body.Code)
	assert.Equal(t, "ok", got.Body.Message)
	assert.Equal(t, "payload", got.Body.Data)
	assert.Nil(t, got.Body.Meta.Page, "non-list response carries no page meta")
	assert.False(t, got.Body.Meta.RequestAt.IsZero())
}

func TestList_CarriesPageMeta(t *testing.T) {
	page := Page{Offset: 0, Limit: 10, Count: 3, Total: 42}
	got := List(context.Background(), []int{1, 2, 3}, page)

	assert.Equal(t, 0, got.Body.Code)
	require.NotNil(t, got.Body.Meta.Page)
	assert.Equal(t, page, *got.Body.Meta.Page)
	assert.Len(t, got.Body.Data, 3)
}

func TestInstall_UnifiesErrorsIntoEnvelope(t *testing.T) {
	Install()

	err := huma.NewError(422, "validation failed", &huma.ErrorDetail{
		Message:  "expected integer",
		Location: "path.count",
		Value:    "abc",
	})
	require.Equal(t, 422, err.GetStatus())

	raw, mErr := json.Marshal(err)
	require.NoError(t, mErr)

	var env struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(raw, &env))
	assert.Equal(t, 422, env.Code)
	// The field-level detail is folded into message; data stays null.
	assert.Contains(t, env.Message, "validation failed")
	assert.Contains(t, env.Message, "path.count")
	assert.Contains(t, env.Message, "expected integer")
	assert.JSONEq(t, "null", string(env.Data), "data is null on error")
}

func TestErr_BusinessEnvelope(t *testing.T) {
	e := Err(context.Background(), 100001, "insufficient balance")

	se, ok := e.(huma.StatusError)
	require.True(t, ok)
	assert.Equal(t, 400, se.GetStatus())

	raw, err := json.Marshal(e)
	require.NoError(t, err)
	var env struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(raw, &env))
	assert.Equal(t, 100001, env.Code)
	assert.Equal(t, "insufficient balance", env.Message)
}
