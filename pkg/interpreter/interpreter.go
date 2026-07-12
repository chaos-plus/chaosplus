// Package interpreter provides a unified runtime facade over multiple dynamic-language
// engines. It is intentionally thin: each engine keeps its own semantics, but callers can
// instantiate, bind values, evaluate scripts, and call functions through one interface.
package interpreter

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Engine names accepted by New.
const (
	EngineYaegi = "yaegi"
	EngineGoja  = "goja"
	EngineLua   = "lua"
	EngineWasm  = "wasm"
)

// Runtime is the common abstraction over every interpreter engine. Implementations must
// be safe for concurrent use where noted; most script engines serialize access through
// a per-runtime lock because the underlying VMs are not goroutine-safe.
type Runtime interface {
	// Name returns the engine identifier (one of the Engine* constants).
	Name() string

	// Eval executes a script fragment in the engine's language. For Yaegi and Goja the
	// returned value is the last evaluated expression; for Lua it is the first return
	// value of the chunk; for Wasm, expr is ignored and the method validates that a
	// module has been loaded (use Call for exported functions).
	Eval(ctx context.Context, expr string) (any, error)

	// Call invokes a named function that exists in the runtime. Args are converted to
	// engine-native values by each implementation. Wasm exported functions accept i32,
	// i64, f32 and f64 arguments.
	Call(ctx context.Context, fn string, args ...any) (any, error)

	// Bind injects a Go value or function into the runtime under the given name.
	// Engine-specific limitations apply (documented on each implementation).
	Bind(name string, value any) error

	// Close releases engine resources. A closed runtime must not be reused.
	Close() error
}

// ErrUnsupported is returned by engines for operations they cannot fulfil, e.g. binding
// host functions into a Wasm runtime via the generic Bind API.
var ErrUnsupported = errors.New("interpreter: unsupported operation for this engine")

// ErrNotFound is returned when a requested function or symbol does not exist.
var ErrNotFound = errors.New("interpreter: function or symbol not found")

// config holds the options passed to New.
type config struct {
	wasmBytes []byte
	bindings  map[string]any
}

// Option configures a Runtime created by New.
type Option func(*config)

// WithWASM provides the WebAssembly module bytes required by EngineWasm.
func WithWASM(b []byte) Option {
	return func(c *config) { c.wasmBytes = b }
}

// WithBinding pre-binds a Go value into the runtime before Eval/Call are used. It is
// equivalent to calling Bind after construction.
func WithBinding(name string, value any) Option {
	return func(c *config) {
		if c.bindings == nil {
			c.bindings = make(map[string]any)
		}
		c.bindings[name] = value
	}
}

// New creates a Runtime for the named engine and applies options.
func New(engine string, opts ...Option) (Runtime, error) {
	cfg := &config{}
	for _, o := range opts {
		if o == nil {
			continue
		}
		o(cfg)
	}

	var rt Runtime
	var err error
	switch engine {
	case EngineYaegi:
		rt, err = newYaegiRuntime()
	case EngineGoja:
		rt, err = newGojaRuntime()
	case EngineLua:
		rt, err = newLuaRuntime()
	case EngineWasm:
		rt, err = newWasmRuntime(cfg.wasmBytes)
	default:
		return nil, fmt.Errorf("interpreter: unknown engine %q (want %s, %s, %s or %s)",
			engine, EngineYaegi, EngineGoja, EngineLua, EngineWasm)
	}
	if err != nil {
		return nil, err
	}

	for n, v := range cfg.bindings {
		if err := rt.Bind(n, v); err != nil {
			_ = rt.Close()
			return nil, fmt.Errorf("interpreter: bind %q: %w", n, err)
		}
	}
	return rt, nil
}

// engineRegistry is a tiny registry used by tests and diagnostics to list supported
// engines. It is populated by init functions in the engine-specific files.
type engineRegistry struct {
	mu    sync.RWMutex
	names []string
}

func (r *engineRegistry) add(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, n := range r.names {
		if n == name {
			return
		}
	}
	r.names = append(r.names, name)
}

var registry engineRegistry

// Engines returns the list of supported engine names.
func Engines() []string {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	out := make([]string, len(registry.names))
	copy(out, registry.names)
	return out
}
