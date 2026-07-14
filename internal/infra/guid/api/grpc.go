// Package api is the guid module's transport layer (REST + gRPC). It depends on a
// small NextFunc port injected by the module rather than on the guid core, which
// keeps the module layering acyclic (module -> api -> proto; api never imports
// the module root). Ids cross the wire as decimal strings, matching the REST
// contract, so this package needs neither guid.ID nor guid.Next directly.
package api

import (
	"context"
	"strconv"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guidv1 "github.com/chaos-plus/chaosplus/internal/infra/guid/proto/gen/go/api/v1"
)

// maxBatch bounds a single batch request (mirrors the REST limit).
const maxBatch = 10000

// NextFunc mints one id. Injected by the module so this package depends on a port,
// not the guid core.
type NextFunc func() (int64, error)

type grpcServer struct {
	guidv1.UnimplementedGuidServiceServer
	next NextFunc
}

func (s *grpcServer) Next(_ context.Context, _ *guidv1.NextRequest) (*guidv1.NextResponse, error) {
	id, err := s.next()
	if err != nil {
		return nil, status.Error(codes.Unavailable, "guid_not_ready")
	}
	return &guidv1.NextResponse{Id: strconv.FormatInt(id, 10)}, nil
}

func (s *grpcServer) NextBatch(_ context.Context, in *guidv1.NextBatchRequest) (*guidv1.NextBatchResponse, error) {
	n := int(in.GetCount())
	if n < 1 || n > maxBatch {
		return nil, status.Errorf(codes.InvalidArgument, "count must be 1..%d", maxBatch)
	}
	ids := make([]string, 0, n)
	for range n {
		id, err := s.next()
		if err != nil {
			return nil, status.Error(codes.Unavailable, "guid_not_ready")
		}
		ids = append(ids, strconv.FormatInt(id, 10))
	}
	return &guidv1.NextBatchResponse{Ids: ids}, nil
}

// RegisterGRPC registers the GuidService on the gRPC server, minting ids via next.
func RegisterGRPC(server *grpc.Server, next NextFunc) {
	guidv1.RegisterGuidServiceServer(server, &grpcServer{next: next})
}
