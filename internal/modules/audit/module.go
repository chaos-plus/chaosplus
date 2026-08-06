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

func NewModule(db *bun.DB, registrar *authz.Registrar, cfg Config) *Module {
	if db == nil || registrar == nil {
		panic("audit module requires database and authz registrar")
	}
	service := NewService(db)
	if cfg.Anchor.Enabled {
		store, err := NewAnchorStore(cfg.Anchor)
		if err != nil {
			panic("audit module: invalid anchor configuration: " + err.Error())
		}
		service = NewServiceWithAnchor(db, store)
	}
	return &Module{service: service, registrar: registrar}
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
