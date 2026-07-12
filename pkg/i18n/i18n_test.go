package i18n

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestI18n_T(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "en.json"), []byte(`{"hello":"Hello","not_found":"Not found"}`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "zh.json"), []byte(`{"hello":"你好","not_found":"不存在"}`), 0644))

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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "en.json"), []byte(`{"success":"OK"}`), 0644))

	require.NoError(t, Init("en", dir))
	assert.Equal(t, "OK", T("success"))
	assert.Equal(t, "en", Locale())
}
