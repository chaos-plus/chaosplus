package app

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withCtx gives an App the root context its lifecycle methods expect.
func withCtx(a *App) *App {
	a.ctx, a.cancel = context.WithCancel(context.Background())
	return a
}

func TestStartGrpcServer_SuccessThenShutdown(t *testing.T) {
	a := withCtx(NewApp(Config{GrpcServer: GrpcServer{Host: "127.0.0.1", Port: 0}}))

	require.NoError(t, a.StartGrpcServer())
	require.NotNil(t, a.grpc)

	require.NoError(t, a.shutdown())
}

func TestStartGrpcServer_BindError(t *testing.T) {
	// Occupy a port, then ask the gRPC server to bind the same one.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	a := withCtx(NewApp(Config{GrpcServer: GrpcServer{Host: "127.0.0.1", Port: port}}))
	assert.Error(t, a.StartGrpcServer())
}

func TestStartRestServer_SuccessThenShutdown(t *testing.T) {
	a := withCtx(NewApp(Config{Name: "test", RestServer: RestServer{Host: "127.0.0.1", Port: 0}}))

	require.NoError(t, a.StartRestServer())
	require.NotNil(t, a.rest)

	require.NoError(t, a.shutdown())
}
