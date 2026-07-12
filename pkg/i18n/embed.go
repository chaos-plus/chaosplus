package i18n

import (
	"embed"
	"encoding/json"
	"io/fs"
	"strings"
)

// LocalesFS holds the embedded locale files (built into the binary).
//
//go:embed locales/*.json
var LocalesFS embed.FS

// InitEmbedded initializes the default global instance from an embedded fs.FS.
func InitEmbedded(fallback string) error {
	defaultI18n = New(fallback)

	// The embed pattern creates a subdirectory "locales" inside the FS
	localesDir, err := fs.Sub(LocalesFS, "locales")
	if err != nil {
		return err
	}

	entries, err := fs.ReadDir(localesDir, ".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		locale := strings.TrimSuffix(entry.Name(), ".json")
		data, err := fs.ReadFile(localesDir, entry.Name())
		if err != nil {
			return err
		}
		var msgs map[string]string
		if err := json.Unmarshal(data, &msgs); err != nil {
			return err
		}
		defaultI18n.mu.Lock()
		defaultI18n.messages[locale] = msgs
		defaultI18n.mu.Unlock()
	}
	return nil
}
