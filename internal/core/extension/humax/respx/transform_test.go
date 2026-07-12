package respx

import (
	"context"
	"testing"

	"github.com/chaos-plus/chaosplus/pkg/i18n"
	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// initI18n loads the global locale bundle so TContext can resolve keys. It is
// idempotent, so tests may call it freely.
func initI18n(t *testing.T) {
	t.Helper()
	require.NoError(t, i18n.InitEmbedded(i18n.Base))
}

func localeCtx(locale string) context.Context {
	return i18n.WithLocale(context.Background(), locale)
}

func TestEnvelopeLocalize(t *testing.T) {
	initI18n(t)

	t.Run("translates key to zh-CN", func(t *testing.T) {
		env := Envelope[string]{Message: "success", Data: "x"}
		got := env.localize(localeCtx("zh-CN")).(Envelope[string])
		assert.Equal(t, "成功", got.Message)
		assert.Equal(t, "x", got.Data)
	})

	t.Run("translates key to en-US", func(t *testing.T) {
		env := Envelope[string]{Message: "success"}
		got := env.localize(localeCtx("en-US")).(Envelope[string])
		assert.Equal(t, "Success", got.Message)
	})

	t.Run("unknown key passes through", func(t *testing.T) {
		env := Envelope[string]{Message: "not_a_key"}
		got := env.localize(localeCtx("zh-CN")).(Envelope[string])
		assert.Equal(t, "not_a_key", got.Message)
	})

	t.Run("does not mutate the source (value receiver)", func(t *testing.T) {
		env := Envelope[string]{Message: "success"}
		_ = env.localize(localeCtx("zh-CN"))
		assert.Equal(t, "success", env.Message, "caller's envelope is untouched")
	})
}

func TestErrorEnvelopeLocalize(t *testing.T) {
	initI18n(t)
	Install()

	t.Run("translates summary and app-authored detail key", func(t *testing.T) {
		e := huma.NewError(422, "validation failed",
			&huma.ErrorDetail{Message: "invalid_ipv4", Location: "path.ip"}).(*errorEnvelope)
		got := e.localize(localeCtx("zh-CN")).(*errorEnvelope)
		assert.Equal(t, "验证失败: 不是合法的 IPv4 地址（应为 x.x.x.x）", got.Message)
	})

	t.Run("unknown detail token (framework English) passes through", func(t *testing.T) {
		e := huma.NewError(422, "validation failed",
			&huma.ErrorDetail{Message: "expected integer", Location: "path.count"}).(*errorEnvelope)
		got := e.localize(localeCtx("zh-CN")).(*errorEnvelope)
		assert.Equal(t, "验证失败: expected integer", got.Message)
	})

	t.Run("app summary key without detail", func(t *testing.T) {
		e := huma.NewError(404, "geoip_not_found").(*errorEnvelope)
		got := e.localize(localeCtx("en-US")).(*errorEnvelope)
		assert.Equal(t, "No geolocation found for this IP", got.Message)
	})

	t.Run("idempotent — second pass is a no-op", func(t *testing.T) {
		e := huma.NewError(422, "validation failed",
			&huma.ErrorDetail{Message: "invalid_ipv4", Location: "path.ip"}).(*errorEnvelope)
		once := e.localize(localeCtx("en-US")).(*errorEnvelope).Message
		twice := e.localize(localeCtx("en-US")).(*errorEnvelope).Message
		assert.Equal(t, "Validation failed: Not a valid IPv4 address (expected x.x.x.x)", once)
		assert.Equal(t, once, twice)
	})
}

func TestLocalizeBody(t *testing.T) {
	initI18n(t)

	t.Run("localizes an envelope", func(t *testing.T) {
		got := localizeBody(localeCtx("zh-CN"), Envelope[int]{Message: "success", Data: 1})
		assert.Equal(t, "成功", got.(Envelope[int]).Message)
	})

	t.Run("passes non-envelope values through", func(t *testing.T) {
		assert.Equal(t, "plain", localizeBody(localeCtx("zh-CN"), "plain"))
	})
}
