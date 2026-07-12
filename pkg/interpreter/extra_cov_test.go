package interpreter

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLua_CallBoundGoFunc_AnyArg covers the reflect.Interface branch of convertToType,
// where a Lua value is passed to a Go func parameter typed as any.
func TestLua_CallBoundGoFunc_AnyArg(t *testing.T) {
	rt, _ := New(EngineLua,
		WithBinding("echo", func(v any) string {
			if s, ok := v.(string); ok {
				return s
			}
			return "other"
		}),
	)
	defer rt.Close()
	v, err := rt.Eval(context.Background(), `return echo("hello")`)
	require.NoError(t, err)
	assert.Equal(t, "hello", v)
}

// TestLua_EvalReturnsTable covers the default branch of luaValueToGo, where a non-scalar
// Lua value (a table) is returned as-is.
func TestLua_EvalReturnsTable(t *testing.T) {
	rt, _ := New(EngineLua)
	defer rt.Close()
	v, err := rt.Eval(context.Background(), `return {1, 2, 3}`)
	require.NoError(t, err)
	assert.NotNil(t, v) // returned as the underlying *lua.LTable
}

// TestYaegi_EvalNilPointer covers exportYaegiValue's nil pointer/interface branch.
func TestYaegi_EvalNilPointer(t *testing.T) {
	rt, _ := New(EngineYaegi)
	defer rt.Close()
	_, err := rt.Eval(context.Background(), `var p *int`)
	require.NoError(t, err)
	v, err := rt.Eval(context.Background(), `p`)
	require.NoError(t, err)
	assert.Nil(t, v)
}

// TestYaegi_BindFloat32 covers the float32 branch of yaegiLiteral.
func TestYaegi_BindFloat32(t *testing.T) {
	rt, _ := New(EngineYaegi)
	defer rt.Close()
	require.NoError(t, rt.Bind("f", float32(3.5)))
	v, err := rt.Eval(context.Background(), `f`)
	require.NoError(t, err)
	assert.InDelta(t, float32(3.5), v, 0.0001)
}
