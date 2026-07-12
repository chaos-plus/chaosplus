package i18n

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseField(t *testing.T) {
	assert.Nil(t, ParseField(""))
	assert.Nil(t, ParseField("{}"))
	assert.Nil(t, ParseField("null"))
	assert.Nil(t, ParseField("{not json"))
	assert.Equal(t,
		map[string]string{"en-US": "Hi", "zh-CN": "你好"},
		ParseField(`{"en-US":"Hi","zh-CN":"你好"}`),
	)
}

func TestMarshalField(t *testing.T) {
	assert.Equal(t, "", MarshalField(nil))
	assert.Equal(t, "", MarshalField(map[string]string{}))

	// round-trips through ParseField.
	m := map[string]string{"en-US": "Hi", "ms-MY": "Hai"}
	assert.Equal(t, m, ParseField(MarshalField(m)))
}

func TestBaseValue(t *testing.T) {
	assert.Equal(t, "Hi", BaseValue(map[string]string{"en-US": "Hi", "zh-CN": "你好"}))
	assert.Equal(t, "", BaseValue(map[string]string{"zh-CN": "你好"})) // no baseline
	assert.Equal(t, "", BaseValue(nil))
}

func TestResolveFieldCode(t *testing.T) {
	m := map[string]string{"en-US": "Color", "zh-CN": "颜色"}
	assert.Equal(t, "颜色", ResolveFieldCode(m, "zh-CN"))                              // hit
	assert.Equal(t, "Color", ResolveFieldCode(m, "ms-MY"))                           // miss → Base
	assert.Equal(t, "Color", ResolveFieldCode(m, ""))                                // empty → Base
	assert.Equal(t, "", ResolveFieldCode(nil, "zh-CN"))                              // empty map → ""
	assert.Equal(t, "", ResolveFieldCode(map[string]string{"zh-CN": "颜色"}, "ms-MY")) // miss, no Base → ""
}

func TestResolveField_Context(t *testing.T) {
	m := map[string]string{"en-US": "Color", "zh-CN": "颜色"}
	ctx := func(loc string) context.Context { return WithLocale(context.Background(), loc) }

	assert.Equal(t, "颜色", ResolveField(m, ctx("zh-CN")))            // canonical hit
	assert.Equal(t, "颜色", ResolveField(m, ctx("zh")))               // short → canonicalized → hit
	assert.Equal(t, "Color", ResolveField(m, ctx("ms")))            // supported but absent → Base
	assert.Equal(t, "Color", ResolveField(m, ctx("ja")))            // unsupported → Base
	assert.Equal(t, "Color", ResolveField(m, context.Background())) // no locale → Base
}
