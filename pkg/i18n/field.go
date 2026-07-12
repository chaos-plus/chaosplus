package i18n

import (
	"context"
	"encoding/json"
)

// ParseField decodes a stored *_i18n JSON column into a locale→value map.
// Empty / NULL / "{}" / "null" / malformed input yields nil so callers fall back
// to the scalar mirror column (ADR-09).
func ParseField(raw string) map[string]string {
	if raw == "" || raw == "{}" || raw == "null" {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// MarshalField encodes a locale→value map for storage in a *_i18n column.
// An empty map yields "" (stored as NULL/empty), keeping the column tidy.
func MarshalField(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}

// BaseValue returns the en-US baseline value — what to mirror into the scalar
// column (used for search/sort/RSQL) and the universal fallback. "" when absent.
func BaseValue(m map[string]string) string {
	return m[Base]
}

// ResolveField picks the value for the request locale (taken from context and
// normalized via Canonical), falling back to en-US, then "". This is the single
// read path for stored multilingual fields; modules must not reimplement it.
func ResolveField(m map[string]string, ctx context.Context) string {
	return ResolveFieldCode(m, CanonicalFromContext(ctx))
}

// ResolveFieldCode is ResolveField with an explicit canonical code (for callers
// that already resolved one, and for tests). An empty/unmatched code falls back
// to en-US, then to "" when the map has no baseline either.
func ResolveFieldCode(m map[string]string, code string) string {
	if len(m) == 0 {
		return ""
	}
	if code != "" {
		if v := m[code]; v != "" {
			return v
		}
	}
	return m[Base]
}
