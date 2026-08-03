package interpreter

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/gopher-lua"
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

func TestLua_CallRuntimeErrorAndNilValues(t *testing.T) {
	rt, err := New(EngineLua)
	require.NoError(t, err)
	defer rt.Close()
	require.NoError(t, rt.Bind("raw", lua.LString("value")))
	value, err := rt.Eval(t.Context(), "return raw")
	require.NoError(t, err)
	assert.Equal(t, "value", value)
	value, err = rt.Eval(t.Context(), "return nil")
	require.NoError(t, err)
	assert.Nil(t, value)
	_, err = rt.Eval(t.Context(), `function explode() error("failed") end`)
	require.NoError(t, err)
	_, err = rt.Call(t.Context(), "explode")
	assert.ErrorContains(t, err, "call")
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

func TestLua_CallBoundGoFunc_Variadic(t *testing.T) {
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
	value, err := rt.Eval(context.Background(), `return sum(1, 2, 3, 4)`)
	require.NoError(t, err)
	assert.Equal(t, int64(10), value)
}

func TestLua_VariadicValidationAndReturnConversion(t *testing.T) {
	rt, err := New(EngineLua,
		WithBinding("join", func(prefix string, values ...int) string { return prefix }),
		WithBinding("bad_return", func() chan int { return make(chan int) }),
		WithBinding("identity", func(value any) any { return value }),
	)
	require.NoError(t, err)
	defer rt.Close()

	_, err = rt.Eval(t.Context(), `return join()`)
	assert.Error(t, err)
	_, err = rt.Eval(t.Context(), `return join(true, 1)`)
	assert.Error(t, err)
	_, err = rt.Eval(t.Context(), `return join("ok", "bad")`)
	assert.Error(t, err)
	_, err = rt.Eval(t.Context(), `return bad_return()`)
	assert.ErrorContains(t, err, "return value")
	value, err := rt.Eval(t.Context(), `return identity("value")`)
	require.NoError(t, err)
	assert.Equal(t, "value", value)
}

func TestLuaConversionBoundaries(t *testing.T) {
	float32Type := reflect.TypeOf(float32(0))
	value, err := convertToType(reflect.TypeOf("1.25"), float32Type, "1.25")
	require.NoError(t, err)
	assert.Equal(t, float32(1.25), value.Interface())
	_, err = convertToType(reflect.TypeOf(struct{}{}), float32Type, struct{}{})
	assert.Error(t, err)
	_, err = convertToType(reflect.TypeOf(1), reflect.TypeOf(""), 1)
	assert.Error(t, err)
	_, err = convertToType(reflect.TypeOf(1), reflect.TypeOf(false), 1)
	assert.Error(t, err)
	_, err = convertToType(reflect.TypeOf(1), reflect.TypeOf((*int)(nil)), 1)
	assert.Error(t, err)

	for input, expected := range map[any]float64{
		int(1): 1, float32(2.5): 2.5, uint(3): 3, uint64(4): 4, "5.5": 5.5,
	} {
		converted, ok := toFloat64(input)
		assert.True(t, ok)
		assert.Equal(t, expected, converted)
	}
	_, ok := toFloat64("not-a-number")
	assert.False(t, ok)
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

func TestLuaBoundFunctionWithNoReturn(t *testing.T) {
	called := false
	rt, err := New(EngineLua, WithBinding("notify", func(value string) { called = value == "done" }))
	require.NoError(t, err)
	defer rt.Close()
	value, err := rt.Eval(context.Background(), `notify("done")`)
	require.NoError(t, err)
	assert.Nil(t, value)
	assert.True(t, called)
}

func TestLuaBoundFunctionConversionFailures(t *testing.T) {
	rt, err := New(EngineLua,
		WithBinding("positive", func(value uint) uint { return value }),
		WithBinding("truth", func(value bool) bool { return value }),
		WithBinding("text", func(value string) string { return value }),
		WithBinding("unsupportedReturn", func() any { return make(chan int) }),
	)
	require.NoError(t, err)
	defer rt.Close()
	for _, expression := range []string{
		`return positive(-1)`, `return truth("true")`, `return text(1)`, `return unsupportedReturn()`,
	} {
		_, err := rt.Eval(context.Background(), expression)
		assert.Error(t, err, expression)
	}
	_, err = rt.Call(context.Background(), "truth", make(chan int))
	assert.Error(t, err)
}
