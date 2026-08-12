package identity

import (
	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/danielgtaylor/huma/v2"
	"github.com/uptrace/bun"
)

type Module struct {
	service   *Service
	registrar *authz.Registrar
}

func NewModule(db *bun.DB, registrar *authz.Registrar, audit auditx.Appender, administrators AdministratorGuard, nextID func() (guid.ID, error)) *Module {
	if registrar == nil || administrators == nil || nextID == nil {
		panic("identity module requires authorization registrar, administrator guard, and id generator")
	}
	return NewModuleWithService(NewService(db, audit, administrators, nextID), registrar)
}

func NewModuleWithService(service *Service, registrar *authz.Registrar) *Module {
	if service == nil || registrar == nil {
		panic("identity module requires service and authorization registrar")
	}
	return &Module{service: service, registrar: registrar}
}
func (m *Module) RegisterREST(api huma.API) { RegisterREST(api, m.service, m.registrar) }
