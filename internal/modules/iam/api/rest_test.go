package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
)

type fakeService struct{}

func (fakeService) PermissionCatalog(context.Context) []authz.Action {
	return []authz.Action{{Resource: "store", Verb: "view", Code: "store_view"}}
}
func (fakeService) SpiceDBSchema(context.Context) string { return "definition user {}" }
func (fakeService) ScopeModel(context.Context) []ScopeNode {
	return []ScopeNode{{Type: "tenant", ParentType: "platform", Relation: "platform"}}
}
func (fakeService) MenuCatalog(context.Context) []MenuItem {
	return []MenuItem{{ID: "stores", PermissionCode: "store_view"}}
}

func TestRegisterREST(t *testing.T) {
	_, a := humatest.New(t)
	registrar := authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry())
	RegisterREST(a, fakeService{}, registrar)

	for _, path := range []string{
		"/iam/permission-catalog",
		"/iam/spicedb/schema",
		"/iam/scope-model",
		"/iam/menu-catalog",
	} {
		resp := a.Get(path)
		require.Equal(t, http.StatusOK, resp.Code, path)
		var body struct {
			Code int             `json:"code"`
			Data json.RawMessage `json:"data"`
		}
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
		assert.Equal(t, 0, body.Code)
		assert.NotEmpty(t, body.Data)
	}
}
