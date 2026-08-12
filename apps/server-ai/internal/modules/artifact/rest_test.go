package artifact

import (
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
)

func TestRegisterRESTDeclaresArtifactEntityAuthorization(t *testing.T) {
	registry := authz.MustRegistry(Actions...)
	module := &Module{registrar: authz.NewDeclarationOnlyRegistrar(registry)}
	_, api := humatest.New(t)
	module.RegisterREST(api)
	if err := authz.ValidateOperations(api, registry); err != nil {
		t.Fatalf("validate artifact operations: %v", err)
	}
	wants := map[string]string{
		"artifact-list":        "artifact_view",
		"artifact-force-valid": "artifact_update",
		"artifact-reconcile":   "artifact_update",
	}
	for _, item := range api.OpenAPI().Paths {
		for _, operation := range []*huma.Operation{item.Get, item.Post} {
			if operation == nil {
				continue
			}
			want, ok := wants[operation.OperationID]
			if !ok {
				t.Fatalf("unexpected artifact operation %s", operation.OperationID)
			}
			if got := operation.Extensions[authz.GuardExtensionKey]; got != want {
				t.Errorf("%s guard = %v, want %s", operation.OperationID, got, want)
			}
			if !artifactRequiredParameter(operation, authz.TenantHeader) || !artifactRequiredParameter(operation, authz.EntityHeader) {
				t.Errorf("%s does not require tenant and entity selectors", operation.OperationID)
			}
			delete(wants, operation.OperationID)
		}
	}
	if len(wants) != 0 {
		t.Fatalf("missing artifact operations: %v", wants)
	}
}

func artifactRequiredParameter(operation *huma.Operation, name string) bool {
	for _, parameter := range operation.Parameters {
		if parameter.Name == name && parameter.Required {
			return true
		}
	}
	return false
}
