package organization

import (
	"context"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/danielgtaylor/huma/v2"
	"github.com/uptrace/bun"
)

type Module struct {
	service         *Service
	tenants         *TenantService
	positions       *PositionService
	groups          *GroupService
	invitations     *InvitationService
	registrar       *authz.Registrar
	db              *bun.DB
	declarationOnly bool
}

// AdministratorGuard is a consumer-side interface satisfied by
// iam.AdministratorGuard. Defined here rather than imported to keep the
// dependency direction organization → iam unidirectional.
type AdministratorGuard interface {
	Protect(context.Context, bun.IDB, string, string) (func() error, error)
}

func NewModule(db *bun.DB, registrar *authz.Registrar, audit auditx.Appender, members ActiveMemberChecker, administrators AdministratorGuard, nextID IDGenerator, credentials InvitationCredentials, createPrincipal InvitationPrincipalCreator) *Module {
	if db == nil || registrar == nil || audit == nil || members == nil || administrators == nil || nextID == nil || credentials == nil || createPrincipal == nil {
		panic("organization module requires database, authz registrar, audit appender, active member checker, administrator guard, id generator, invitation credentials, and principal creator")
	}
	return &Module{service: NewService(db, audit, nextID), tenants: NewTenantService(db, audit, nextID), positions: NewPositionService(db, audit, members, administrators, nextID), groups: NewGroupService(db, audit, members, administrators, nextID), invitations: NewInvitationService(db, audit, nextID, credentials, createPrincipal), registrar: registrar, db: db}
}

func NewDeclarationOnlyModule(registrar *authz.Registrar) *Module {
	if registrar == nil || !registrar.IsDeclarationOnly() {
		panic("organization declaration module requires declaration-only authz registrar")
	}
	return &Module{registrar: registrar, declarationOnly: true}
}

func (m *Module) Migrate(ctx context.Context) error {
	if m.declarationOnly {
		return nil
	}
	return Migrate(ctx, m.db)
}

func (m *Module) RegisterREST(api huma.API) {
	RegisterTenantREST(api, m.tenants, m.registrar)
	RegisterREST(api, m.service, m.registrar)
	RegisterPositionREST(api, m.positions, m.registrar)
	RegisterGroupREST(api, m.groups, m.registrar)
	RegisterInvitationREST(api, m.invitations, m.registrar)
}

func (m *Module) ProvisioningGroups() *GroupService { return m.groups }
