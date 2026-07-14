package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guidv1 "github.com/chaos-plus/chaosplus/internal/infra/guid/proto/gen/go/api/v1"
)

func TestGRPC_Next(t *testing.T) {
	s := &grpcServer{next: counterNext()}
	resp, err := s.Next(context.Background(), &guidv1.NextRequest{})
	require.NoError(t, err)
	assert.Regexp(t, `^\d+$`, resp.GetId())
}

func TestGRPC_Next_Unavailable(t *testing.T) {
	s := &grpcServer{next: failingNext()}
	_, err := s.Next(context.Background(), &guidv1.NextRequest{})
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

func TestGRPC_NextBatch_Distinct(t *testing.T) {
	s := &grpcServer{next: counterNext()}
	resp, err := s.NextBatch(context.Background(), &guidv1.NextBatchRequest{Count: 5})
	require.NoError(t, err)
	require.Len(t, resp.GetIds(), 5)
	seen := map[string]struct{}{}
	for _, id := range resp.GetIds() {
		_, dup := seen[id]
		assert.False(t, dup, "batch ids must be unique")
		seen[id] = struct{}{}
	}
}

func TestGRPC_NextBatch_OutOfRange(t *testing.T) {
	s := &grpcServer{next: counterNext()}
	for _, n := range []uint32{0, maxBatch + 1} {
		_, err := s.NextBatch(context.Background(), &guidv1.NextBatchRequest{Count: n})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	}
}

func TestGRPC_NextBatch_Unavailable(t *testing.T) {
	s := &grpcServer{next: failingNext()}
	_, err := s.NextBatch(context.Background(), &guidv1.NextBatchRequest{Count: 3})
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

func TestRegisterGRPC_Registers(t *testing.T) {
	srv := grpc.NewServer()
	RegisterGRPC(srv, counterNext())
	_, ok := srv.GetServiceInfo()["chaosplus.guid.v1.GuidService"]
	assert.True(t, ok, "GuidService should be registered")
}
