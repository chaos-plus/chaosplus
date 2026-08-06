package federation

import (
	"context"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	authnmod "github.com/chaos-plus/chaosplus/internal/modules/authn"
	identitymod "github.com/chaos-plus/chaosplus/internal/modules/identity"
	"github.com/danielgtaylor/huma/v2"
	"github.com/uptrace/bun"
)

type Module struct {
	service         *Service
	registrar       *authz.Registrar
	db              *bun.DB
	declarationOnly bool
}

func NewModule(db *bun.DB, registrar *authz.Registrar, audit auditx.Appender, identities *identitymod.Service, authn *authnmod.WebService, cfg Config, key []byte) *Module {
	if db == nil || registrar == nil {
		panic("federation module requires database and authorization registrar")
	}
	return &Module{service: NewService(db, audit, identities, authn, cfg, key), registrar: registrar, db: db}
}

func NewDeclarationOnlyModule(registrar *authz.Registrar) *Module {
	if registrar == nil || !registrar.IsDeclarationOnly() {
		panic("federation declaration module requires declaration-only authorization registrar")
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
