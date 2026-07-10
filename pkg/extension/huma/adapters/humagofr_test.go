package humagofr

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gofr.dev/pkg/gofr"
)

var lastModified = time.Now()

type GreetingInput struct {
	ID          string `path:"id"`
	ContentType string `header:"Content-Type"`
	Num         int    `query:"num"`
	Body        struct {
		Suffix string `json:"suffix" maxLength:"5"`
	}
}

type GreetingOutput struct {
	ETag         string    `header:"ETag"`
	LastModified time.Time `header:"Last-Modified"`
	Body         struct {
		Greeting    string `json:"greeting"`
		Suffix      string `json:"suffix"`
		Length      int    `json:"length"`
		ContentType string `json:"content_type"`
		Num         int    `json:"num"`
	}
}

func greet(ctx context.Context, input *GreetingInput) (*GreetingOutput, error) {
	resp := &GreetingOutput{}
	resp.ETag = "abc123"
	resp.LastModified = lastModified
	resp.Body.Greeting = "Hello, " + input.ID + input.Body.Suffix
	resp.Body.Suffix = input.Body.Suffix
	resp.Body.Length = len(resp.Body.Greeting)
	resp.Body.ContentType = input.ContentType
	resp.Body.Num = input.Num
	return resp, nil
}

// newTestAPI builds an API on a standalone adapter (no GoFr runtime needed),
// which is exactly what huma.API/humatest exercise via Adapter.ServeHTTP.
func newTestAPI(t *testing.T, prefix string) (huma.API, humatest.TestAPI) {
	t.Helper()
	adapter := NewAdapter(prefix)
	api := huma.NewAPI(huma.DefaultConfig("Test", "1.0.0"), adapter)
	return api, humatest.Wrap(t, api)
}

func TestGreeting(t *testing.T) {
	api, tapi := newTestAPI(t, "")
	huma.Register(api, huma.Operation{
		OperationID: "greet",
		Method:      http.MethodPost,
		Path:        "/foo/{id}",
	}, greet)

	resp := tapi.Post("/foo/123?num=5",
		"Content-Type: application/json",
		strings.NewReader(`{"suffix": "!"}`),
	)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	var out GreetingOutput
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out.Body))
	assert.Equal(t, "Hello, 123!", out.Body.Greeting)
	assert.Equal(t, "!", out.Body.Suffix)
	assert.Equal(t, len("Hello, 123!"), out.Body.Length)
	assert.Equal(t, "application/json", out.Body.ContentType)
	assert.Equal(t, 5, out.Body.Num)
	assert.Equal(t, "abc123", resp.Header().Get("ETag"))
}

func TestPathParamDecoding(t *testing.T) {
	api, tapi := newTestAPI(t, "")

	type PathInput struct {
		Value string `path:"value"`
	}
	type Output struct {
		Body struct {
			Value string `json:"value"`
		}
	}

	huma.Get(api, "/test/{value}", func(ctx context.Context, input *PathInput) (*Output, error) {
		out := &Output{}
		out.Body.Value = input.Value
		return out, nil
	})

	// Simple path parameter.
	resp := tapi.Get("/test/hello")
	assert.Equal(t, http.StatusOK, resp.Code)
	var normal Output
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &normal.Body))
	assert.Equal(t, "hello", normal.Body.Value)

	// URL-encoded path parameter should be decoded.
	resp = tapi.Get("/test/hello%20world")
	assert.Equal(t, http.StatusOK, resp.Code)
	var special Output
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &special.Body))
	assert.Equal(t, "hello world", special.Body.Value)
}

func TestPrefix(t *testing.T) {
	adapter := NewAdapter("/api")
	config := huma.DefaultConfig("Test", "1.0.0")
	config.Servers = []*huma.Server{{URL: "http://localhost:8888/api"}}
	api := huma.NewAPI(config, adapter)

	type Output struct {
		Body struct {
			Field string `json:"field"`
		}
	}
	huma.Get(api, "/test", func(ctx context.Context, input *struct{}) (*Output, error) {
		return &Output{}, nil
	})

	tapi := humatest.Wrap(t, api)

	// The operation registered at /test is served under the /api prefix.
	resp := tapi.Get("/api/test")
	assert.Equal(t, http.StatusOK, resp.Code)

	// Like humago, the prefix applies to every adapter-registered route,
	// including the built-in OpenAPI endpoint. The OpenAPI *path* itself
	// (inside the document) stays prefix-free.
	resp = tapi.Get("/api/openapi.json")
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), `"/test"`)
}

// TestMiddlewareBridge verifies the core GoFr integration: the middleware serves
// Huma-owned routes and passes everything else through to the next handler.
func TestMiddlewareBridge(t *testing.T) {
	adapter := NewAdapter("")
	api := huma.NewAPI(huma.DefaultConfig("Test", "1.0.0"), adapter)

	type Output struct {
		Body struct {
			OK bool `json:"ok"`
		}
	}
	huma.Get(api, "/huma", func(ctx context.Context, input *struct{}) (*Output, error) {
		out := &Output{}
		out.Body.OK = true
		return out, nil
	})

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusTeapot)
	})
	handler := adapter.Middleware()(next)

	// Huma-owned route: served by the mux, next is NOT called.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/huma", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.False(t, nextCalled, "Huma should handle its own route")

	// Unknown route: passed through to GoFr's handler.
	nextCalled = false
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/gofr-owned", nil))
	assert.True(t, nextCalled, "non-Huma route should fall through to GoFr")
	assert.Equal(t, http.StatusTeapot, rec.Code)
}

// freePort asks the OS for an unused TCP port and returns it as a string. GoFr
// checks that its configured HTTP port is available when the first route is
// registered, so tests must point it at a free port rather than the default.
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	return strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
}

// TestGofrWiring proves New wires the adapter into a real *gofr.App (registers
// the middleware) and returns a working API. The huma.API is exercised through
// its adapter, so no ports are bound.
func TestGofrWiring(t *testing.T) {
	t.Setenv("METRICS_PORT", "0")
	t.Setenv("HTTP_PORT", freePort(t)) // avoid depending on the default port 8000
	app := gofr.New()

	api := New(app, huma.DefaultConfig("Test", "1.0.0"))
	huma.Register(api, huma.Operation{
		OperationID: "greet",
		Method:      http.MethodPost,
		Path:        "/foo/{id}",
	}, greet)

	tapi := humatest.Wrap(t, api)
	resp := tapi.Post("/foo/123?num=5",
		"Content-Type: application/json",
		strings.NewReader(`{"suffix": "!"}`),
	)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	var out GreetingOutput
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out.Body))
	assert.Equal(t, "Hello, 123!", out.Body.Greeting)
}

func TestUnwrap(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	ctx := NewContext(&huma.Operation{}, req, rec)

	r, w := Unwrap(ctx)
	assert.Same(t, req, r)
	assert.Same(t, rec, w)
}

func TestUnwrapPanicsOnForeignContext(t *testing.T) {
	assert.Panics(t, func() {
		Unwrap(nil)
	})
}
