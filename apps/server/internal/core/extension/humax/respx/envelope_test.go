package respx

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countInput struct {
	Count int `path:"count"`
}

// ipInput mirrors an app validator (like geoip) that returns an i18n key as its
// field-detail message, so the whole error message localizes.
type ipInput struct {
	IP string `path:"ip"`
}

func (i *ipInput) Resolve(_ huma.Context) []error {
	if i.IP != "ok" {
		return []error{&huma.ErrorDetail{Message: "invalid_ipv4", Location: "path.ip", Value: i.IP}}
	}
	return nil
}

// newTestAPI builds a chi router wired exactly like the real server: Timing +
// Locale middleware, the LocalizeMessage transformer, and Install for unified
// errors. It registers a success, a business-error, and a validation route.
func newTestAPI(t *testing.T) http.Handler {
	t.Helper()
	initI18n(t)

	router := chi.NewMux()
	router.Use(Timing)
	router.Use(Locale)

	config := huma.DefaultConfig("test", "1.0.0")
	config.Transformers = append(config.Transformers, LocalizeMessage)
	Install()
	api := humachi.New(router, config)

	huma.Get(api, "/ping", func(ctx context.Context, _ *struct{}) (*Body[string], error) {
		return OK(ctx, "pong"), nil
	})
	huma.Get(api, "/boom", func(ctx context.Context, _ *struct{}) (*Body[string], error) {
		return nil, Err(ctx, 100001, "user_not_found")
	})
	huma.Get(api, "/n/{count}", func(ctx context.Context, in *countInput) (*Body[int], error) {
		return OK(ctx, in.Count), nil
	})
	huma.Get(api, "/ip/{ip}", func(ctx context.Context, in *ipInput) (*Body[string], error) {
		return OK(ctx, in.IP), nil
	})
	return router
}

type envelopeResp struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
	Meta    struct {
		RequestAt string `json:"request_at"`
	} `json:"meta"`
}

func do(t *testing.T, h http.Handler, target string, headers map[string]string) (int, envelopeResp) {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+target, nil)
	require.NoError(t, err)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	response, err := server.Client().Do(req)
	require.NoError(t, err)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	var env envelopeResp
	require.NoError(t, json.Unmarshal(body, &env), "body: %s", body)
	return response.StatusCode, env
}

func TestIntegration_SuccessLocalized(t *testing.T) {
	h := newTestAPI(t)

	t.Run("zh-CN via Accept-Language", func(t *testing.T) {
		status, env := do(t, h, "/ping", map[string]string{"Accept-Language": "zh-CN"})
		assert.Equal(t, http.StatusOK, status)
		assert.Equal(t, 0, env.Code)
		assert.Equal(t, "成功", env.Message)
		assert.JSONEq(t, `"pong"`, string(env.Data))
	})

	t.Run("en-US default", func(t *testing.T) {
		_, env := do(t, h, "/ping", nil)
		assert.Equal(t, "Success", env.Message)
	})

	t.Run("request_at serialized as UTC (Z)", func(t *testing.T) {
		_, env := do(t, h, "/ping", nil)
		assert.True(t, strings.HasSuffix(env.Meta.RequestAt, "Z"),
			"timestamp is UTC RFC3339: %s", env.Meta.RequestAt)
		ts, err := time.Parse(time.RFC3339, env.Meta.RequestAt)
		require.NoError(t, err)
		assert.Equal(t, time.UTC, ts.UTC().Location())
	})

	t.Run("?lang override beats Accept-Language", func(t *testing.T) {
		_, env := do(t, h, "/ping?lang=zh-CN", map[string]string{"Accept-Language": "en-US"})
		assert.Equal(t, "成功", env.Message)
	})
}

func TestIntegration_BusinessErrorLocalized(t *testing.T) {
	h := newTestAPI(t)

	status, env := do(t, h, "/boom", map[string]string{"Accept-Language": "zh-CN"})
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, 100001, env.Code)
	assert.Equal(t, "未找到指定用户，请核对用户标识符和租户后重试。", env.Message)
	assert.JSONEq(t, "null", string(env.Data))
}

func TestIntegration_ValidationErrorLocalized(t *testing.T) {
	h := newTestAPI(t)

	status, env := do(t, h, "/n/abc", map[string]string{"Accept-Language": "zh-CN"})
	assert.Equal(t, http.StatusUnprocessableEntity, status)
	assert.Equal(t, 422, env.Code)
	assert.Equal(t, "请求数据校验失败，请修正以下字段: 字段 path.count：应为整数。", env.Message)
	assert.NotContains(t, env.Message, "invalid integer")

	_, ms := do(t, h, "/n/abc", map[string]string{"Accept-Language": "ms-MY"})
	assert.Equal(t, "Pengesahan data permintaan gagal; betulkan medan berikut: Medan path.count: Integer diperlukan.", ms.Message)
}

// TestIntegration_AppValidationFullyLocalized reproduces the geoip scenario: an
// app validator returning an i18n key as its detail — the whole message localizes.
func TestIntegration_AppValidationFullyLocalized(t *testing.T) {
	h := newTestAPI(t)

	status, env := do(t, h, "/ip/bad?lang=zh-CN", nil)
	assert.Equal(t, http.StatusUnprocessableEntity, status)
	assert.Equal(t, 422, env.Code)
	assert.Equal(t, "请求数据校验失败，请修正以下字段: 字段 path.ip：不是合法的 IPv4 地址（应为 x.x.x.x）", env.Message,
		"no English remains in the message")

	_, en := do(t, h, "/ip/bad", nil)
	assert.Equal(t, "Request data validation failed; correct the following fields: Field path.ip: Not a valid IPv4 address (expected x.x.x.x)", en.Message)
}
