package providers

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIP2LocationDownloadExtractAndDiscoverDatabase(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	var archive bytes.Buffer
	zipWriter := zip.NewWriter(&archive)
	entry, err := zipWriter.Create("IP2LOCATION-LITE-DB11.BIN")
	require.NoError(t, err)
	_, err = entry.Write([]byte("database payload"))
	require.NoError(t, err)
	require.NoError(t, zipWriter.Close())

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(archive.Bytes())
	}))
	t.Cleanup(server.Close)
	provider := &IP2Location{Token: "token", DownloadBaseURL: server.URL}

	require.NoError(t, provider.DownloadDb())
	path, err := provider.GetDbPath()
	require.NoError(t, err)
	assert.Equal(t, "IP2LOCATION-LITE-DB11.BIN", filepath.Base(path))
}
