package provisioning

import (
	"context"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/danielgtaylor/huma/v2"
	"github.com/uptrace/bun"
)

type Module struct {
	service         *Service
	registrar       *authz.Registrar
	db              *bun.DB
	declarationOnly bool
}

func NewModule(db *bun.DB, registrar *authz.Registrar, audit auditx.Appender, nextID IDGenerator, identities IdentityProvisioner, groups GroupProvisioner, cfg Config, key []byte) *Module {
	if db == nil || registrar == nil {
		panic("provisioning module requires database and authorization registrar")
	}
	return &Module{service: NewService(db, audit, nextID, identities, groups, cfg, key), registrar: registrar, db: db}
}

func NewDeclarationOnlyModule(registrar *authz.Registrar) *Module {
	if registrar == nil || !registrar.IsDeclarationOnly() {
		panic("provisioning declaration module requires declaration-only authorization registrar")
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
	RegisterAdminREST(api, m.service, m.registrar)
	RegisterTargetREST(api, m.service, m.registrar)
	RegisterSCIMREST(api, m.service)
}
