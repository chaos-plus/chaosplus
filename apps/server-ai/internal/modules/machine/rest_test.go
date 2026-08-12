package machine

import (
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
)

func TestRegisterRESTDeclaresEntityAuthorization(t *testing.T) {
	registry := authz.MustRegistry(Actions...)
	module := &Module{hub: &Hub{}, registrar: authz.NewDeclarationOnlyRegistrar(registry)}
	_, api := humatest.New(t)
	module.RegisterREST(api)

	if err := authz.ValidateOperations(api, registry); err != nil {
		t.Fatalf("validate machine operations: %v", err)
	}
	operations := map[string]string{
		"machine-issue-onboarding-token": "machine_create",
		"machine-list":                   "machine_view",
		"machine-get":                    "machine_view",
		"machine-confirm":                "machine_update",
		"machine-onboarding-status":      "machine_view",
		"machine-delete":                 "machine_delete",
		"machine-rotate-token":           "machine_update",
	}
	for path, item := range api.OpenAPI().Paths {
		for _, operation := range []*huma.Operation{item.Get, item.Post, item.Delete} {
			if operation == nil || operation.OperationID == "machine-daemon-websocket" {
				continue
			}
			want, exists := operations[operation.OperationID]
			if !exists {
				t.Fatalf("unexpected operation %s %s", operation.Method, path)
			}
			if got := operation.Extensions[authz.GuardExtensionKey]; got != want {
				t.Errorf("%s guard = %v, want %s", operation.OperationID, got, want)
			}
			if !hasRequiredParameter(operation, authz.TenantHeader) || !hasRequiredParameter(operation, authz.EntityHeader) {
				t.Errorf("%s does not require tenant and entity selectors", operation.OperationID)
			}
			delete(operations, operation.OperationID)
		}
	}
	if len(operations) != 0 {
		t.Fatalf("missing machine operations: %v", operations)
	}

	websocket := api.OpenAPI().Paths["/api/machines/ws"].Get
	if websocket == nil || websocket.OperationID != "machine-daemon-websocket" {
		t.Fatal("machine daemon websocket is not registered")
	}
	if _, guarded := websocket.Extensions[authz.GuardExtensionKey]; guarded {
		t.Fatal("machine-token websocket must not use a user IAM guard")
	}
	if len(websocket.Security) != 0 || !hasRequiredParameter(websocket, "token") {
		t.Fatal("machine-token websocket contract is incomplete")
	}
}

func hasRequiredParameter(operation *huma.Operation, name string) bool {
	for _, parameter := range operation.Parameters {
		if parameter.Name == name && parameter.Required {
			return true
		}
	}
	return false
}
