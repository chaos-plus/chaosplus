package i18n

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCanonical(t *testing.T) {
	cases := map[string]string{
		"en-US":          "en-US", // exact
		"EN-us":          "en-US", // case-insensitive exact
		"zh-CN":          "zh-CN",
		"ms-MY":          "ms-MY",
		"ms":             "ms-MY", // prefix
		"zh":             "zh-CN", // prefix
		"zh-TW":          "zh-CN", // prefix collapse
		"en_US":          "en-US", // underscore separator
		"zh-CN,en;q=0.9": "zh-CN", // raw Accept-Language header
		"ja":             "en-US", // unsupported → Base
		"ja-JP":          "en-US",
		"":               "en-US", // empty → Base
		"   ":            "en-US",
		"x-pirate":       "en-US",
	}
	for in, want := range cases {
		assert.Equalf(t, want, Canonical(in), "Canonical(%q)", in)
	}
}

func TestSupported_IncludesBuiltin(t *testing.T) {
	codes := map[string]bool{}
	for _, li := range Supported() {
		codes[li.Code] = true
	}
	for _, c := range []string{"en-US", "zh-CN", "ms-MY"} {
		assert.Truef(t, codes[c], "Supported() should include built-in %s", c)
	}
}

func TestCanonicalFromContext(t *testing.T) {
	assert.Equal(t, "zh-CN", CanonicalFromContext(WithLocale(context.Background(), "zh")))
	assert.Equal(t, "en-US", CanonicalFromContext(context.Background())) // no locale set
}
