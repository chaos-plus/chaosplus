package respx

import (
	"context"
	"fmt"
	"testing"

	"github.com/chaos-plus/chaosplus/pkg/i18n"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/validation"
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
		assert.Equal(t, "请求数据校验失败，请修正以下字段: 字段 path.ip：不是合法的 IPv4 地址（应为 x.x.x.x）", got.Message)
	})

	t.Run("unknown detail never leaks untranslated text", func(t *testing.T) {
		e := huma.NewError(422, "validation failed",
			&huma.ErrorDetail{Message: "implementation detail", Location: "path.count"}).(*errorEnvelope)
		got := e.localize(localeCtx("zh-CN")).(*errorEnvelope)
		assert.Equal(t, "请求数据校验失败，请修正以下字段: 字段 path.count：该值无效。", got.Message)
	})

	t.Run("app summary key without detail", func(t *testing.T) {
		e := huma.NewError(404, "geoip_not_found").(*errorEnvelope)
		got := e.localize(localeCtx("en-US")).(*errorEnvelope)
		assert.Equal(t, "No geolocation data was found for this IP address. Verify the address or try another provider.", got.Message)
	})

	t.Run("idempotent — second pass is a no-op", func(t *testing.T) {
		e := huma.NewError(422, "validation failed",
			&huma.ErrorDetail{Message: "invalid_ipv4", Location: "path.ip"}).(*errorEnvelope)
		once := e.localize(localeCtx("en-US")).(*errorEnvelope).Message
		twice := e.localize(localeCtx("en-US")).(*errorEnvelope).Message
		assert.Equal(t, "Request data validation failed; correct the following fields: Field path.ip: Not a valid IPv4 address (expected x.x.x.x)", once)
		assert.Equal(t, once, twice)
	})

	t.Run("drops arbitrary internal errors", func(t *testing.T) {
		e := huma.NewError(503, "authorization_unavailable", assert.AnError).(*errorEnvelope)
		got := e.localize(localeCtx("ms-MY")).(*errorEnvelope)
		assert.Equal(t, "Kebenaran tidak dapat dinilai dengan selamat. Cuba lagi kemudian; hubungi pentadbir jika masalah berterusan.", got.Message)
		assert.NotContains(t, got.Message, assert.AnError.Error())
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

func TestEveryFrameworkValidationMessageLocalizes(t *testing.T) {
	initI18n(t)
	Install()
	tokens := []string{
		validation.MsgUnexpectedProperty,
		validation.MsgExpectedRFC3339DateTime, validation.MsgExpectedRFC1123DateTime,
		validation.MsgExpectedRFC3339Date, validation.MsgExpectedRFC3339Time,
		fmt.Sprintf(validation.MsgExpectedRFC5322Email, "detail"),
		validation.MsgExpectedRFC5890Hostname, validation.MsgExpectedRFC2673IPv4,
		validation.MsgExpectedRFC2373IPv6, validation.MsgExpectedRFCIPAddr,
		fmt.Sprintf(validation.MsgExpectedRFC3986URI, "detail"),
		fmt.Sprintf(validation.MsgExpectedRFC4122UUID, "detail"),
		validation.MsgExpectedRFC6570URITemplate, validation.MsgExpectedRFC6901JSONPointer,
		validation.MsgExpectedRFC6901RelativeJSONPointer,
		fmt.Sprintf(validation.MsgExpectedRegexp, "detail"),
		validation.MsgExpectedMatchAtLeastOneSchema, validation.MsgExpectedMatchExactlyOneSchema,
		validation.MsgExpectedNotMatchSchema, validation.MsgExpectedPropertyNameInObject,
		validation.MsgExpectedBoolean, fmt.Sprintf(validation.MsgExpectedDuration, "detail"),
		validation.MsgExpectedNumber, validation.MsgExpectedInteger, validation.MsgExpectedString,
		validation.MsgExpectedBase64String, validation.MsgExpectedArray, validation.MsgExpectedObject,
		validation.MsgExpectedArrayItemsUnique, fmt.Sprintf(validation.MsgExpectedOneOf, "a, b"),
		fmt.Sprintf(validation.MsgExpectedMinimumNumber, 1),
		fmt.Sprintf(validation.MsgExpectedExclusiveMinimumNumber, 1),
		fmt.Sprintf(validation.MsgExpectedMaximumNumber, 2),
		fmt.Sprintf(validation.MsgExpectedExclusiveMaximumNumber, 2),
		fmt.Sprintf(validation.MsgExpectedNumberBeMultipleOf, 2),
		fmt.Sprintf(validation.MsgExpectedMinLength, 1), fmt.Sprintf(validation.MsgExpectedMaxLength, 2),
		fmt.Sprintf(validation.MsgExpectedBePattern, "slug"),
		fmt.Sprintf(validation.MsgExpectedMatchPattern, "^[a-z]+$"),
		fmt.Sprintf(validation.MsgExpectedMinItems, 1), fmt.Sprintf(validation.MsgExpectedMaxItems, 2),
		fmt.Sprintf(validation.MsgExpectedMinProperties, 1), fmt.Sprintf(validation.MsgExpectedMaxProperties, 2),
		fmt.Sprintf(validation.MsgExpectedRequiredProperty, "name"),
		fmt.Sprintf(validation.MsgExpectedDependentRequiredProperty, "child", "parent"),
	}
	for _, locale := range i18n.Supported() {
		for _, token := range tokens {
			message := localizeToken(localeCtx(locale.Code), token)
			assert.NotContains(t, message, "validation_", "%s: %q", locale.Code, token)
			assert.NotContains(t, message, "%!", "%s: %q", locale.Code, token)
		}
	}
}
