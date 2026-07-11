package app

import (
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"google.golang.org/grpc"
)

// newTestApp builds an App with just the fields the lifecycle methods need, so
// tests can exercise shutdown/awaitShutdown without running Bootstrap (no otel,
// logging or database setup). The zero-value DatasourceRouter makes dbr.Close()
// a no-op.
func newTestApp() *App {
	return &App{serveErr: make(chan error, 2)}
}

// listen grabs an OS-assigned loopback port and fails the test on error.
func listen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	return ln
}

// TestShutdownNoServersIsNoop verifies shutdown is safe when nothing was
// started — the startup-failure path relies on this (nil servers are skipped).
func TestShutdownNoServersIsNoop(t *testing.T) {
	a := newTestApp()
	if err := a.shutdown(); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

// TestShutdownStopsRestServer verifies a running REST server is drained: its
// ListenAndServe/Serve returns http.ErrServerClosed after shutdown.
func TestShutdownStopsRestServer(t *testing.T) {
	ln := listen(t)
	srv := &http.Server{Handler: http.NewServeMux()}

	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve(ln) }()

	a := newTestApp()
	a.rest = srv

	if err := a.shutdown(); err != nil {
		t.Fatalf("shutdown returned error: %v", err)
	}

	select {
	case err := <-serveDone:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("expected http.ErrServerClosed, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("rest server did not stop within timeout")
	}
}

// TestShutdownStopsGrpcServer verifies a running gRPC server is gracefully
// stopped and its Serve goroutine returns.
func TestShutdownStopsGrpcServer(t *testing.T) {
	ln := listen(t)
	srv := grpc.NewServer()

	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve(ln) }()

	a := newTestApp()
	a.grpc = srv

	if err := a.shutdown(); err != nil {
		t.Fatalf("shutdown returned error: %v", err)
	}

	select {
	case err := <-serveDone:
		// GracefulStop makes Serve return nil (or ErrServerStopped); anything
		// else is a real failure.
		if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			t.Fatalf("grpc Serve returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("grpc server did not stop within timeout")
	}
}

// TestAwaitShutdownReturnsServeError verifies that a fatal serve error reported
// on serveErr wakes awaitShutdown, drives teardown, and is propagated to the
// caller (so the process exits non-zero).
func TestAwaitShutdownReturnsServeError(t *testing.T) {
	a := newTestApp()
	wantErr := errors.New("boom")
	a.serveErr <- wantErr // buffered, so awaitShutdown's select picks it immediately

	err := a.awaitShutdown()
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected returned error to wrap %v, got %v", wantErr, err)
	}
}

// TestAwaitShutdownStopsServersOnServeError verifies that when one server fails,
// awaitShutdown also tears down the other running server (fail-fast: one down,
// all down).
func TestAwaitShutdownStopsServersOnServeError(t *testing.T) {
	ln := listen(t)
	srv := &http.Server{Handler: http.NewServeMux()}
	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve(ln) }()

	a := newTestApp()
	a.rest = srv

	// Simulate the gRPC server dying after bind.
	a.serveErr <- errors.New("grpc serve: boom")

	if err := a.awaitShutdown(); err == nil {
		t.Fatal("expected non-nil error from awaitShutdown")
	}

	select {
	case err := <-serveDone:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("expected rest server to be stopped, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("rest server was not stopped after peer failure")
	}
}
