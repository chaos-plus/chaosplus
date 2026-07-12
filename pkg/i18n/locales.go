package i18n

import (
	"context"
	"strings"
)

// LocaleInfo describes one supported locale. Code is the canonical BCP-47 code and
// is also the key used in stored *_i18n JSON columns (ADR-09), e.g. "en-US".
type LocaleInfo struct {
	Code  string `json:"code"`  // canonical code, e.g. "zh-CN"
	Label string `json:"label"` // native display label for the switcher, e.g. "简体中文"
	RTL   bool   `json:"rtl"`
}

// Base is the fixed fallback baseline (ADR-09): every *_i18n map must carry it,
// and every unresolved locale lookup falls back to it. Not configurable.
const Base = "en-US"

// builtin are the compiled-in supported locales. ADR-09 收口: the frontend bundles
// these at build time — adding a UI locale is a build change, not a runtime/pack
// operation (the S3-pack path was removed). en-US is the fixed fallback (Base).
var builtin = []LocaleInfo{
	{Code: "en-US", Label: "English"},
	{Code: "zh-CN", Label: "简体中文"},
	{Code: "ms-MY", Label: "Bahasa Melayu"},
}

// Supported returns the supported locale set. The returned slice is a copy.
func Supported() []LocaleInfo {
	return append([]LocaleInfo(nil), builtin...)
}

// Canonical maps an arbitrary Accept-Language-ish input to a supported canonical
// code: exact (case-insensitive) match → language-prefix match → Base. This is
// the single locale normalizer; callers (middleware, data resolution) must not
// hand-roll their own mapping.
//
//	Canonical("ms")              == "ms-MY"  // language-prefix match
//	Canonical("zh-TW")           == "zh-CN"  // language-prefix match (zh)
//	Canonical("zh-CN,en;q=0.9")  == "zh-CN"  // raw Accept-Language header
//	Canonical("ja")              == "en-US"  // unsupported → Base
func Canonical(input string) string {
	in := strings.TrimSpace(input)
	if in == "" {
		return Base
	}
	locales := Supported()
	// 1. exact match on the full code.
	for _, li := range locales {
		if strings.EqualFold(li.Code, in) {
			return li.Code
		}
	}
	// 2. language-prefix match (primary subtag).
	inLang := primarySubtag(in)
	for _, li := range locales {
		if strings.EqualFold(primarySubtag(li.Code), inLang) {
			return li.Code
		}
	}
	return Base
}

// CanonicalFromContext returns the canonical locale for the request, derived from
// the locale set on the context by localeMiddleware. Base when unset/unknown.
func CanonicalFromContext(ctx context.Context) string {
	return Canonical(LocaleFromContext(ctx))
}

// primarySubtag returns the primary language subtag — the part before the first
// '-'/'_'/','/';' separator (covering both BCP-47 codes and raw Accept-Language).
func primarySubtag(code string) string {
	for i, r := range code {
		if r == '-' || r == '_' || r == ',' || r == ';' {
			return code[:i]
		}
	}
	return code
}
