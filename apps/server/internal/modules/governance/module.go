package governance

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

func NewModule(db *bun.DB, registrar *authz.Registrar, audit auditx.Appender, grants RoleGrantStore, nextID IDGenerator) *Module {
	if db == nil || registrar == nil {
		panic("governance module requires database and authz registrar")
	}
	return &Module{service: NewService(db, audit, grants, nextID), registrar: registrar, db: db}
}

func NewDeclarationOnlyModule(registrar *authz.Registrar) *Module {
	if registrar == nil || !registrar.IsDeclarationOnly() {
		panic("governance declaration module requires a declaration-only authz registrar")
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
	RegisterREST(api, m.service, m.registrar)
}
