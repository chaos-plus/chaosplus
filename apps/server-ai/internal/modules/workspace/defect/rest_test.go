package defect

import (
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/danielgtaylor/huma/v2/humatest"
	"testing"
)

func TestRegisterRESTDeclaresDefectAuthorization(t *testing.T) {
	registry := authz.MustRegistry(Actions...)
	module := &Module{registrar: authz.NewDeclarationOnlyRegistrar(registry)}
	_, api := humatest.New(t)
	module.RegisterREST(api)
	if err := authz.ValidateOperations(api, registry); err != nil {
		t.Fatal(err)
	}
}
