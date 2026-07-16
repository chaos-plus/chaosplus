package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	iamdomain "github.com/chaos-plus/chaosplus/internal/modules/iam/domain"
)

type fakeService struct{ err error }

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
func (f fakeService) CreateRole(context.Context, string, string, string) (iamdomain.Role, error) {
	return fakeRole(), f.err
}
func (f fakeService) ListRoles(context.Context, string) ([]iamdomain.Role, error) {
	return []iamdomain.Role{fakeRole()}, f.err
}
func (f fakeService) GetRole(context.Context, string, string) (iamdomain.Role, error) {
	return fakeRole(), f.err
}
func (f fakeService) UpdateRole(context.Context, string, string, *string, *string) (iamdomain.Role, error) {
	return fakeRole(), f.err
}
func (f fakeService) DeleteRole(context.Context, string, string) error { return f.err }
func (f fakeService) ListPermissions(context.Context, string, string) ([]string, error) {
	return []string{"store_view"}, f.err
}
func (f fakeService) GrantPermission(context.Context, string, string, string) (bool, error) {
	return true, f.err
}
func (f fakeService) RevokePermission(context.Context, string, string, string) (bool, error) {
	return true, f.err
}
func (f fakeService) ListMembers(context.Context, string, string) ([]string, error) {
	return []string{"u1"}, f.err
}
func (f fakeService) AddMember(context.Context, string, string, string) (bool, error) {
	return true, f.err
}
func (f fakeService) RemoveMember(context.Context, string, string, string) (bool, error) {
	return true, f.err
}

func TestRegisterRESTWriteRoutes(t *testing.T) {
	_, a := humatest.New(t)
	RegisterREST(a, fakeService{}, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	header := "X-Tenant-Id: t1"

	responses := []*httptest.ResponseRecorder{
		a.Post("/iam/roles", header, map[string]any{"name": "Managers"}),
		a.Patch("/iam/roles/r1", header, map[string]any{"name": "Operators"}),
		a.Put("/iam/roles/r1/permissions/store_view", header),
		a.Delete("/iam/roles/r1/permissions/store_view", header),
		a.Put("/iam/roles/r1/members/u1", header),
		a.Delete("/iam/roles/r1/members/u1", header),
		a.Delete("/iam/roles/r1", header),
	}
	for _, response := range responses {
		assert.Equal(t, http.StatusOK, response.Code, response.Body.String())
	}
}

func TestAPIErrorMapping(t *testing.T) {
	tests := []struct {
		err    error
		status int
	}{
		{iamdomain.ErrRoleNotFound, http.StatusNotFound},
		{iamdomain.ErrRoleNameConflict, http.StatusConflict},
		{iamdomain.ErrInvalidArgument, http.StatusUnprocessableEntity},
		{iamdomain.ErrPermissionNotFound, http.StatusUnprocessableEntity},
		{errors.New("database down"), http.StatusInternalServerError},
	}
	for _, tc := range tests {
		err := apiError("test", tc.err)
		statusErr, ok := err.(interface{ GetStatus() int })
		require.True(t, ok)
		assert.Equal(t, tc.status, statusErr.GetStatus())
	}
}

func TestRegisterRESTWriteRouteErrors(t *testing.T) {
	_, a := humatest.New(t)
	RegisterREST(a, fakeService{err: iamdomain.ErrRoleNotFound}, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	header := "X-Tenant-Id: t1"
	responses := []*httptest.ResponseRecorder{
		a.Get("/iam/roles", header),
		a.Post("/iam/roles", header, map[string]any{"name": "Managers"}),
		a.Get("/iam/roles/r1", header),
		a.Patch("/iam/roles/r1", header, map[string]any{"name": "Operators"}),
		a.Delete("/iam/roles/r1", header),
		a.Get("/iam/roles/r1/permissions", header),
		a.Put("/iam/roles/r1/permissions/store_view", header),
		a.Delete("/iam/roles/r1/permissions/store_view", header),
		a.Get("/iam/roles/r1/members", header),
		a.Put("/iam/roles/r1/members/u1", header),
		a.Delete("/iam/roles/r1/members/u1", header),
	}
	for _, response := range responses {
		assert.Equal(t, http.StatusNotFound, response.Code, response.Body.String())
	}
}

func TestRoleDomainConversion(t *testing.T) {
	role := fakeRole()
	got := roleFromDomain(role)
	assert.Equal(t, role.ID, got.ID)
	assert.Equal(t, []Role{got}, rolesFromDomain([]iamdomain.Role{role}))
}

func fakeRole() iamdomain.Role {
	return iamdomain.Role{ID: "r1", TenantID: "t1", Name: "Managers", CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(1, 0)}
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
		"/iam/roles",
		"/iam/roles/r1",
		"/iam/roles/r1/permissions",
		"/iam/roles/r1/members",
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
