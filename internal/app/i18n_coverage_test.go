package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// placeholderMark matches a run of two or more '?' characters, which is how
// untranslated placeholder strings appear in locale files (e.g. "??????").
var placeholderMark = regexp.MustCompile(`\?\?`)

// localeFiles returns every embedded locale JSON under the repo's i18n/locales
// directories (pkg and all modules), keyed by bundle directory then locale.
func localeFiles(t *testing.T) map[string]map[string]map[string]string {
	t.Helper()
	// Tests run with the package directory as cwd; the repo root is two levels up.
	root := "../../"
	bundles := make(map[string]map[string]map[string]string)
	err := filepath.Walk(filepath.Join(root, "internal"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".json") ||
			!strings.Contains(path, string(filepath.Separator)+"i18n"+string(filepath.Separator)+"locales") {
			return err
		}
		base := filepath.Dir(path)
		locale := strings.TrimSuffix(filepath.Base(path), ".json")
		if bundles[base] == nil {
			bundles[base] = make(map[string]map[string]string)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var msgs map[string]string
		if err := json.Unmarshal(data, &msgs); err != nil {
			t.Errorf("invalid JSON in %s: %v", path, err)
			return nil
		}
		bundles[base][locale] = msgs
		return nil
	})
	require.NoError(t, err)
	return bundles
}

// TestLocaleBundlesHaveNoPlaceholders guards against untranslated '?' runs in
// any locale file, which surface to clients as raw question marks.
func TestLocaleBundlesHaveNoPlaceholders(t *testing.T) {
	for base, locales := range localeFiles(t) {
		for locale, msgs := range locales {
			for key, value := range msgs {
				if placeholderMark.MatchString(value) {
					t.Errorf("%s [%s] key %q contains untranslated placeholder: %q", base, locale, key, value)
				}
			}
		}
	}
}

// TestLocaleBundlesAreAlignedAcrossLanguages verifies every bundle defines the
// same key set in en-US, zh-CN, and ms-MY so a request in any supported locale
// resolves every message without falling back to the raw key.
func TestLocaleBundlesAreAlignedAcrossLanguages(t *testing.T) {
	for base, locales := range localeFiles(t) {
		en, ok := locales["en-US"]
		if !ok {
			continue
		}
		enKeys := make(map[string]bool, len(en))
		for k := range en {
			enKeys[k] = true
		}
		for locale, msgs := range locales {
			if locale == "en-US" {
				continue
			}
			for key := range enKeys {
				if msgs[key] == "" {
					t.Errorf("%s [%s] is missing key %q present in en-US", base, locale, key)
				}
			}
			for key := range msgs {
				if !enKeys[key] {
					t.Errorf("%s [%s] has key %q missing from en-US", base, locale, key)
				}
			}
		}
	}
}
