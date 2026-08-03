package audit

import (
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/danielgtaylor/huma/v2"
	"github.com/uptrace/bun"
)

type Module struct {
	service   *Service
	registrar *authz.Registrar
}

func NewModule(db *bun.DB, registrar *authz.Registrar) *Module {
	if db == nil || registrar == nil {
		panic("audit module requires database and authz registrar")
	}
	return &Module{service: NewService(db), registrar: registrar}
}

func NewDeclarationOnlyModule(registrar *authz.Registrar) *Module {
	if registrar == nil || !registrar.IsDeclarationOnly() {
		panic("audit declaration module requires declaration-only registrar")
	}
	return &Module{registrar: registrar}
}

func (m *Module) RegisterREST(api huma.API) {
	RegisterREST(api, m.service, m.registrar)
}
