package interpreter

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"sync"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

func init() { registry.add(EngineYaegi) }

// yaegiRuntime wraps a Yaegi Go interpreter.
//
// Bind supports only primitive values (string, number, bool). Functions and complex
// values must be defined inside the evaluated Go code. Users reference bindings directly:
//
//	result := Offset + 5
//
// The runtime serializes access with a mutex because a single Yaegi interpreter is not
// safe for concurrent use.
type yaegiRuntime struct {
	mu       sync.Mutex
	i        *interp.Interpreter
	bindings map[string]any
}

func newYaegiRuntime() (Runtime, error) {
	i := interp.New(interp.Options{})
	i.Use(stdlib.Symbols)
	i.Use(interp.Symbols)
	return &yaegiRuntime{
		i:        i,
		bindings: make(map[string]any),
	}, nil
}

func (r *yaegiRuntime) Name() string { return EngineYaegi }

func (r *yaegiRuntime) Bind(name string, value any) error {
	if name == "" {
		return fmt.Errorf("interpreter/yaegi: binding name cannot be empty")
	}
	if !isYaegiPrimitive(value) {
		return fmt.Errorf("interpreter/yaegi: Bind %q is %w; only string/number/bool values are supported", name, ErrUnsupported)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	lit, err := yaegiLiteral(value)
	if err != nil {
		return err
	}
	src := fmt.Sprintf("var %s = %s", name, lit)
	if _, err := r.i.Eval(src); err != nil {
		return fmt.Errorf("interpreter/yaegi: bind %q: %w", name, err)
	}
	r.bindings[name] = value
	return nil
}

func (r *yaegiRuntime) Eval(ctx context.Context, expr string) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	v, err := r.i.Eval(expr)
	if err != nil {
		return nil, fmt.Errorf("interpreter/yaegi: eval: %w", err)
	}
	if !v.IsValid() {
		return nil, nil
	}
	return exportYaegiValue(v), nil
}

func (r *yaegiRuntime) Call(ctx context.Context, fn string, args ...any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	v, err := r.i.Eval(fn)
	if err != nil {
		return nil, fmt.Errorf("interpreter/yaegi: resolve %q: %w", fn, err)
	}
	if !v.IsValid() || v.Kind() != reflect.Func {
		return nil, fmt.Errorf("interpreter/yaegi: %q is not a function: %w", fn, ErrNotFound)
	}
	in := make([]reflect.Value, len(args))
	for i, a := range args {
		in[i] = reflect.ValueOf(a)
	}
	out := v.Call(in)
	if len(out) == 0 {
		return nil, nil
	}
	return exportYaegiValue(out[0]), nil
}

func (r *yaegiRuntime) Close() error { return nil }

func isYaegiPrimitive(v any) bool {
	switch v.(type) {
	case string, bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return true
	}
	return false
}

func yaegiLiteral(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return strconv.Quote(x), nil
	case bool:
		return strconv.FormatBool(x), nil
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", x), nil
	case float32:
		return strconv.FormatFloat(float64(x), 'g', -1, 32), nil
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64), nil
	}
	return "", fmt.Errorf("interpreter/yaegi: unsupported literal type %T", v)
}

func exportYaegiValue(v reflect.Value) any {
	if !v.IsValid() {
		return nil
	}
	switch v.Kind() {
	case reflect.Interface, reflect.Ptr:
		if v.IsNil() {
			return nil
		}
		return v.Elem().Interface()
	}
	if v.CanInterface() {
		return v.Interface()
	}
	return nil
}
