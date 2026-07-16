package iam

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	iamapi "github.com/chaos-plus/chaosplus/internal/modules/iam/api"
)

// Module exposes the first IAM management surface. It is intentionally read-only
// for now: SpiceDB remains the future source of truth for grants, while these
// endpoints let the admin UI discover the catalog and scope model.
type Module struct {
	service   *Service
	registrar *authz.Registrar
}

func NewModule(registrar *authz.Registrar) *Module {
	if registrar == nil {
		panic("iam module requires an authz registrar")
	}
	return &Module{
		service:   NewService(registrar.Registry()),
		registrar: registrar,
	}
}

func (m *Module) RegisterREST(api huma.API) {
	iamapi.RegisterREST(api, m.service, m.registrar)
}

// Service owns IAM read models that are not authorization decisions.
type Service struct {
	registry *authz.Registry
}

func NewService(registry *authz.Registry) *Service {
	return &Service{registry: registry}
}

func (s *Service) PermissionCatalog(context.Context) []authz.Action {
	return s.registry.All()
}

func (s *Service) SpiceDBSchema(context.Context) string {
	return authz.GenerateSchema(s.registry.All())
}

func (s *Service) ScopeModel(context.Context) []iamapi.ScopeNode {
	return []iamapi.ScopeNode{
		{Type: "platform", ParentType: "", Relation: "administer", Label: "Platform"},
		{Type: "tenant", ParentType: "platform", Relation: "platform", Label: "Brand tenant"},
		{Type: "merchant", ParentType: "tenant", Relation: "tenant", Label: "Merchant"},
		{Type: "store", ParentType: "merchant", Relation: "merchant", Label: "Store"},
		{Type: "dept", ParentType: "tenant", Relation: "parent", Label: "Department tree"},
	}
}

func (s *Service) MenuCatalog(context.Context) []iamapi.MenuItem {
	return []iamapi.MenuItem{
		{ID: "iam", Label: "IAM", PermissionCode: "menu_view", Children: []iamapi.MenuItem{
			{ID: "iam-users", Label: "Users", Path: "/iam/users", PermissionCode: "user_view"},
			{ID: "iam-roles", Label: "Roles", Path: "/iam/roles", PermissionCode: "role_view"},
			{ID: "iam-depts", Label: "Departments", Path: "/iam/depts", PermissionCode: "dept_view"},
			{ID: "iam-menus", Label: "Menus", Path: "/iam/menus", PermissionCode: "menu_view"},
		}},
		{ID: "org", Label: "Organization", PermissionCode: "tenant_view", Children: []iamapi.MenuItem{
			{ID: "org-tenants", Label: "Tenants", Path: "/tenants", PermissionCode: "tenant_view"},
			{ID: "org-merchants", Label: "Merchants", Path: "/merchants", PermissionCode: "merchant_view"},
			{ID: "org-stores", Label: "Stores", Path: "/stores", PermissionCode: "store_view"},
		}},
	}
}
