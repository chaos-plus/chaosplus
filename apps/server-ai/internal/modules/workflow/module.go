package workflow

import (
	"context"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/secure"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/nats-io/nats.go"
	"github.com/uptrace/bun"
)

type Module struct {
	db         *bun.DB
	repository *BunRepository
	manager    *RunManager
	registrar  *authz.Registrar
	origin     secure.OriginPolicy
	nextID     func() (guid.ID, error)
}

func NewModule(db *bun.DB, nc *nats.Conn, link RunnerLink, runnerHandle string, holderID guid.ID, nextID func() (guid.ID, error), registrar *authz.Registrar, origin secure.OriginPolicy, projectors ...EventProjector) *Module {
	if registrar == nil {
		panic("workflow module requires authorization registrar")
	}
	repository := NewBunRepository(db, projectors...)
	return &Module{db: db, repository: repository, manager: NewRunManager(nc, link, repository, runnerHandle, holderID, nextID), registrar: registrar, origin: origin, nextID: nextID}
}
func (m *Module) Migrate(ctx context.Context) error { return Migrate(ctx, m.db) }
func (m *Module) Start(ctx context.Context) error {
	if err := m.manager.Start(ctx); err != nil {
		return err
	}
	return nil
}
func (m *Module) Stop(ctx context.Context) error { return m.manager.Stop(ctx) }
func (m *Module) Repository() *BunRepository     { return m.repository }
func (m *Module) Manager() *RunManager           { return m.manager }
