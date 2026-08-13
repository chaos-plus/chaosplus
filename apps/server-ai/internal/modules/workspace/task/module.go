package task

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

func NewModule(db *bun.DB, nextID func() (guid.ID, error), registrar *authz.Registrar, launcher WorkflowLauncher, events EventSink) *Module {
	if registrar == nil {
		panic("task module requires authorization registrar")
	}
	repository := NewRepository(db, nextID)
	return &Module{db: db, repository: repository, service: NewService(repository, launcher, events), registrar: registrar}
}
func (m *Module) Migrate(ctx context.Context) error { return Migrate(ctx, m.db) }
func (m *Module) Service() *Service                 { return m.service }
func (m *Module) References() *BunRepository        { return m.repository }
