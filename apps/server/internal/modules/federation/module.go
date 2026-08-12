package federation

import (
	"context"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	authnmod "github.com/chaos-plus/chaosplus/internal/modules/authn"
	identitymod "github.com/chaos-plus/chaosplus/internal/modules/identity"
	"github.com/danielgtaylor/huma/v2"
	"github.com/uptrace/bun"
)

type Module struct {
	service         *Service
	registrar       *authz.Registrar
	db              *bun.DB
	cfg             Config
	declarationOnly bool
}

func NewModule(db *bun.DB, registrar *authz.Registrar, audit auditx.Appender, identities *identitymod.Service, authn *authnmod.WebService, cfg Config, key []byte, nextID func() (guid.ID, error)) *Module {
	if db == nil || registrar == nil || nextID == nil {
		panic("federation module requires database, authorization registrar, and id generator")
	}
	return &Module{service: NewService(db, audit, identities, authn, cfg, key, nextID), registrar: registrar, db: db, cfg: cfg}
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

// Start initializes the SAML identity provider signing key after migrations
// have run so generated keys can be persisted safely.
func (m *Module) Start(ctx context.Context) error {
	if m.declarationOnly {
		return nil
	}
	return m.service.StartSAML(ctx, m.cfg.SAML)
}

func (m *Module) RegisterREST(api huma.API) {
	RegisterREST(api, m.service, m.registrar)
	RegisterSAMLREST(api, m.service, m.registrar)
}
