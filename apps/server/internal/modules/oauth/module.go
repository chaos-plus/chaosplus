package oauth

import (
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/chaos-plus/chaosplus/internal/modules/authn"
	"github.com/danielgtaylor/huma/v2"
	"github.com/uptrace/bun"
)

type Module struct {
	service   *Service
	registrar *authz.Registrar
}

func NewModule(db *bun.DB, authn *authn.WebService, registrar *authz.Registrar, nextID func() (guid.ID, error)) *Module {
	return &Module{service: NewService(db, authn, nextID), registrar: registrar}
}

func (m *Module) RegisterREST(api huma.API) { RegisterREST(api, m.service, m.registrar) }
