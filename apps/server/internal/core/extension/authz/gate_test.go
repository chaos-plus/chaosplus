package authz

import (
	"context"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOperationGateInputAndNamingBoundaries(t *testing.T) {
	assert.Error(t, ValidateOperations(nil, DefaultRegistry()))
	assert.Error(t, ValidateOperations(nil, nil))
	assert.Nil(t, operations(nil))
	assert.Equal(t, "unnamed", operationName(nil))
	assert.Equal(t, "unnamed", operationName(&huma.Operation{}))
}

func TestOperationGateRejectsUndeclaredPermissionMetadata(t *testing.T) {
	_, api := humatest.New(t)
	huma.Register(api, huma.Operation{
		OperationID: "undeclared-guard",
		Method:      http.MethodGet,
		Path:        "/undeclared-guard",
		Metadata:    map[string]any{guardMetadataKey: "missing_view"},
	}, func(context.Context, *struct{}) (*struct{ Body string }, error) {
		return &struct{ Body string }{Body: "unreachable"}, nil
	})

	err := ValidateOperations(api, DefaultRegistry())
	require.Error(t, err)
	assert.ErrorContains(t, err, `undeclared-guard uses undeclared permission "missing_view"`)
}
