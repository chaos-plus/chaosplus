package guidv1_test

import (
	"context"
	"net"
	"testing"

	guidapi "github.com/chaos-plus/chaosplus/internal/infra/guid/api"
	guidv1 "github.com/chaos-plus/chaosplus/internal/infra/guid/proto/gen/go/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestGeneratedGRPCClientAndServerRoundTrip(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, request any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(ctx, request)
	}))
	var next int64 = 100
	guidapi.RegisterGRPC(server, func() (int64, error) {
		next++
		return next, nil
	})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	connection, err := grpc.NewClient("passthrough:///guid", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = connection.Close() })

	client := guidv1.NewGuidServiceClient(connection)
	response, err := client.Next(context.Background(), &guidv1.NextRequest{})
	require.NoError(t, err)
	assert.Equal(t, "101", response.GetId())
	batch, err := client.NextBatch(context.Background(), &guidv1.NextBatchRequest{Count: 3})
	require.NoError(t, err)
	assert.Equal(t, []string{"102", "103", "104"}, batch.GetIds())
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.Next(cancelled, &guidv1.NextRequest{})
	assert.Equal(t, codes.Canceled, status.Code(err))
	_, err = client.NextBatch(cancelled, &guidv1.NextBatchRequest{})
	assert.Equal(t, codes.Canceled, status.Code(err))
}

func TestUnimplementedGeneratedServerReturnsUnimplemented(t *testing.T) {
	server := guidv1.UnimplementedGuidServiceServer{}
	_, err := server.Next(context.Background(), &guidv1.NextRequest{})
	assert.Error(t, err)
	_, err = server.NextBatch(context.Background(), &guidv1.NextBatchRequest{})
	assert.Error(t, err)
}
