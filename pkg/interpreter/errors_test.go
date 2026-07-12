package interpreter

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cancelledCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// --- New options & registry ---

func TestNew_NilOptionIgnored(t *testing.T) {
	rt, err := New(EngineGoja, nil, WithBinding("x", 1))
	require.NoError(t, err)
	defer rt.Close()
	v, err := rt.Eval(context.Background(), "x")
	require.NoError(t, err)
	assert.InDelta(t, float64(1), v, 0)
}

func TestNew_BindFailureClosesRuntime(t *testing.T) {
	// Yaegi rejects non-primitive bindings; New must surface the bind error.
	_, err := New(EngineYaegi, WithBinding("bad", []int{1, 2, 3}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bind")
}

func TestRegistry_AddDeduplicates(t *testing.T) {
	var r engineRegistry
	r.add("a")
	r.add("a")
	r.add("b")
	assert.Len(t, r.names, 2)
}

func TestNames_AllEngines(t *testing.T) {
	for _, e := range []string{EngineYaegi, EngineGoja, EngineLua} {
		rt, err := New(e)
		require.NoError(t, err)
		assert.Equal(t, e, rt.Name())
		rt.Close()
	}
	rt, err := New(EngineWasm, WithWASM(testWasmAdd))
	require.NoError(t, err)
	assert.Equal(t, EngineWasm, rt.Name())
	rt.Close()
}

// --- Goja error paths ---

func TestGoja_EvalError(t *testing.T) {
	rt, _ := New(EngineGoja)
	defer rt.Close()
	_, err := rt.Eval(context.Background(), "this is ) not valid js (")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "eval")
}

func TestGoja_EvalContextCancelled(t *testing.T) {
	rt, _ := New(EngineGoja)
	defer rt.Close()
	_, err := rt.Eval(cancelledCtx(), "1+1")
	require.ErrorIs(t, err, context.Canceled)
}

func TestGoja_CallNotFunction(t *testing.T) {
	rt, _ := New(EngineGoja)
	defer rt.Close()
	_, err := rt.Eval(context.Background(), "var notFn = 5")
	require.NoError(t, err)
	_, err = rt.Call(context.Background(), "notFn")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestGoja_CallContextCancelled(t *testing.T) {
	rt, _ := New(EngineGoja)
	defer rt.Close()
	_, err := rt.Call(cancelledCtx(), "anything")
	require.ErrorIs(t, err, context.Canceled)
}

func TestGoja_CallRuntimeError(t *testing.T) {
	rt, _ := New(EngineGoja)
	defer rt.Close()
	_, err := rt.Eval(context.Background(), `function boom(){ throw new Error("kaboom") }`)
	require.NoError(t, err)
	_, err = rt.Call(context.Background(), "boom")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kaboom")
}

// --- Yaegi error paths ---

func TestYaegi_EvalError(t *testing.T) {
	rt, _ := New(EngineYaegi)
	defer rt.Close()
	_, err := rt.Eval(context.Background(), "@@@invalid@@@")
	require.Error(t, err)
}

func TestYaegi_EvalContextCancelled(t *testing.T) {
	rt, _ := New(EngineYaegi)
	defer rt.Close()
	_, err := rt.Eval(cancelledCtx(), "1+1")
	require.ErrorIs(t, err, context.Canceled)
}

func TestYaegi_CallNotAFunction(t *testing.T) {
	rt, _ := New(EngineYaegi)
	defer rt.Close()
	_, err := rt.Eval(context.Background(), "var X = 10")
	require.NoError(t, err)
	_, err = rt.Call(context.Background(), "X")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestYaegi_CallResolveError(t *testing.T) {
	rt, _ := New(EngineYaegi)
	defer rt.Close()
	_, err := rt.Call(context.Background(), "undefinedSymbol")
	require.Error(t, err)
}

func TestYaegi_CallContextCancelled(t *testing.T) {
	rt, _ := New(EngineYaegi)
	defer rt.Close()
	_, err := rt.Call(cancelledCtx(), "anything")
	require.ErrorIs(t, err, context.Canceled)
}

func TestYaegi_BindNonPrimitive(t *testing.T) {
	rt, _ := New(EngineYaegi)
	defer rt.Close()
	err := rt.Bind("m", map[string]int{"a": 1})
	require.ErrorIs(t, err, ErrUnsupported)
}

func TestYaegi_BindAllPrimitiveTypes(t *testing.T) {
	rt, _ := New(EngineYaegi)
	defer rt.Close()
	cases := map[string]any{
		"sVal": "hello",
		"bVal": true,
		"iVal": int(7),
		"i64":  int64(8),
		"u32":  uint32(9),
		"f32":  float32(1.5),
		"f64":  float64(2.5),
	}
	for name, v := range cases {
		require.NoError(t, rt.Bind(name, v), "bind %s", name)
	}
	got, err := rt.Eval(context.Background(), "sVal")
	require.NoError(t, err)
	assert.Equal(t, "hello", got)
}

func TestYaegi_EvalInvalidReturnsNil(t *testing.T) {
	rt, _ := New(EngineYaegi)
	defer rt.Close()
	// A statement with no value yields an invalid reflect.Value -> nil.
	v, err := rt.Eval(context.Background(), "var z = 1")
	require.NoError(t, err)
	assert.Nil(t, v)
}

func TestYaegi_CallNoReturn(t *testing.T) {
	rt, _ := New(EngineYaegi)
	defer rt.Close()
	_, err := rt.Eval(context.Background(), "func Noop() {}")
	require.NoError(t, err)
	v, err := rt.Call(context.Background(), "Noop")
	require.NoError(t, err)
	assert.Nil(t, v)
}

func TestYaegi_BindEmptyName(t *testing.T) {
	rt, _ := New(EngineYaegi)
	defer rt.Close()
	require.Error(t, rt.Bind("", 1))
}
