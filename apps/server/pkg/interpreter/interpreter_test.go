package interpreter

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Minimal hand-crafted WebAssembly module exporting an "add" function:
//
//	(func (export "add") (param i32 i32) (result i32) local.get 0 local.get 1 i32.add)
var testWasmAdd = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
	0x01, 0x07, 0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f,
	0x03, 0x02, 0x01, 0x00,
	0x07, 0x07, 0x01, 0x03, 0x61, 0x64, 0x64, 0x00, 0x00,
	0x0a, 0x09, 0x01, 0x07, 0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b,
}

func TestNew_UnknownEngine(t *testing.T) {
	_, err := New("python")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown engine")
}

func TestEngines(t *testing.T) {
	names := Engines()
	require.Subset(t, names, []string{EngineYaegi, EngineGoja, EngineLua, EngineWasm})
}

func TestYaegi_EvalAndCall(t *testing.T) {
	rt, err := New(EngineYaegi, WithBinding("Offset", 10))
	require.NoError(t, err)
	defer rt.Close()

	v, err := rt.Eval(context.Background(), `Offset + 5`)
	require.NoError(t, err)
	assert.Equal(t, 15, v)

	_, err = rt.Eval(context.Background(), `func Add(a, b int) int { return a + b }`)
	require.NoError(t, err)

	v, err = rt.Call(context.Background(), "Add", 3, 4)
	require.NoError(t, err)
	assert.Equal(t, 7, v)
}

func TestGoja_EvalAndCall(t *testing.T) {
	rt, err := New(EngineGoja,
		WithBinding("offset", 10),
		WithBinding("add", func(a, b int) int { return a + b }),
	)
	require.NoError(t, err)
	defer rt.Close()

	v, err := rt.Eval(context.Background(), `offset + 5`)
	require.NoError(t, err)
	assert.InDelta(t, float64(15), v, 0)

	v, err = rt.Eval(context.Background(), `add(3, 4)`)
	require.NoError(t, err)
	assert.InDelta(t, float64(7), v, 0)

	v, err = rt.Call(context.Background(), "add", 5, 6)
	require.NoError(t, err)
	assert.InDelta(t, float64(11), v, 0)
}

func TestLua_EvalAndCall(t *testing.T) {
	rt, err := New(EngineLua,
		WithBinding("offset", 10),
		WithBinding("add", func(a, b int) int { return a + b }),
	)
	require.NoError(t, err)
	defer rt.Close()

	v, err := rt.Eval(context.Background(), `return offset + 5`)
	require.NoError(t, err)
	assert.Equal(t, int64(15), v)

	v, err = rt.Call(context.Background(), "add", 3, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(7), v)
}

func TestWasm_Call(t *testing.T) {
	rt, err := New(EngineWasm, WithWASM(testWasmAdd))
	require.NoError(t, err)
	defer rt.Close()

	_, err = rt.Eval(context.Background(), "")
	require.NoError(t, err)

	v, err := rt.Call(context.Background(), "add", 3, 4)
	require.NoError(t, err)
	assert.Equal(t, int32(7), v)
}

func TestWasm_MissingModule(t *testing.T) {
	_, err := New(EngineWasm)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "WithWASM")
}

func TestWasm_FunctionNotFound(t *testing.T) {
	rt, err := New(EngineWasm, WithWASM(testWasmAdd))
	require.NoError(t, err)
	defer rt.Close()

	_, err = rt.Call(context.Background(), "missing")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestBind_EmptyName(t *testing.T) {
	engines := []string{EngineYaegi, EngineGoja, EngineLua}
	for _, e := range engines {
		rt, err := New(e)
		require.NoError(t, err)
		err = rt.Bind("", 1)
		assert.Error(t, err, "engine %s should reject empty name", e)
		rt.Close()
	}
}

func TestWasm_BindUnsupported(t *testing.T) {
	rt, err := New(EngineWasm, WithWASM(testWasmAdd))
	require.NoError(t, err)
	defer rt.Close()

	err = rt.Bind("hostFn", func() {})
	assert.ErrorIs(t, err, ErrUnsupported)
}
