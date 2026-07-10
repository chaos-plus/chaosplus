// Package humagofr provides a Huma v2 adapter for the GoFr framework
// (https://gofr.dev).
//
// GoFr intentionally hides the underlying net/http transport: its handlers have
// the signature func(*gofr.Context) (any, error) and it never exposes the raw
// http.ResponseWriter / *http.Request that Huma needs. To bridge the two, this
// adapter keeps its own *http.ServeMux (identical in spirit to the official
// humago adapter) and plugs into GoFr as a middleware. Because GoFr registers a
// catch-all route at startup, a registered middleware runs for every request:
// the middleware serves Huma-owned routes from the internal mux and passes all
// other requests through to GoFr's own router.
package humagofr

import (
	"context"
	"crypto/tls"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/queryparam"

	"gofr.dev/pkg/gofr"
	gofrHTTP "gofr.dev/pkg/gofr/http"
)

// MultipartMaxMemory is the maximum memory to use when parsing multipart
// form data.
var MultipartMaxMemory int64 = 8 * 1024

// Unwrap extracts the underlying HTTP request and response writer from a Huma
// context. If passed a context from a different adapter it will panic.
func Unwrap(ctx huma.Context) (*http.Request, http.ResponseWriter) {
	for {
		if c, ok := ctx.(interface{ Unwrap() huma.Context }); ok {
			ctx = c.Unwrap()
			continue
		}
		break
	}
	if c, ok := ctx.(*gofrContext); ok {
		return c.Unwrap()
	}
	panic("not a humagofr context")
}

type gofrContext struct {
	op     *huma.Operation
	r      *http.Request
	w      http.ResponseWriter
	status int
}

// check that gofrContext implements huma.Context
var _ huma.Context = &gofrContext{}

func (c *gofrContext) Unwrap() (*http.Request, http.ResponseWriter) {
	return c.r, c.w
}

func (c *gofrContext) Operation() *huma.Operation {
	return c.op
}

func (c *gofrContext) Context() context.Context {
	return c.r.Context()
}

func (c *gofrContext) Method() string {
	return c.r.Method
}

func (c *gofrContext) Host() string {
	return c.r.Host
}

func (c *gofrContext) RemoteAddr() string {
	return c.r.RemoteAddr
}

func (c *gofrContext) URL() url.URL {
	return *c.r.URL
}

func (c *gofrContext) Param(name string) string {
	return c.r.PathValue(name)
}

func (c *gofrContext) Query(name string) string {
	return queryparam.Get(c.r.URL.RawQuery, name)
}

func (c *gofrContext) Header(name string) string {
	return c.r.Header.Get(name)
}

func (c *gofrContext) EachHeader(cb func(name, value string)) {
	for name, values := range c.r.Header {
		for _, value := range values {
			cb(name, value)
		}
	}
}

func (c *gofrContext) BodyReader() io.Reader {
	return c.r.Body
}

func (c *gofrContext) GetMultipartForm() (*multipart.Form, error) {
	err := c.r.ParseMultipartForm(MultipartMaxMemory)
	return c.r.MultipartForm, err
}

func (c *gofrContext) SetReadDeadline(deadline time.Time) error {
	return huma.SetReadDeadline(c.w, deadline)
}

func (c *gofrContext) SetStatus(code int) {
	c.status = code
	c.w.WriteHeader(code)
}

func (c *gofrContext) Status() int {
	return c.status
}

func (c *gofrContext) AppendHeader(name string, value string) {
	c.w.Header().Add(name, value)
}

func (c *gofrContext) SetHeader(name string, value string) {
	c.w.Header().Set(name, value)
}

func (c *gofrContext) BodyWriter() io.Writer {
	return c.w
}

func (c *gofrContext) TLS() *tls.ConnectionState {
	return c.r.TLS
}

func (c *gofrContext) Version() huma.ProtoVersion {
	return huma.ProtoVersion{
		Proto:      c.r.Proto,
		ProtoMajor: c.r.ProtoMajor,
		ProtoMinor: c.r.ProtoMinor,
	}
}

// NewContext creates a new Huma context from an HTTP request and response.
func NewContext(op *huma.Operation, r *http.Request, w http.ResponseWriter) huma.Context {
	return &gofrContext{op: op, r: r, w: w}
}

// Adapter bridges Huma to GoFr. It owns an internal *http.ServeMux that holds
// all Huma-registered routes and implements huma.Adapter (Handle + ServeHTTP).
// Use Middleware to plug the adapter into a *gofr.App.
type Adapter struct {
	app    *gofr.App
	mux    *http.ServeMux
	prefix string
}

// check that *Adapter implements huma.Adapter
var _ huma.Adapter = (*Adapter)(nil)

// Handle registers a Huma operation on the internal mux using the Go 1.22+
// "METHOD /path" pattern syntax, which supports {name} path parameters read
// back via (*gofrContext).Param through http.Request.PathValue. When the
// adapter is bound to a GoFr app it also registers a placeholder GoFr route so
// the operation participates in GoFr's lifecycle (see registerGofrRoute).
func (a *Adapter) Handle(op *huma.Operation, handler func(huma.Context)) {
	a.mux.HandleFunc(strings.ToUpper(op.Method)+" "+a.prefix+op.Path, func(w http.ResponseWriter, r *http.Request) {
		handler(&gofrContext{op: op, r: r, w: w})
	})
	a.registerGofrRoute(op.Method, a.prefix+op.Path)
}

// registerGofrRoute registers a no-op route on the GoFr app for method+path.
// This is required for two reasons:
//
//  1. GoFr only starts its HTTP server once at least one HTTP route is
//     registered (it stays down for CMD/gRPC-only apps). Without this, an
//     API wired purely through Middleware would never come up.
//  2. It makes the operation show up in GoFr's startup route listing and in
//     its per-request access logs — the observability GoFr is chosen for.
//
// The placeholder handler never actually runs: Middleware intercepts the
// request and serves it from the huma mux before GoFr's handler is reached.
// GoFr only exposes GET/POST/PUT/DELETE/PATCH; operations using any other
// method are still served (via Middleware + GoFr's catch-all) but are not
// listed by GoFr.
func (a *Adapter) registerGofrRoute(method, path string) {
	if a.app == nil {
		return
	}

	noop := func(*gofr.Context) (any, error) { return nil, nil }

	switch strings.ToUpper(method) {
	case http.MethodGet:
		a.app.GET(path, noop)
	case http.MethodPost:
		a.app.POST(path, noop)
	case http.MethodPut:
		a.app.PUT(path, noop)
	case http.MethodDelete:
		a.app.DELETE(path, noop)
	case http.MethodPatch:
		a.app.PATCH(path, noop)
	}
}

// ServeHTTP serves a request directly from the Huma route table. This is what
// huma.API and humatest use; it does not touch GoFr.
func (a *Adapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mux.ServeHTTP(w, r)
}

// Middleware returns a GoFr middleware that forwards Huma-owned requests to the
// internal mux and passes every other request through to the next handler
// (GoFr's own router). Register it with app.UseMiddleware.
func (a *Adapter) Middleware() gofrHTTP.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// A non-empty pattern means the internal mux has a matching route
			// for this method+path, so Huma owns it.
			if _, pattern := a.mux.Handler(r); pattern != "" {
				a.mux.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// NewAdapter creates a standalone Huma adapter backed by its own mux. The
// prefix is prepended to every registered route path (but not to the OpenAPI);
// pass "" for no prefix. The returned adapter is not yet wired into any GoFr
// app — call Adapter.Middleware and app.UseMiddleware to do that, or use New.
func NewAdapter(prefix string) *Adapter {
	return &Adapter{mux: http.NewServeMux(), prefix: prefix}
}

// New creates a new Huma API and wires it into the given GoFr app.
//
//	app := gofr.New()
//	api := humagofr.New(app, huma.DefaultConfig("My API", "1.0.0"))
//	huma.Get(api, "/greet", greetHandler)
//	app.Run()
func New(app *gofr.App, config huma.Config) huma.API {
	return NewWithPrefix(app, "", config)
}

// NewWithPrefix creates a new Huma API wired into the given GoFr app, prepending
// prefix to each route path (but not to the OpenAPI). This mirrors a router's
// group functionality and should be combined with config.Servers base paths so
// the generated OpenAPI URLs stay correct.
//
//	app := gofr.New()
//	config := huma.DefaultConfig("My API", "1.0.0")
//	config.Servers = []*huma.Server{{URL: "http://example.com/api"}}
//	api := humagofr.NewWithPrefix(app, "/api", config)
func NewWithPrefix(app *gofr.App, prefix string, config huma.Config) huma.API {
	if prefix != "" && len(config.Servers) == 0 {
		config.Servers = append(config.Servers, &huma.Server{URL: prefix})
	}
	adapter := &Adapter{app: app, mux: http.NewServeMux(), prefix: prefix}
	app.UseMiddleware(adapter.Middleware())
	return huma.NewAPI(config, adapter)
}
