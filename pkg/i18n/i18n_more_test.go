package i18n

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeLocale is a small helper to create a locale JSON file in dir.
func writeLocale(t *testing.T, dir, locale, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, locale+".json"), []byte(body), 0644))
}

func TestI18n_Translate(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeLocale(t, dir, "en", `{"hello":"Hello","greet":"Hello, %s!","only_en":"EnglishOnly"}`)
	writeLocale(t, dir, "zh", `{"hello":"你好","greet":"你好，%s！"}`)

	i := New("en")
	require.NoError(t, i.Load("en", dir))
	require.NoError(t, i.Load("zh", dir))

	tests := []struct {
		name   string
		locale string
		key    string
		args   []any
		want   string
	}{
		{name: "existing key default locale", locale: "en", key: "hello", want: "Hello"},
		{name: "existing key non-default locale", locale: "zh", key: "hello", want: "你好"},
		{name: "missing key returns key", locale: "en", key: "does_not_exist", want: "does_not_exist"},
		{name: "interpolation with args", locale: "en", key: "greet", args: []any{"World"}, want: "Hello, World!"},
		{name: "interpolation non-default locale", locale: "zh", key: "greet", args: []any{"世界"}, want: "你好，世界！"},
		{name: "fallback to default locale when missing in current", locale: "zh", key: "only_en", want: "EnglishOnly"},
		{name: "unknown locale falls back to default", locale: "fr", key: "hello", want: "Hello"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			i.SetLocale(tc.locale)
			got := i.T(tc.key, tc.args...)

			// Assert
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestI18n_LookupLowercaseMatch(t *testing.T) {
	// Arrange: stored key is lowercase, queried with mixed case.
	dir := t.TempDir()
	writeLocale(t, dir, "en", `{"some_key":"Lowercased"}`)

	i := New("en")
	require.NoError(t, i.Load("en", dir))

	// Act
	got := i.T("SOME_KEY")

	// Assert
	assert.Equal(t, "Lowercased", got)
}

func TestI18n_Has(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeLocale(t, dir, "en", `{"present":"yes"}`)

	i := New("en")
	require.NoError(t, i.Load("en", dir))

	// Act + Assert
	assert.True(t, i.Has("present"), "existing key should be reported as present")
	assert.False(t, i.Has("absent"), "missing key should be reported as absent")
}

func TestI18n_TContext(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeLocale(t, dir, "en", `{"hello":"Hello"}`)
	writeLocale(t, dir, "zh", `{"hello":"你好"}`)

	i := New("en")
	require.NoError(t, i.Load("en", dir))
	require.NoError(t, i.Load("zh", dir))

	t.Run("locale set in context overrides default", func(t *testing.T) {
		// Arrange
		ctx := WithLocale(context.Background(), "zh")
		// Act
		got := i.TContext(ctx, "hello")
		// Assert
		assert.Equal(t, "你好", got)
	})

	t.Run("no locale in context uses default", func(t *testing.T) {
		// Act
		got := i.TContext(context.Background(), "hello")
		// Assert
		assert.Equal(t, "Hello", got)
	})

	t.Run("context locale with args", func(t *testing.T) {
		dir2 := t.TempDir()
		writeLocale(t, dir2, "zh", `{"greet":"你好，%s"}`)
		j := New("en")
		require.NoError(t, j.Load("zh", dir2))

		ctx := WithLocale(context.Background(), "zh")
		got := j.TContext(ctx, "greet", "世界")
		assert.Equal(t, "你好，世界", got)
	})
}

func TestLocaleContextHelpers(t *testing.T) {
	t.Run("WithLocale and LocaleFromContext round trip", func(t *testing.T) {
		ctx := WithLocale(context.Background(), "ja")
		assert.Equal(t, "ja", LocaleFromContext(ctx))
	})

	t.Run("empty context returns empty locale", func(t *testing.T) {
		assert.Equal(t, "", LocaleFromContext(context.Background()))
	})
}

func TestI18n_LocaleGetter(t *testing.T) {
	i := New("en")
	assert.Equal(t, "en", i.Locale())
	i.SetLocale("zh")
	assert.Equal(t, "zh", i.Locale())
}

func TestI18n_Load_Errors(t *testing.T) {
	t.Run("missing file returns error", func(t *testing.T) {
		i := New("en")
		err := i.Load("en", t.TempDir())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "read locale file")
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		dir := t.TempDir()
		writeLocale(t, dir, "en", `{not valid json`)
		i := New("en")
		err := i.Load("en", dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse locale file")
	})
}

func TestI18n_LoadDir(t *testing.T) {
	t.Run("loads all json files and ignores non-json and dirs", func(t *testing.T) {
		// Arrange
		dir := t.TempDir()
		writeLocale(t, dir, "en", `{"hello":"Hello"}`)
		writeLocale(t, dir, "zh", `{"hello":"你好"}`)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("ignore me"), 0644))
		require.NoError(t, os.Mkdir(filepath.Join(dir, "nested"), 0755))

		i := New("en")
		// Act
		require.NoError(t, i.LoadDir(dir))

		// Assert
		assert.Equal(t, "Hello", i.T("hello"))
		i.SetLocale("zh")
		assert.Equal(t, "你好", i.T("hello"))
	})

	t.Run("missing dir returns error", func(t *testing.T) {
		i := New("en")
		err := i.LoadDir(filepath.Join(t.TempDir(), "nope"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "read locales dir")
	})

	t.Run("invalid json in dir returns error", func(t *testing.T) {
		dir := t.TempDir()
		writeLocale(t, dir, "en", `{broken`)
		i := New("en")
		err := i.LoadDir(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "load locale")
	})
}

func TestI18n_LoadFS(t *testing.T) {
	t.Run("loads json files from fs.FS subtree", func(t *testing.T) {
		// Arrange
		fsys := fstest.MapFS{
			"i18n/locales/en.json":  {Data: []byte(`{"hello":"Hello","greet":"Hi %s"}`)},
			"i18n/locales/zh.json":  {Data: []byte(`{"hello":"你好"}`)},
			"i18n/locales/skip.txt": {Data: []byte("ignored")},
		}

		i := New("en")
		// Act
		require.NoError(t, i.LoadFS(fsys, "i18n/locales"))

		// Assert
		assert.Equal(t, "Hello", i.T("hello"))
		assert.Equal(t, "Hi Bob", i.T("greet", "Bob"))
		i.SetLocale("zh")
		assert.Equal(t, "你好", i.T("hello"))
	})

	t.Run("missing dir returns error", func(t *testing.T) {
		i := New("en")
		err := i.LoadFS(fstest.MapFS{}, "does/not/exist")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "read locales fs dir")
	})

	t.Run("invalid json returns error", func(t *testing.T) {
		fsys := fstest.MapFS{
			"locales/en.json": {Data: []byte(`{broken`)},
		}
		i := New("en")
		err := i.LoadFS(fsys, "locales")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse locale file")
	})
}

func TestNewModuleI18n(t *testing.T) {
	t.Run("loads locales from valid dir", func(t *testing.T) {
		dir := t.TempDir()
		writeLocale(t, dir, "en", `{"hello":"Hello"}`)
		inst := NewModuleI18n("en", dir)
		assert.Equal(t, "Hello", inst.T("hello"))
	})

	t.Run("missing dir returns usable passthrough instance", func(t *testing.T) {
		inst := NewModuleI18n("en", filepath.Join(t.TempDir(), "missing"))
		require.NotNil(t, inst)
		assert.Equal(t, "hello", inst.T("hello"))
	})
}

func TestNewModuleI18nFS(t *testing.T) {
	t.Run("loads locales from fs", func(t *testing.T) {
		fsys := fstest.MapFS{
			"locales/en.json": {Data: []byte(`{"hello":"Hello"}`)},
		}
		inst := NewModuleI18nFS("en", fsys, "locales")
		assert.Equal(t, "Hello", inst.T("hello"))
	})

	t.Run("missing dir returns usable passthrough instance", func(t *testing.T) {
		inst := NewModuleI18nFS("en", fstest.MapFS{}, "missing")
		require.NotNil(t, inst)
		assert.Equal(t, "hello", inst.T("hello"))
	})
}

func TestInit_Errors(t *testing.T) {
	t.Run("missing dir returns error", func(t *testing.T) {
		err := Init("en", filepath.Join(t.TempDir(), "missing"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "read locales dir")
	})

	t.Run("invalid json returns error", func(t *testing.T) {
		dir := t.TempDir()
		writeLocale(t, dir, "en", `{broken`)
		err := Init("en", dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "load locale")
	})

	t.Run("ignores non-json and dirs", func(t *testing.T) {
		dir := t.TempDir()
		writeLocale(t, dir, "en", `{"k":"v"}`)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "x.txt"), []byte("nope"), 0644))
		require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0755))
		require.NoError(t, Init("en", dir))
		assert.Equal(t, "v", T("k"))
	})
}

func TestGlobalFunctions(t *testing.T) {
	// Arrange: initialize the global default instance.
	dir := t.TempDir()
	writeLocale(t, dir, "en", `{"hello":"Hello"}`)
	writeLocale(t, dir, "zh", `{"hello":"你好"}`)
	require.NoError(t, Init("en", dir))

	t.Run("global T and Locale", func(t *testing.T) {
		assert.Equal(t, "Hello", T("hello"))
		assert.Equal(t, "en", Locale())
	})

	t.Run("global SetLocale", func(t *testing.T) {
		SetLocale("zh")
		assert.Equal(t, "zh", Locale())
		assert.Equal(t, "你好", T("hello"))
		SetLocale("en") // restore
	})

	t.Run("global TContext with context locale", func(t *testing.T) {
		ctx := WithLocale(context.Background(), "zh")
		assert.Equal(t, "你好", TContext(ctx, "hello"))
	})

	t.Run("global TContext without context locale uses default", func(t *testing.T) {
		assert.Equal(t, "Hello", TContext(context.Background(), "hello"))
	})
}

func TestGlobalFunctions_NilDefault(t *testing.T) {
	// Arrange: force the global instance to nil to exercise nil guards.
	defaultI18n = nil

	// Act + Assert
	assert.Equal(t, "key", T("key"))
	assert.Equal(t, "key", TContext(context.Background(), "key"))
	assert.Equal(t, "", Locale())
	// SetLocale on nil should be a no-op (not panic).
	assert.NotPanics(t, func() { SetLocale("zh") })
}

func TestInitEmbedded(t *testing.T) {
	// Arrange + Act: load from the package's embedded locales/*.json.
	require.NoError(t, InitEmbedded("en-US"))

	// Assert: the embedded en-US/zh-CN locales are present.
	assert.Equal(t, "en-US", Locale())
	// At least one known locale is loaded; verify by checking a key resolves
	// to something other than the key, OR that Has reports presence for a
	// real key if any exist. We assert the instance is wired up.
	require.NotNil(t, defaultI18n)
	defaultI18n.mu.RLock()
	_, hasEn := defaultI18n.messages["en-US"]
	_, hasZh := defaultI18n.messages["zh-CN"]
	defaultI18n.mu.RUnlock()
	assert.True(t, hasEn, "embedded en locale should be loaded")
	assert.True(t, hasZh, "embedded zh locale should be loaded")
}
