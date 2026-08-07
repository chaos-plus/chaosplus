package interpreter

import (
	"context"
	"fmt"
	"sync"

	"github.com/dop251/goja"
)

func init() { registry.add(EngineGoja) }

// gojaRuntime wraps a Goja JavaScript interpreter. A single Goja runtime is not
// goroutine-safe, so all operations are protected by a mutex.
type gojaRuntime struct {
	mu sync.Mutex
	vm *goja.Runtime
}

func newGojaRuntime() (Runtime, error) {
	return &gojaRuntime{vm: goja.New()}, nil
}

func (r *gojaRuntime) Name() string { return EngineGoja }

func (r *gojaRuntime) Bind(name string, value any) error {
	if name == "" {
		return fmt.Errorf("interpreter/goja: binding name cannot be empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.vm.Set(name, value)
}

func (r *gojaRuntime) Eval(ctx context.Context, expr string) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	v, err := r.vm.RunString(expr)
	if err != nil {
		return nil, fmt.Errorf("interpreter/goja: eval: %w", err)
	}
	return v.Export(), nil
}

func (r *gojaRuntime) Call(ctx context.Context, fn string, args ...any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	callable, ok := goja.AssertFunction(r.vm.Get(fn))
	if !ok {
		return nil, fmt.Errorf("interpreter/goja: %q is not a function: %w", fn, ErrNotFound)
	}
	values := make([]goja.Value, len(args))
	for i, a := range args {
		values[i] = r.vm.ToValue(a)
	}
	res, err := callable(goja.Undefined(), values...)
	if err != nil {
		return nil, fmt.Errorf("interpreter/goja: call %q: %w", fn, err)
	}
	return res.Export(), nil
}

func (r *gojaRuntime) Close() error { return nil }
