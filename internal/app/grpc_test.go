package app

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withCtx(app *App) *App {
	app.ctx, app.cancel = context.WithCancel(context.Background())
	return app
}

func TestStartGrpcServer_SuccessThenShutdown(t *testing.T) {
	a := withCtx(NewApp(Config{GrpcServer: GrpcServer{Host: "127.0.0.1", Port: 0}}))
	require.NoError(t, a.StartGrpcServer())
	require.NotNil(t, a.grpc)
	require.NoError(t, a.shutdown())
}

func TestStartGrpcServer_BindError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	a := withCtx(NewApp(Config{GrpcServer: GrpcServer{Host: "127.0.0.1", Port: port}}))
	assert.Error(t, a.StartGrpcServer())
}
