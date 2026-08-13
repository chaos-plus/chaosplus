package objective

import (
	"context"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

type Module struct {
	db         *bun.DB
	repository *BunRepository
	service    *Service
	registrar  *authz.Registrar
}

func NewModule(db *bun.DB, nextID func() (guid.ID, error), r *authz.Registrar) *Module {
	if r == nil {
		panic("objective module requires authorization registrar")
	}
	repository := NewRepository(db, nextID)
	return &Module{db: db, repository: repository, service: NewService(repository), registrar: r}
}
func (m *Module) Migrate(ctx context.Context) error      { return Migrate(ctx, m.db) }
func (m *Module) Service() *Service                      { return m.service }
func (m *Module) References() *BunRepository             { return m.repository }
func (m *Module) SetKeyResultUsage(usage KeyResultUsage) { m.service.SetKeyResultUsage(usage) }
