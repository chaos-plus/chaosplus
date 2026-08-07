package guidv1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedMessagesImplementProtocolContract(t *testing.T) {
	nextRequest := &NextRequest{}
	nextRequest.Reset()
	assert.NotEmpty(t, nextRequest.ProtoReflect().Descriptor().FullName())
	assert.Empty(t, nextRequest.String())
	descriptor, _ := nextRequest.Descriptor()
	assert.NotEmpty(t, descriptor)

	nextResponse := &NextResponse{Id: "42"}
	assert.Equal(t, "42", nextResponse.GetId())
	assert.NotEmpty(t, nextResponse.String())
	nextResponse.Reset()
	assert.Empty(t, nextResponse.GetId())
	assert.NotEmpty(t, nextResponse.ProtoReflect().Descriptor().FullName())
	descriptor, _ = nextResponse.Descriptor()
	assert.NotEmpty(t, descriptor)

	batchRequest := &NextBatchRequest{Count: 3}
	assert.Equal(t, uint32(3), batchRequest.GetCount())
	assert.NotEmpty(t, batchRequest.String())
	batchRequest.Reset()
	assert.Zero(t, batchRequest.GetCount())
	assert.NotEmpty(t, batchRequest.ProtoReflect().Descriptor().FullName())
	descriptor, _ = batchRequest.Descriptor()
	assert.NotEmpty(t, descriptor)

	batchResponse := &NextBatchResponse{Ids: []string{"1", "2", "3"}}
	require.Equal(t, []string{"1", "2", "3"}, batchResponse.GetIds())
	assert.NotEmpty(t, batchResponse.String())
	batchResponse.Reset()
	assert.Nil(t, batchResponse.GetIds())
	assert.NotEmpty(t, batchResponse.ProtoReflect().Descriptor().FullName())
	descriptor, _ = batchResponse.Descriptor()
	assert.NotEmpty(t, descriptor)

	assert.Empty(t, (*NextResponse)(nil).GetId())
	assert.Zero(t, (*NextBatchRequest)(nil).GetCount())
	assert.Nil(t, (*NextBatchResponse)(nil).GetIds())
	assert.NotEmpty(t, file_api_v1_guid_proto_rawDescGZIP())
}
