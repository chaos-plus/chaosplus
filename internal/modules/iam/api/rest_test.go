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

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/spicedbx"
	iamdomain "github.com/chaos-plus/chaosplus/internal/modules/iam/domain"
)

type fakeService struct{ err error }

type allowVerifier struct{}

func (allowVerifier) Authenticate(context.Context, string, string) (*authnext.Claims, error) {
	return &authnext.Claims{Subject: "u1"}, nil
}

type allowPermission struct{}

func (allowPermission) Check(context.Context, spicedbx.ObjectRef, string, spicedbx.SubjectRef, spicedbx.ZedToken) (bool, error) {
	return true, nil
}

type allowMembership struct{}

func (allowMembership) IsMemberActive(context.Context, string, string) (bool, error) {
	return true, nil
}

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
func (f fakeService) PutTenantMember(context.Context, string, string, string, string, iamdomain.MemberStatus) (iamdomain.TenantMember, error) {
	return fakeTenantMember(), f.err
}
func (f fakeService) GetTenantMember(context.Context, string, string) (iamdomain.TenantMember, error) {
	return fakeTenantMember(), f.err
}
func (f fakeService) ListTenantMembers(context.Context, string, iamdomain.MemberFilter) ([]iamdomain.TenantMember, int64, error) {
	return []iamdomain.TenantMember{fakeTenantMember()}, 1, f.err
}
func (f fakeService) SetTenantMemberStatus(context.Context, string, string, iamdomain.MemberStatus) (iamdomain.TenantMember, error) {
	return fakeTenantMember(), f.err
}
func (f fakeService) ListTenantMemberRoles(context.Context, string, string) ([]string, error) {
	return []string{"r1"}, f.err
}
func (f fakeService) CreateMenu(context.Context, iamdomain.Menu) (iamdomain.Menu, error) {
	return fakeMenu(), f.err
}
func (f fakeService) ListMenus(context.Context, string) ([]iamdomain.Menu, error) {
	return []iamdomain.Menu{fakeMenu()}, f.err
}
func (f fakeService) GetMenu(context.Context, string, string) (iamdomain.Menu, error) {
	return fakeMenu(), f.err
}
func (f fakeService) UpdateMenu(context.Context, iamdomain.Menu) (iamdomain.Menu, error) {
	return fakeMenu(), f.err
}
func (f fakeService) DeleteMenu(context.Context, string, string) error { return f.err }
func (f fakeService) EffectiveMenus(context.Context, string, string) ([]MenuItem, error) {
	return []MenuItem{{ID: "m1", Label: "Users"}}, f.err
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
		{iamdomain.ErrMemberNotFound, http.StatusNotFound},
		{iamdomain.ErrMenuNotFound, http.StatusNotFound},
		{iamdomain.ErrMenuConflict, http.StatusConflict},
		{iamdomain.ErrMenuHasChildren, http.StatusConflict},
		{iamdomain.ErrMemberInactive, http.StatusConflict},
		{errors.New("database down"), http.StatusInternalServerError},
	}
	for _, tc := range tests {
		err := apiError("test", tc.err)
		statusErr, ok := err.(interface{ GetStatus() int })
		require.True(t, ok)
		assert.Equal(t, tc.status, statusErr.GetStatus())
	}
}

func TestRegisterRESTAdminRoutes(t *testing.T) {
	_, a := humatest.New(t)
	RegisterREST(a, fakeService{}, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	header := "X-Tenant-Id: t1"
	responses := []*httptest.ResponseRecorder{
		a.Get("/iam/members?limit=50", header),
		a.Post("/iam/members", header, map[string]any{"subject": "u1", "display_name": "User", "status": "active"}),
		a.Get("/iam/members/u1", header),
		a.Patch("/iam/members/u1", header, map[string]any{"status": "disabled"}),
		a.Get("/iam/members/u1/roles", header),
		a.Get("/iam/menus", header),
		a.Post("/iam/menus", header, map[string]any{"label": "Users", "route": "/iam/users", "sort_order": 0, "permission_code": "user_view", "status": "active"}),
		a.Get("/iam/menus/m1", header),
		a.Patch("/iam/menus/m1", header, map[string]any{"label": "People"}),
		a.Delete("/iam/menus/m1", header),
	}
	for _, response := range responses {
		assert.Equal(t, http.StatusOK, response.Code, response.Body.String())
	}
}

func TestAdminWriteRouteErrors(t *testing.T) {
	_, a := humatest.New(t)
	RegisterREST(a, fakeService{err: iamdomain.ErrInvalidArgument}, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	header := "X-Tenant-Id: t1"
	responses := []*httptest.ResponseRecorder{
		a.Get("/iam/members?limit=50", header),
		a.Post("/iam/members", header, map[string]any{"subject": "u1", "display_name": "User", "status": "active"}),
		a.Patch("/iam/members/u1", header, map[string]any{"status": "disabled"}),
		a.Get("/iam/members/u1/roles", header),
		a.Get("/iam/menus", header),
		a.Post("/iam/menus", header, map[string]any{"label": "Users", "sort_order": 0, "status": "active"}),
		a.Patch("/iam/menus/m1", header, map[string]any{"label": "People"}),
		a.Delete("/iam/menus/m1", header),
	}
	for _, response := range responses {
		assert.Equal(t, http.StatusUnprocessableEntity, response.Code, response.Body.String())
	}
}

func TestEffectiveMenuRouteUsesAuthenticatedSubject(t *testing.T) {
	_, a := humatest.New(t)
	registrar := authz.NewRegistrar(authz.DefaultRegistry(), allowVerifier{}, allowPermission{}, allowMembership{})
	RegisterREST(a, fakeService{}, registrar)
	response := a.Get("/iam/me/menus", "X-Tenant-Id: t1", "Authorization: Bearer token")
	assert.Equal(t, http.StatusOK, response.Code, response.Body.String())
}

func TestAdminRouteErrors(t *testing.T) {
	tests := []struct {
		err    error
		path   string
		status int
	}{
		{iamdomain.ErrMemberNotFound, "/iam/members/u1", http.StatusNotFound},
		{iamdomain.ErrMenuNotFound, "/iam/menus/m1", http.StatusNotFound},
		{iamdomain.ErrMenuHasChildren, "/iam/menus/m1", http.StatusConflict},
	}
	for _, tc := range tests {
		_, a := humatest.New(t)
		RegisterREST(a, fakeService{err: tc.err}, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
		var response *httptest.ResponseRecorder
		if errors.Is(tc.err, iamdomain.ErrMenuHasChildren) {
			response = a.Delete(tc.path, "X-Tenant-Id: t1")
		} else {
			response = a.Get(tc.path, "X-Tenant-Id: t1")
		}
		assert.Equal(t, tc.status, response.Code, response.Body.String())
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

func fakeTenantMember() iamdomain.TenantMember {
	return iamdomain.TenantMember{TenantID: "t1", Subject: "u1", DisplayName: "User", Status: iamdomain.MemberActive, CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(1, 0)}
}

func fakeMenu() iamdomain.Menu {
	return iamdomain.Menu{ID: "m1", TenantID: "t1", Label: "Users", Route: "/iam/users", PermissionCode: "user_view", Status: iamdomain.MenuActive, CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(1, 0)}
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
