package geoip

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModule_LookupGeoIP(t *testing.T) {
	_, api := humatest.New(t)
	(&Module{}).RegisterREST(api)

	// 127.0.0.1 always resolves (the ipapi provider special-cases loopback, so it
	// succeeds without network or a database). Data is a list with one entry per
	// provider that resolved; which providers are registered is not deterministic,
	// so we assert only that the list is non-empty and every entry echoes the ip.
	resp := api.Get("/geoip/127.0.0.1")
	require.Equal(t, http.StatusOK, resp.Code)

	var body struct {
		Code int `json:"code"`
		Data []struct {
			Provider string `json:"provider"`
			Ip       string `json:"ip"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Equal(t, 0, body.Code)
	require.NotEmpty(t, body.Data)
	for _, entry := range body.Data {
		assert.Equal(t, "127.0.0.1", entry.Ip)
		assert.NotEmpty(t, entry.Provider)
	}
}

func TestModule_LookupGeoIP_RejectsNonIPv4(t *testing.T) {
	_, api := humatest.New(t)
	(&Module{}).RegisterREST(api)

	// A malformed address must be rejected as a parameter validation error
	// before any provider is consulted.
	resp := api.Get("/geoip/123.123.123.1234")
	require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
	assert.Contains(t, resp.Body.String(), "path.ip")
	// The detail is emitted as an i18n key; respx localizes it per request.
	assert.Contains(t, resp.Body.String(), "invalid_ipv4")
}

func TestModule_LookupSelf_Redirects(t *testing.T) {
	_, api := humatest.New(t)
	(&Module{}).RegisterREST(api)

	// humatest sets RemoteAddr to 127.0.0.1:12345, so /geoip redirects to the
	// caller's own detected ip.
	resp := api.Get("/geoip")
	assert.Equal(t, http.StatusTemporaryRedirect, resp.Code)
	assert.Equal(t, "/geoip/127.0.0.1", resp.Header().Get("Location"))
}

func TestModule_LookupSelf_PrefersPublicIPv4(t *testing.T) {
	_, api := humatest.New(t)
	(&Module{}).RegisterREST(api)

	// X-Forwarded-For carries a private hop and a public client; the public
	// IPv4 must win regardless of position, and the IPv6 hop is ignored.
	resp := api.Get("/geoip", "X-Forwarded-For: 10.0.0.1, 203.0.113.9, 2001:db8::1")
	assert.Equal(t, http.StatusTemporaryRedirect, resp.Code)
	assert.Equal(t, "/geoip/203.0.113.9", resp.Header().Get("Location"))
}

func TestModule_LookupSelf_IPv6LoopbackMapsToIPv4(t *testing.T) {
	_, api := humatest.New(t)
	(&Module{}).RegisterREST(api)

	// A browser hitting localhost forwards ::1; it must map to 127.0.0.1
	// rather than redirecting to /geoip/::1.
	resp := api.Get("/geoip", "X-Real-IP: ::1")
	assert.Equal(t, http.StatusTemporaryRedirect, resp.Code)
	assert.Equal(t, "/geoip/127.0.0.1", resp.Header().Get("Location"))
}
