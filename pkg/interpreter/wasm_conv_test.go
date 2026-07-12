package interpreter

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/wazero/api"
)

func TestWasm_EvalContextCancelled(t *testing.T) {
	rt, err := New(EngineWasm, WithWASM(testWasmAdd))
	require.NoError(t, err)
	defer rt.Close()
	_, err = rt.Eval(cancelledCtx(), "")
	require.ErrorIs(t, err, context.Canceled)
}

func TestWasm_CallContextCancelled(t *testing.T) {
	rt, err := New(EngineWasm, WithWASM(testWasmAdd))
	require.NoError(t, err)
	defer rt.Close()
	_, err = rt.Call(cancelledCtx(), "add", 1, 2)
	require.ErrorIs(t, err, context.Canceled)
}

func TestWasm_CallWrongArgCount(t *testing.T) {
	rt, err := New(EngineWasm, WithWASM(testWasmAdd))
	require.NoError(t, err)
	defer rt.Close()
	_, err = rt.Call(context.Background(), "add", 1) // add expects 2
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expects 2 args")
}

func TestWasm_CallArgEncodeError(t *testing.T) {
	rt, err := New(EngineWasm, WithWASM(testWasmAdd))
	require.NoError(t, err)
	defer rt.Close()
	// "add" expects i32 args; passing a non-numeric type fails encodeWasmValue.
	_, err = rt.Call(context.Background(), "add", "nope", 2)
	require.Error(t, err)
}

func TestWasm_NewRuntimeBadBytes(t *testing.T) {
	// Non-empty but invalid wasm bytes fail Instantiate.
	_, err := New(EngineWasm, WithWASM([]byte{0x01, 0x02, 0x03, 0x04}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "instantiate")
}

// --- white-box value conversion coverage ---

func TestEncodeWasmValue(t *testing.T) {
	tests := []struct {
		name    string
		t       api.ValueType
		v       any
		wantErr bool
	}{
		{"i32 ok", api.ValueTypeI32, int(5), false},
		{"i32 from int64", api.ValueTypeI32, int64(5), false},
		{"i32 bad", api.ValueTypeI32, "x", true},
		{"i64 ok", api.ValueTypeI64, int64(9), false},
		{"i64 bad", api.ValueTypeI64, struct{}{}, true},
		{"f32 ok", api.ValueTypeF32, float32(1.5), false},
		{"f32 bad", api.ValueTypeF32, "x", true},
		{"f64 ok", api.ValueTypeF64, float64(2.5), false},
		{"f64 bad", api.ValueTypeF64, []int{1}, true},
		{"unsupported type", api.ValueType(0x99), int(1), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := encodeWasmValue(tt.t, tt.v)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestDecodeWasmValue(t *testing.T) {
	assert.Equal(t, int32(7), decodeWasmValue(api.ValueTypeI32, api.EncodeI32(7)))
	assert.Equal(t, int64(8), decodeWasmValue(api.ValueTypeI64, 8))
	assert.InDelta(t, float32(1.5), decodeWasmValue(api.ValueTypeF32, api.EncodeF32(1.5)), 0.0001)
	assert.InDelta(t, float64(2.5), decodeWasmValue(api.ValueTypeF64, api.EncodeF64(2.5)), 0.0001)
	// unknown type returns the raw uint64
	assert.Equal(t, uint64(42), decodeWasmValue(api.ValueType(0x99), 42))
}

func TestDecodeWasmResults(t *testing.T) {
	// empty
	assert.Nil(t, decodeWasmResults(nil, nil))
	// single
	got := decodeWasmResults([]api.ValueType{api.ValueTypeI32}, []uint64{api.EncodeI32(3)})
	assert.Equal(t, int32(3), got)
	// multiple
	multi := decodeWasmResults(
		[]api.ValueType{api.ValueTypeI32, api.ValueTypeI64},
		[]uint64{api.EncodeI32(1), 2},
	)
	arr, ok := multi.([]any)
	require.True(t, ok)
	assert.Equal(t, int32(1), arr[0])
	assert.Equal(t, int64(2), arr[1])
}

func TestToInt64(t *testing.T) {
	tests := []struct {
		v    any
		want int64
		ok   bool
	}{
		{int(1), 1, true},
		{int8(2), 2, true},
		{int16(3), 3, true},
		{int32(4), 4, true},
		{int64(5), 5, true},
		{uint(6), 6, true},
		{uint8(7), 7, true},
		{uint16(8), 8, true},
		{uint32(9), 9, true},
		{uint64(10), 10, true},
		{float32(11), 11, true},
		{float64(12), 12, true},
		{"13", 13, true},
		{"notnum", 0, false},
		{struct{}{}, 0, false},
	}
	for _, tt := range tests {
		got, ok := toInt64(tt.v)
		assert.Equal(t, tt.ok, ok, "value %v ok", tt.v)
		if tt.ok {
			assert.Equal(t, tt.want, got, "value %v", tt.v)
		}
	}
}

func TestToFloat64(t *testing.T) {
	tests := []struct {
		v    any
		want float64
		ok   bool
	}{
		{int(1), 1, true},
		{int64(2), 2, true},
		{float64(3.5), 3.5, true},
		{float32(4), 4, true},
		{uint(5), 5, true},
		{uint64(6), 6, true},
		{"7.5", 7.5, true},
		{"bad", 0, false},
		{struct{}{}, 0, false},
	}
	for _, tt := range tests {
		got, ok := toFloat64(tt.v)
		assert.Equal(t, tt.ok, ok, "value %v ok", tt.v)
		if tt.ok {
			assert.InDelta(t, tt.want, got, 0.0001, "value %v", tt.v)
		}
	}
}

func TestWasm_BindUnsupportedAndName(t *testing.T) {
	rt, err := New(EngineWasm, WithWASM(testWasmAdd))
	require.NoError(t, err)
	defer rt.Close()
	assert.Equal(t, EngineWasm, rt.Name())
}
