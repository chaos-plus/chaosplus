package providers

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeolite2DownloadAndDiscoverDatabase(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("LOCALAPPDATA", cache)
	content := []byte("valid-mmdb-payload")
	digest := sha256.Sum256(content)
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.Contains(request.URL.Path, "/releases/latest") {
			_, _ = fmt.Fprintf(writer, `{"tag_name":"v1","assets":[{"name":"GeoLite2-City.mmdb","browser_download_url":"%s/database","digest":"sha256:%s"}]}`, serverURL, hex.EncodeToString(digest[:]))
			return
		}
		_, _ = writer.Write(content)
	}))
	t.Cleanup(server.Close)
	serverURL = server.URL

	provider := &Geolite2{Owner: "owner", Repo: "repo", Db: "GeoLite2-City.mmdb", APIBaseURL: server.URL}
	require.NoError(t, provider.DownloadDb(provider.Db))
	path, err := provider.GetDbPath()
	require.NoError(t, err)
	assert.Equal(t, "GeoLite2-City.mmdb", filepath.Base(path))
	stored, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, content, stored)
}
