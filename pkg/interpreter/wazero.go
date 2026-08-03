package interpreter

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

const defaultWASMMemoryPages = 256 // 16MiB

func init() { registry.add(EngineWasm) }

// wasmRuntime wraps a Wazero WebAssembly runtime with one instantiated module.
//
// Bind is not supported for Wasm because host functions must be defined before the
// module is instantiated. Use Eval only to verify the module is loaded (expr is ignored
// and the module name is returned); use Call to invoke exported functions. Arguments are
// converted to Wasm i32, i64, f32 or f64 values based on the function's parameter types.
//
// The runtime serializes calls because guest module instances may not be safe for
// arbitrary concurrent access.
type wasmRuntime struct {
	mu  sync.Mutex
	rt  wazero.Runtime
	mod api.Module
}

func newWasmRuntime(wasmBytes []byte, memoryPages uint32) (Runtime, error) {
	if len(wasmBytes) == 0 {
		return nil, fmt.Errorf("interpreter/wasm: module bytes are required (use WithWASM)")
	}
	if memoryPages == 0 {
		memoryPages = defaultWASMMemoryPages
	}
	ctx := context.Background()
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithMemoryLimitPages(memoryPages).WithCloseOnContextDone(true))
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		_ = rt.Close(ctx)
		return nil, fmt.Errorf("interpreter/wasm: compile module: %w", err)
	}
	defer compiled.Close(ctx)
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithStartFunctions())
	if err != nil {
		_ = rt.Close(ctx)
		return nil, fmt.Errorf("interpreter/wasm: instantiate module: %w", err)
	}
	return &wasmRuntime{rt: rt, mod: mod}, nil
}

func (r *wasmRuntime) CallBytes(ctx context.Context, fn string, input []byte, maxOutput uint32) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if maxOutput == 0 {
		return nil, fmt.Errorf("interpreter/wasm: max output must be positive")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	alloc := r.mod.ExportedFunction("alloc")
	hook := r.mod.ExportedFunction(fn)
	if alloc == nil || hook == nil || r.mod.Memory() == nil {
		return nil, fmt.Errorf("interpreter/wasm: byte ABI requires memory, alloc and %q exports: %w", fn, ErrNotFound)
	}
	allocated, err := alloc.Call(ctx, uint64(len(input)))
	if err != nil || len(allocated) != 1 {
		return nil, fmt.Errorf("interpreter/wasm: allocate input: %w", err)
	}
	if uint64(len(input)) > math.MaxUint32 {
		return nil, fmt.Errorf("interpreter/wasm: input exceeds 32-bit guest ABI")
	}
	inputPtr := api.DecodeU32(allocated[0])
	if ok := r.mod.Memory().Write(inputPtr, input); !ok {
		return nil, fmt.Errorf("interpreter/wasm: input exceeds guest memory")
	}
	inputLen := api.DecodeU32(uint64(len(input)))
	result, err := hook.Call(ctx, uint64(inputPtr), uint64(inputLen))
	r.deallocate(ctx, inputPtr, inputLen)
	if err != nil || len(result) != 1 {
		return nil, fmt.Errorf("interpreter/wasm: call %q: %w", fn, err)
	}
	outputPtr, outputLen := api.DecodeU32(result[0]>>32), api.DecodeU32(result[0])
	if outputLen > maxOutput {
		return nil, fmt.Errorf("interpreter/wasm: output is %d bytes, limit is %d", outputLen, maxOutput)
	}
	output, ok := r.mod.Memory().Read(outputPtr, outputLen)
	if !ok {
		return nil, fmt.Errorf("interpreter/wasm: output exceeds guest memory")
	}
	copyOfOutput := append([]byte(nil), output...)
	r.deallocate(ctx, outputPtr, outputLen)
	return copyOfOutput, nil
}

func (r *wasmRuntime) deallocate(ctx context.Context, ptr, size uint32) {
	if dealloc := r.mod.ExportedFunction("dealloc"); dealloc != nil {
		_, _ = dealloc.Call(ctx, uint64(ptr), uint64(size))
	}
}

func (r *wasmRuntime) Name() string { return EngineWasm }

func (r *wasmRuntime) Bind(name string, value any) error {
	return fmt.Errorf("interpreter/wasm: Bind %q is %w; define host functions before instantiation", name, ErrUnsupported)
}

func (r *wasmRuntime) Eval(ctx context.Context, expr string) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.mod.Name(), nil
}

func (r *wasmRuntime) Call(ctx context.Context, fn string, args ...any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	f := r.mod.ExportedFunction(fn)
	if f == nil {
		return nil, fmt.Errorf("interpreter/wasm: exported function %q not found: %w", fn, ErrNotFound)
	}
	def := f.Definition()
	paramTypes := def.ParamTypes()
	if len(args) != len(paramTypes) {
		return nil, fmt.Errorf("interpreter/wasm: function %q expects %d args, got %d", fn, len(paramTypes), len(args))
	}
	encoded := make([]uint64, len(args))
	for i, a := range args {
		v, err := encodeWasmValue(paramTypes[i], a)
		if err != nil {
			return nil, fmt.Errorf("interpreter/wasm: arg %d for %q: %w", i, fn, err)
		}
		encoded[i] = v
	}
	res, err := f.Call(ctx, encoded...)
	if err != nil {
		return nil, fmt.Errorf("interpreter/wasm: call %q: %w", fn, err)
	}
	return decodeWasmResults(def.ResultTypes(), res), nil
}

func (r *wasmRuntime) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.rt == nil {
		return nil
	}
	return r.rt.Close(context.Background())
}

func encodeWasmValue(t api.ValueType, v any) (uint64, error) {
	switch t {
	case api.ValueTypeI32:
		n, ok := toInt64(v)
		if !ok || n < math.MinInt32 || n > math.MaxInt32 {
			return 0, fmt.Errorf("expected integer for i32, got %T", v)
		}
		return api.EncodeI32(int32(n)), nil
	case api.ValueTypeI64:
		n, ok := toInt64(v)
		if !ok {
			return 0, fmt.Errorf("expected integer for i64, got %T", v)
		}
		return api.EncodeI64(n), nil
	case api.ValueTypeF32:
		n, ok := toFloat64(v)
		if !ok {
			return 0, fmt.Errorf("expected number for f32, got %T", v)
		}
		return api.EncodeF32(float32(n)), nil
	case api.ValueTypeF64:
		n, ok := toFloat64(v)
		if !ok {
			return 0, fmt.Errorf("expected number for f64, got %T", v)
		}
		return api.EncodeF64(n), nil
	}
	return 0, fmt.Errorf("unsupported wasm value type %v", t)
}

func decodeWasmResults(types []api.ValueType, values []uint64) any {
	if len(values) == 0 {
		return nil
	}
	if len(values) == 1 {
		return decodeWasmValue(types[0], values[0])
	}
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = decodeWasmValue(types[i], v)
	}
	return out
}

func decodeWasmValue(t api.ValueType, v uint64) any {
	switch t {
	case api.ValueTypeI32:
		return api.DecodeI32(v)
	case api.ValueTypeI64:
		return int64(v)
	case api.ValueTypeF32:
		return api.DecodeF32(v)
	case api.ValueTypeF64:
		return api.DecodeF64(v)
	}
	return v
}

func toInt64(v any) (int64, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true
	case int8:
		return int64(x), true
	case int16:
		return int64(x), true
	case int32:
		return int64(x), true
	case int64:
		return x, true
	case uint:
		if uint64(x) > math.MaxInt64 {
			return 0, false
		}
		return int64(x), true
	case uint8:
		return int64(x), true
	case uint16:
		return int64(x), true
	case uint32:
		return int64(x), true
	case uint64:
		if x > math.MaxInt64 {
			return 0, false
		}
		return int64(x), true
	case float32:
		return finiteInteger(float64(x))
	case float64:
		return finiteInteger(x)
	case string:
		if n, err := strconv.ParseInt(x, 10, 64); err == nil {
			return n, true
		}
	}
	return 0, false
}

func finiteInteger(value float64) (int64, bool) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < math.MinInt64 || value >= -math.MinInt64 || math.Trunc(value) != value {
		return 0, false
	}
	return int64(value), true
}
