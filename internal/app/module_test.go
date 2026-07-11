package app

import (
	"context"
	"errors"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"google.golang.org/grpc"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
)

// fakeModule implements every capability interface, with configurable errors and
// spies, so the phase runners can be exercised in isolation.
type fakeModule struct {
	name       string
	migrateErr error
	startErr   error
	stopErr    error

	migrated bool
	started  bool
	stopped  bool
	restReg  bool
	grpcReg  bool

	stopOrder *[]string
}

func (f *fakeModule) Migrate(context.Context) error { f.migrated = true; return f.migrateErr }
func (f *fakeModule) Start(context.Context) error   { f.started = true; return f.startErr }
func (f *fakeModule) RegisterREST(huma.API)         { f.restReg = true }
func (f *fakeModule) RegisterGRPC(*grpc.Server)     { f.grpcReg = true }
func (f *fakeModule) Stop(context.Context) error {
	f.stopped = true
	if f.stopOrder != nil {
		*f.stopOrder = append(*f.stopOrder, f.name)
	}
	return f.stopErr
}

// inert has none of the capabilities, proving the runners skip non-participants.
type inert struct{}

func TestMigrateModules(t *testing.T) {
	f := &fakeModule{}
	a := &App{mods: []any{f, inert{}}}
	require.NoError(t, a.migrateModules(context.Background()))
	assert.True(t, f.migrated)

	bad := &fakeModule{migrateErr: errors.New("boom")}
	a = &App{mods: []any{bad}}
	assert.ErrorContains(t, a.migrateModules(context.Background()), "boom")
}

func TestStartModules(t *testing.T) {
	f := &fakeModule{}
	a := &App{mods: []any{f, inert{}}}
	require.NoError(t, a.startModules(context.Background()))
	assert.True(t, f.started)

	bad := &fakeModule{startErr: errors.New("nope")}
	a = &App{mods: []any{bad}}
	assert.ErrorContains(t, a.startModules(context.Background()), "nope")
}

func TestRegisterPhases(t *testing.T) {
	f := &fakeModule{}
	a := &App{mods: []any{f, inert{}}}
	a.registerREST(nil)
	a.registerGRPC(nil)
	assert.True(t, f.restReg)
	assert.True(t, f.grpcReg)
}

func TestStopModules_ReverseOrderAndJoinsErrors(t *testing.T) {
	var order []string
	a := &App{mods: []any{
		&fakeModule{name: "a", stopOrder: &order},
		inert{},
		&fakeModule{name: "b", stopOrder: &order, stopErr: errors.New("b-fail")},
		&fakeModule{name: "c", stopOrder: &order},
	}}

	err := a.stopModules(context.Background())
	assert.ErrorContains(t, err, "b-fail")
	// Reverse registration order, skipping the inert module.
	assert.Equal(t, []string{"c", "b", "a"}, order)
}

func TestBuildModules(t *testing.T) {
	// No writable database → only the geoip module.
	a := &App{}
	mods := a.buildModules()
	require.Len(t, mods, 1)
	_, isGeoip := mods[0].(geoipModule)
	assert.True(t, isGeoip)

	// With a writer → id generator is prepended.
	a = &App{dbr: bunx.DatasourceRouter{Writer: []*bun.DB{nil}}}
	require.Len(t, a.buildModules(), 2)
}

func TestFailStop(t *testing.T) {
	a := &App{serveErr: make(chan error, 1)}
	a.failStop(errors.New("lease gone"))
	select {
	case err := <-a.serveErr:
		assert.ErrorContains(t, err, "lease gone")
	default:
		t.Fatal("expected a fatal error on serveErr")
	}

	// Full channel must not block (app is already going down).
	a.failStop(errors.New("second"))
}
