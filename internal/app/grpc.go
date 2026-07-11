package app

import (
	"errors"
	"fmt"
	"log/slog"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// StartGrpcServer binds the gRPC listener and starts serving in a background
// goroutine so Run can continue on to the REST server. The created server is
// stored on the App so awaitShutdown can stop it gracefully. A bind failure is
// returned to the caller; a serve failure after a successful bind is reported on
// a.serveErr so awaitShutdown can bring the whole app down.
func (a *App) StartGrpcServer() error {
	addr := fmt.Sprintf("%s:%d", a.cfg.GrpcServer.Host, a.cfg.GrpcServer.Port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("grpc listen %s: %w", addr, err)
	}

	server := grpc.NewServer()
	a.registerGRPC(server)
	reflection.Register(server) // enables grpcurl and service discovery for tooling
	a.grpc = server

	go func() {
		slog.Info("grpc server listening", "addr", addr)
		if err := server.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			a.serveErr <- fmt.Errorf("grpc serve: %w", err)
		}
	}()

	return nil
}
