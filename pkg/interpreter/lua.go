package interpreter

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	lua "github.com/yuin/gopher-lua"
)

func init() { registry.add(EngineLua) }

// luaRuntime wraps a Gopher-Lua state. A single LState is not goroutine-safe, so all
// operations are protected by a mutex.
//
// Bind supports primitive values (string, number, bool) and Go functions. Go functions
// should preferably have the signature func(...any) any or func(any) any; the arguments
// are converted from Lua values (string, number, bool) and the first return value is
// pushed back to Lua.
type luaRuntime struct {
	mu sync.Mutex
	L  *lua.LState
}

func newLuaRuntime() (Runtime, error) {
	return &luaRuntime{L: lua.NewState()}, nil
}

func (r *luaRuntime) Name() string { return EngineLua }

func (r *luaRuntime) Bind(name string, value any) error {
	if name == "" {
		return fmt.Errorf("interpreter/lua: binding name cannot be empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	lv, err := goValueToLua(r.L, value)
	if err != nil {
		return fmt.Errorf("interpreter/lua: bind %q: %w", name, err)
	}
	r.L.SetGlobal(name, lv)
	return nil
}

func (r *luaRuntime) Eval(ctx context.Context, expr string) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.L.DoString(expr); err != nil {
		return nil, fmt.Errorf("interpreter/lua: eval: %w", err)
	}
	ret := r.L.Get(-1)
	r.L.Pop(1)
	return luaValueToGo(ret), nil
}

func (r *luaRuntime) Call(ctx context.Context, fn string, args ...any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	fnValue := r.L.GetGlobal(fn)
	if fnValue.Type() == lua.LTNil {
		return nil, fmt.Errorf("interpreter/lua: %q is not defined: %w", fn, ErrNotFound)
	}
	largs := make([]lua.LValue, len(args))
	for i, a := range args {
		lv, err := goValueToLua(r.L, a)
		if err != nil {
			return nil, fmt.Errorf("interpreter/lua: arg %d: %w", i, err)
		}
		largs[i] = lv
	}
	if err := r.L.CallByParam(lua.P{
		Fn:      fnValue,
		NRet:    1,
		Protect: true,
	}, largs...); err != nil {
		return nil, fmt.Errorf("interpreter/lua: call %q: %w", fn, err)
	}
	ret := r.L.Get(-1)
	r.L.Pop(1)
	return luaValueToGo(ret), nil
}

func (r *luaRuntime) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.L.Close()
	return nil
}

func goValueToLua(L *lua.LState, v any) (lua.LValue, error) {
	if v == nil {
		return lua.LNil, nil
	}
	switch x := v.(type) {
	case bool:
		return lua.LBool(x), nil
	case string:
		return lua.LString(x), nil
	case int:
		return lua.LNumber(x), nil
	case int8:
		return lua.LNumber(x), nil
	case int16:
		return lua.LNumber(x), nil
	case int32:
		return lua.LNumber(x), nil
	case int64:
		return lua.LNumber(x), nil
	case uint:
		return lua.LNumber(x), nil
	case uint8:
		return lua.LNumber(x), nil
	case uint16:
		return lua.LNumber(x), nil
	case uint32:
		return lua.LNumber(x), nil
	case uint64:
		return lua.LNumber(x), nil
	case float32:
		return lua.LNumber(x), nil
	case float64:
		return lua.LNumber(x), nil
	case lua.LValue:
		return x, nil
	}

	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Func {
		return luaBindGoFunction(L, rv)
	}
	return nil, fmt.Errorf("unsupported Go type %T for Lua binding", v)
}

func luaValueToGo(v lua.LValue) any {
	switch x := v.(type) {
	case *lua.LNilType:
		return nil
	case lua.LBool:
		return bool(x)
	case lua.LString:
		return string(x)
	case lua.LNumber:
		f := float64(x)
		if f == float64(int64(f)) {
			return int64(f)
		}
		return f
	}
	return v
}

// luaBindGoFunction wraps a Go function so it can be called from Lua. Arguments are
// converted to the target parameter types where possible; for variadic func(...any) any
// the Lua arguments are passed as a slice of any.
func luaBindGoFunction(L *lua.LState, rv reflect.Value) (lua.LValue, error) {
	ft := rv.Type()
	if ft.NumOut() > 1 {
		return nil, fmt.Errorf("Lua-bound Go function may return at most one value")
	}
	return L.NewFunction(func(L *lua.LState) int {
		n := L.GetTop()
		args, err := luaBuildCallArgs(L, rv, n)
		if err != nil {
			L.RaiseError("%s", err.Error())
			return 0
		}
		out := rv.Call(args)
		if len(out) > 0 {
			lv, err := goValueToLua(L, out[0].Interface())
			if err != nil {
				L.RaiseError("return value: %s", err.Error())
				return 0
			}
			L.Push(lv)
			return 1
		}
		return 0
	}), nil
}

func luaBuildCallArgs(L *lua.LState, rv reflect.Value, n int) ([]reflect.Value, error) {
	ft := rv.Type()
	isVariadic := ft.IsVariadic()
	numIn := ft.NumIn()

	if !isVariadic {
		if n != numIn {
			return nil, fmt.Errorf("expected %d args, got %d", numIn, n)
		}
		args := make([]reflect.Value, numIn)
		for i := 0; i < numIn; i++ {
			gv := luaValueToGo(L.Get(i + 1))
			cv, err := convertToType(reflect.TypeOf(gv), ft.In(i), gv)
			if err != nil {
				return nil, fmt.Errorf("arg %d: %w", i+1, err)
			}
			args[i] = cv
		}
		return args, nil
	}

	if numIn == 0 {
		return nil, fmt.Errorf("variadic function with no params is not supported")
	}
	if n < numIn-1 {
		return nil, fmt.Errorf("expected at least %d args, got %d", numIn-1, n)
	}

	args := make([]reflect.Value, numIn)
	for i := 0; i < numIn-1; i++ {
		gv := luaValueToGo(L.Get(i + 1))
		cv, err := convertToType(reflect.TypeOf(gv), ft.In(i), gv)
		if err != nil {
			return nil, fmt.Errorf("arg %d: %w", i+1, err)
		}
		args[i] = cv
	}

	variadic := ft.In(numIn - 1).Elem()
	extra := make([]reflect.Value, n-(numIn-1))
	for i := numIn - 1; i < n; i++ {
		gv := luaValueToGo(L.Get(i + 1))
		cv, err := convertToType(reflect.TypeOf(gv), variadic, gv)
		if err != nil {
			return nil, fmt.Errorf("arg %d: %w", i+1, err)
		}
		extra[i-(numIn-1)] = cv
	}
	args[numIn-1] = reflect.AppendSlice(reflect.MakeSlice(ft.In(numIn-1), 0, len(extra)), reflect.ValueOf(extra))
	return args, nil
}

func convertToType(src reflect.Type, dst reflect.Type, v any) (reflect.Value, error) {
	if dst == src || dst == reflect.TypeOf(v) {
		return reflect.ValueOf(v), nil
	}
	switch dst.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, ok := toFloat64(v)
		if !ok {
			return reflect.Value{}, fmt.Errorf("cannot convert %T to %s", v, dst)
		}
		return reflect.ValueOf(int64(n)).Convert(dst), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, ok := toFloat64(v)
		if !ok || n < 0 {
			return reflect.Value{}, fmt.Errorf("cannot convert %T to %s", v, dst)
		}
		return reflect.ValueOf(uint64(n)).Convert(dst), nil
	case reflect.Float32, reflect.Float64:
		n, ok := toFloat64(v)
		if !ok {
			return reflect.Value{}, fmt.Errorf("cannot convert %T to %s", v, dst)
		}
		return reflect.ValueOf(n).Convert(dst), nil
	case reflect.String:
		s, ok := v.(string)
		if !ok {
			return reflect.Value{}, fmt.Errorf("cannot convert %T to string", v)
		}
		return reflect.ValueOf(s), nil
	case reflect.Bool:
		b, ok := v.(bool)
		if !ok {
			return reflect.Value{}, fmt.Errorf("cannot convert %T to bool", v)
		}
		return reflect.ValueOf(b), nil
	case reflect.Interface:
		return reflect.ValueOf(v), nil
	}
	return reflect.Value{}, fmt.Errorf("cannot convert %T to %s", v, dst)
}

func toFloat64(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case uint:
		return float64(x), true
	case uint64:
		return float64(x), true
	case string:
		var f float64
		if _, err := fmt.Sscanf(x, "%f", &f); err == nil {
			return f, true
		}
	}
	return 0, false
}
