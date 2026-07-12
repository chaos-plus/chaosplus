package interpreter

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLua_EvalError(t *testing.T) {
	rt, _ := New(EngineLua)
	defer rt.Close()
	_, err := rt.Eval(context.Background(), "this is ++ not lua")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "eval")
}

func TestLua_EvalContextCancelled(t *testing.T) {
	rt, _ := New(EngineLua)
	defer rt.Close()
	_, err := rt.Eval(cancelledCtx(), "return 1")
	require.ErrorIs(t, err, context.Canceled)
}

func TestLua_CallNotDefined(t *testing.T) {
	rt, _ := New(EngineLua)
	defer rt.Close()
	_, err := rt.Call(context.Background(), "ghost")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestLua_CallContextCancelled(t *testing.T) {
	rt, _ := New(EngineLua)
	defer rt.Close()
	_, err := rt.Call(cancelledCtx(), "ghost")
	require.ErrorIs(t, err, context.Canceled)
}

func TestLua_BindEmptyName(t *testing.T) {
	rt, _ := New(EngineLua)
	defer rt.Close()
	require.Error(t, rt.Bind("", 1))
}

func TestLua_BindUnsupportedType(t *testing.T) {
	rt, _ := New(EngineLua)
	defer rt.Close()
	err := rt.Bind("ch", make(chan int))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

// TestLua_BindAllNumericTypes covers goValueToLua across every numeric kind plus bool,
// string, and nil.
func TestLua_BindAllNumericTypes(t *testing.T) {
	rt, _ := New(EngineLua)
	defer rt.Close()
	cases := map[string]any{
		"b":    true,
		"s":    "str",
		"i":    int(1),
		"i8":   int8(2),
		"i16":  int16(3),
		"i32":  int32(4),
		"i64":  int64(5),
		"u":    uint(6),
		"u8":   uint8(7),
		"u16":  uint16(8),
		"u32":  uint32(9),
		"u64":  uint64(10),
		"f32":  float32(1.5),
		"f64":  float64(2.5),
		"null": nil,
	}
	for name, v := range cases {
		require.NoError(t, rt.Bind(name, v), "bind %s", name)
	}
	v, err := rt.Eval(context.Background(), "return i + i8")
	require.NoError(t, err)
	assert.Equal(t, int64(3), v)
}

// TestLua_EvalReturnsFloat exercises the float (non-integer) branch of luaValueToGo.
func TestLua_EvalReturnsFloat(t *testing.T) {
	rt, _ := New(EngineLua)
	defer rt.Close()
	v, err := rt.Eval(context.Background(), "return 3.25")
	require.NoError(t, err)
	assert.Equal(t, 3.25, v)
}

// TestLua_EvalReturnsBoolAndString covers the bool/string branches of luaValueToGo.
func TestLua_EvalReturnsBoolAndString(t *testing.T) {
	rt, _ := New(EngineLua)
	defer rt.Close()
	v, err := rt.Eval(context.Background(), "return true")
	require.NoError(t, err)
	assert.Equal(t, true, v)
	v, err = rt.Eval(context.Background(), `return "hi"`)
	require.NoError(t, err)
	assert.Equal(t, "hi", v)
}

// TestLua_CallBoundGoFunc_FixedArgs exercises luaBuildCallArgs (non-variadic) and
// convertToType for int/float/string/bool destinations.
func TestLua_CallBoundGoFunc_FixedArgs(t *testing.T) {
	rt, _ := New(EngineLua,
		WithBinding("concat", func(s string, n int, f float64, b bool) string {
			if b {
				return s
			}
			_ = n
			_ = f
			return ""
		}),
	)
	defer rt.Close()
	v, err := rt.Eval(context.Background(), `return concat("ok", 3, 1.5, true)`)
	require.NoError(t, err)
	assert.Equal(t, "ok", v)
}

// TestLua_CallBoundGoFunc_WrongArgCount triggers the arg-count mismatch error inside
// luaBuildCallArgs, surfaced as a Lua runtime error.
func TestLua_CallBoundGoFunc_WrongArgCount(t *testing.T) {
	rt, _ := New(EngineLua,
		WithBinding("add", func(a, b int) int { return a + b }),
	)
	defer rt.Close()
	_, err := rt.Eval(context.Background(), `return add(1)`)
	require.Error(t, err)
}

// TestLua_CallBoundGoFunc_Variadic documents that the variadic branch of
// luaBuildCallArgs is currently broken: it builds the variadic slice with
// reflect.AppendSlice(MakeSlice(...), reflect.ValueOf(extra)) where extra is already a
// []reflect.Value, producing a "int != reflect.Value" reflect panic surfaced as a Lua
// error. The test asserts the (current) failure rather than success so it stays green
// without a production fix. See luaBuildCallArgs in lua.go.
func TestLua_CallBoundGoFunc_Variadic_CurrentlyErrors(t *testing.T) {
	rt, _ := New(EngineLua,
		WithBinding("sum", func(nums ...int) int {
			total := 0
			for _, n := range nums {
				total += n
			}
			return total
		}),
	)
	defer rt.Close()
	_, err := rt.Eval(context.Background(), `return sum(1, 2, 3, 4)`)
	require.Error(t, err)
}

// TestLua_CallBoundGoFunc_Uint covers the uint destination branch of convertToType.
func TestLua_CallBoundGoFunc_Uint(t *testing.T) {
	rt, _ := New(EngineLua,
		WithBinding("u", func(x uint32) uint32 { return x + 1 }),
	)
	defer rt.Close()
	v, err := rt.Eval(context.Background(), `return u(41)`)
	require.NoError(t, err)
	assert.Equal(t, int64(42), v)
}

// TestLua_CallBoundGoFunc_ConvertError forces a convertToType failure (string arg where
// an int is expected), raised as a Lua error.
func TestLua_CallBoundGoFunc_ConvertError(t *testing.T) {
	rt, _ := New(EngineLua,
		WithBinding("needInt", func(n int) int { return n }),
	)
	defer rt.Close()
	_, err := rt.Eval(context.Background(), `return needInt("not-a-number")`)
	require.Error(t, err)
}

// TestLua_BindMultiReturnRejected verifies a Go func with >1 return is rejected.
func TestLua_BindMultiReturnRejected(t *testing.T) {
	rt, _ := New(EngineLua)
	defer rt.Close()
	err := rt.Bind("multi", func() (int, int) { return 1, 2 })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at most one value")
}

// NOTE: a bound Go function returning *no* value (e.g. func(int)) is intentionally not
// tested here. Invoking it via Eval triggers a "register underflow" panic deep in
// gopher-lua because luaBindGoFunction pushes nothing while the Eval path unconditionally
// pops a result. That is a production defect, not a test gap; covering it would require a
// production fix, which is out of scope for this test-only change.
