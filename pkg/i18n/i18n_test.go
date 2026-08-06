package i18n

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestI18n_T(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "en.json"), []byte(`{"hello":"Hello","not_found":"Not found"}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "zh.json"), []byte(`{"hello":"你好","not_found":"不存在"}`), 0o600))

	i := New("en")
	require.NoError(t, i.Load("en", dir))
	require.NoError(t, i.Load("zh", dir))

	assert.Equal(t, "Hello", i.T("hello"))

	i.SetLocale("zh")
	assert.Equal(t, "你好", i.T("hello"))
	assert.Equal(t, "不存在", i.T("not_found"))

	// fallback
	assert.Equal(t, "unknown", i.T("unknown"))
}

func TestInit(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "en.json"), []byte(`{"success":"OK"}`), 0o600))

	require.NoError(t, Init("en", dir))
	assert.Equal(t, "OK", T("success"))
	assert.Equal(t, "en", Locale())
}

// readFailFS lists locale entries but fails when the file is opened, so LoadFS
// exercises its read-error path deterministically without a real filesystem.
type readFailFS struct {
	fstest.MapFS
}

func (f readFailFS) Open(name string) (fs.File, error) {
	if strings.HasSuffix(name, ".json") {
		return nil, fs.ErrPermission
	}
	return f.MapFS.Open(name)
}

func TestRegisterFS_NotInitialized(t *testing.T) {
	old := defaultI18n
	defaultI18n = nil
	defer func() { defaultI18n = old }()

	err := RegisterFS(fstest.MapFS{"en.json": {Data: []byte(`{"x":"y"}`)}}, ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
}

func TestRegisterFS_AddsNewLocale(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "en.json"), []byte(`{"success":"OK"}`), 0o600))
	require.NoError(t, Init("en", dir))

	fsys := fstest.MapFS{
		"locales/en-US.json": {Data: []byte(`{"success":"OK"}`)},
		"locales/zh-CN.json": {Data: []byte(`{"success":"OK"}`)},
		"locales/ms-MY.json": {Data: []byte(`{"success":"OK"}`)},
		"locales/fr.json":    {Data: []byte(`{"success":"OK"}`)},
	}
	require.NoError(t, RegisterFS(fsys, "locales"))
	assert.Equal(t, "OK", T("success"))
}

func TestLoadFS_ReadError(t *testing.T) {
	i := New(Base)
	fsys := readFailFS{MapFS: fstest.MapFS{"en.json": {Data: []byte(`{"hello":"Hi"}`)}}}
	err := i.LoadFS(fsys, ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read locale file")
}
