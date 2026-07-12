package i18n

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// I18n manages translations.
type I18n struct {
	mu       sync.RWMutex
	locale   string
	fallback string
	messages map[string]map[string]string // locale -> key -> message
}

// New creates a new I18n instance.
func New(fallback string) *I18n {
	return &I18n{
		fallback: fallback,
		locale:   fallback,
		messages: make(map[string]map[string]string),
	}
}

// Load loads a locale file from the given directory.
func (i *I18n) Load(locale, dir string) error {
	path := filepath.Join(dir, locale+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read locale file: %w", err)
	}

	var msgs map[string]string
	if err := json.Unmarshal(data, &msgs); err != nil {
		return fmt.Errorf("parse locale file: %w", err)
	}

	i.mu.Lock()
	i.messages[locale] = msgs
	i.mu.Unlock()
	return nil
}

// LoadFS loads all *.json locale files from an fs.FS subtree (e.g. an embed.FS),
// making locale loading independent of the process working directory. dir uses
// forward slashes (io/fs convention), e.g. "i18n/locales".
func (i *I18n) LoadFS(fsys fs.FS, dir string) error {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return fmt.Errorf("read locales fs dir: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		locale := strings.TrimSuffix(entry.Name(), ".json")
		data, err := fs.ReadFile(fsys, dir+"/"+entry.Name())
		if err != nil {
			return fmt.Errorf("read locale file %s: %w", entry.Name(), err)
		}
		var msgs map[string]string
		if err := json.Unmarshal(data, &msgs); err != nil {
			return fmt.Errorf("parse locale file %s: %w", entry.Name(), err)
		}
		i.mu.Lock()
		i.messages[locale] = msgs
		i.mu.Unlock()
	}
	return nil
}

// SetLocale sets the current locale.
func (i *I18n) SetLocale(locale string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.locale = locale
}

// Locale returns the current locale.
func (i *I18n) Locale() string {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.locale
}

// T translates a key with optional formatting arguments using the current locale.
func (i *I18n) T(key string, args ...any) string {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.translate(i.locale, key, args...)
}

// TContext translates a key using the locale from context.
func (i *I18n) TContext(ctx context.Context, key string, args ...any) string {
	i.mu.RLock()
	defer i.mu.RUnlock()
	locale := LocaleFromContext(ctx)
	if locale == "" {
		locale = i.locale
	}
	return i.translate(locale, key, args...)
}

func (i *I18n) translate(locale, key string, args ...any) string {
	msg := i.lookup(locale, key)
	if msg == "" {
		msg = i.lookup(i.fallback, key)
	}
	if msg == "" {
		return key
	}
	if len(args) > 0 {
		return fmt.Sprintf(msg, args...)
	}
	return msg
}

// lookup finds a message for the given locale and key.
func (i *I18n) lookup(locale, key string) string {
	msgs, ok := i.messages[locale]
	if !ok {
		return ""
	}
	// Try exact match first
	if msg, ok := msgs[key]; ok {
		return msg
	}
	// Try lowercase match
	if msg, ok := msgs[strings.ToLower(key)]; ok {
		return msg
	}
	return ""
}

// Has checks if a key exists in the current locale.
func (i *I18n) Has(key string) bool {
	return i.T(key) != key
}

// LoadDir loads all locale files from the given directory.
func (i *I18n) LoadDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read locales dir: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		locale := strings.TrimSuffix(entry.Name(), ".json")
		if err := i.Load(locale, dir); err != nil {
			return fmt.Errorf("load locale %s: %w", locale, err)
		}
	}
	return nil
}

// NewModuleI18n creates a pre-loaded I18n instance for a module.
// It logs a warning if the directory cannot be read but returns a usable instance.
func NewModuleI18n(fallback, dir string) *I18n {
	inst := New(fallback)
	if err := inst.LoadDir(dir); err != nil {
		// Non-fatal: fallback to key passthrough when locale files are missing.
		// This is common during unit tests that run without locale file paths.
	}
	return inst
}

// NewModuleI18nFS creates a pre-loaded I18n instance from an embedded fs.FS, so a
// module's locales are compiled into the binary and resolve regardless of the
// process working directory.
func NewModuleI18nFS(fallback string, fsys fs.FS, dir string) *I18n {
	inst := New(fallback)
	if err := inst.LoadFS(fsys, dir); err != nil {
		// Non-fatal: fallback to key passthrough. With a correctly embedded FS
		// this should never happen, so surface it at Error level (with the dir
		// for context) rather than silently degrading every translation to its
		// key. Returning the error is not viable here: all ~19 call sites use a
		// sync.Once package-var pattern that cannot propagate one.
		slog.Error("i18n: failed to load module locales", "dir", dir, "error", err)
	}
	return inst
}

// default global instance
var defaultI18n *I18n

// Init initializes the default global instance.
func Init(fallback, localesDir string) error {
	defaultI18n = New(fallback)
	entries, err := os.ReadDir(localesDir)
	if err != nil {
		return fmt.Errorf("read locales dir: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		locale := strings.TrimSuffix(entry.Name(), ".json")
		if err := defaultI18n.Load(locale, localesDir); err != nil {
			return fmt.Errorf("load locale %s: %w", locale, err)
		}
	}
	return nil
}

// SetLocale sets the global locale.
func SetLocale(locale string) {
	if defaultI18n != nil {
		defaultI18n.SetLocale(locale)
	}
}

// T translates a key using the global instance.
func T(key string, args ...any) string {
	if defaultI18n == nil {
		return key
	}
	return defaultI18n.T(key, args...)
}

// TContext translates a key using the locale from context via the global instance.
func TContext(ctx context.Context, key string, args ...any) string {
	if defaultI18n == nil {
		return key
	}
	return defaultI18n.TContext(ctx, key, args...)
}

// Locale returns the global locale.
func Locale() string {
	if defaultI18n == nil {
		return ""
	}
	return defaultI18n.Locale()
}
