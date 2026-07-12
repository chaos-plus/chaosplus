package respx

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOK_SuccessEnvelope(t *testing.T) {
	got := OK(context.Background(), "payload")

	assert.Equal(t, 0, got.Body.Code)
	assert.Equal(t, "success", got.Body.Message, "message carries the i18n key")
	assert.Equal(t, "payload", got.Body.Data)
	assert.Nil(t, got.Body.Meta.Page, "non-list response carries no page meta")
	assert.False(t, got.Body.Meta.RequestAt.IsZero())
	assert.Equal(t, time.UTC, got.Body.Meta.RequestAt.Location(), "RequestAt is UTC")
}

func TestList_CarriesPageMeta(t *testing.T) {
	page := Page{Offset: 0, Limit: 10, Count: 3, Total: 42}
	got := List(context.Background(), []int{1, 2, 3}, page)

	assert.Equal(t, 0, got.Body.Code)
	assert.Equal(t, "success", got.Body.Message)
	require.NotNil(t, got.Body.Meta.Page)
	assert.Equal(t, page, *got.Body.Meta.Page)
	assert.Len(t, got.Body.Data, 3)
	assert.Equal(t, time.UTC, got.Body.Meta.RequestAt.Location(), "RequestAt is UTC")
}

func TestInstall_NormalizesSummaryAndKeepsDetailKey(t *testing.T) {
	Install()

	// huma's built-in "validation failed" summary is normalized to its i18n key;
	// the app-authored detail is an i18n key; the field location is dropped.
	err := huma.NewError(422, "validation failed", &huma.ErrorDetail{
		Message:  "invalid_ipv4",
		Location: "path.ip",
		Value:    "abc",
	})
	require.Equal(t, 422, err.GetStatus())

	ee, ok := err.(*errorEnvelope)
	require.True(t, ok)
	assert.Equal(t, 422, ee.Code)
	assert.Equal(t, "validation_failed: invalid_ipv4", ee.Message,
		"message is '<summary key>: <detail key>' before localization, location dropped")
	assert.Equal(t, time.UTC, ee.Meta.RequestAt.Location(), "RequestAt is UTC")

	// Raw serialization keeps the {code,message,meta,data} shape; data is null.
	raw, mErr := json.Marshal(err)
	require.NoError(t, mErr)
	var env struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(raw, &env))
	assert.Equal(t, 422, env.Code)
	assert.Equal(t, "validation_failed: invalid_ipv4", env.Message)
	assert.JSONEq(t, "null", string(env.Data), "data is null on error")
}

func TestInstall_AppSummaryPassesThroughUnchanged(t *testing.T) {
	Install()

	// A non-huma summary (e.g. an app i18n key via huma.Error404NotFound) is not
	// clobbered by any status mapping — it is carried verbatim for localization.
	err := huma.NewError(404, "geoip_not_found")
	ee, ok := err.(*errorEnvelope)
	require.True(t, ok)
	assert.Equal(t, 404, ee.Code)
	assert.Equal(t, "geoip_not_found", ee.Message)
}

func TestErr_BusinessEnvelope(t *testing.T) {
	e := Err(context.Background(), 100001, "insufficient_balance")

	se, ok := e.(huma.StatusError)
	require.True(t, ok)
	assert.Equal(t, 400, se.GetStatus())

	ee, ok := e.(*errorEnvelope)
	require.True(t, ok)
	assert.Equal(t, 100001, ee.Code)
	assert.Equal(t, "insufficient_balance", ee.Message, "message carries the i18n key")
	assert.Equal(t, "insufficient_balance", ee.Error(), "Error() exposes the message")
	assert.Equal(t, time.UTC, ee.Meta.RequestAt.Location(), "RequestAt is UTC")
}

func TestDetailOf(t *testing.T) {
	t.Run("joins multiple details without location", func(t *testing.T) {
		got := detailOf([]error{
			&huma.ErrorDetail{Message: "expected integer", Location: "path.count"},
			&huma.ErrorDetail{Message: "required"},
		})
		assert.Equal(t, "expected integer; required", got)
	})

	t.Run("skips nil errors and empty text", func(t *testing.T) {
		assert.Empty(t, detailOf([]error{nil}))
		assert.Empty(t, detailOf(nil))
	})
}
