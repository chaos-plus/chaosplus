package artifact

import (
	"context"
	"time"

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

func NewModule(db *bun.DB, nextID func() (guid.ID, error), reader ArtifactReader, scopes ScopeDirectory, reconcileInterval time.Duration, registrar *authz.Registrar) *Module {
	if registrar == nil {
		panic("artifact module requires authorization registrar")
	}
	repository := NewBunRepository(db, nextID)
	return &Module{db: db, repository: repository, service: NewService(repository, reader, scopes, reconcileInterval), registrar: registrar}
}

func (m *Module) Migrate(ctx context.Context) error { return Migrate(ctx, m.db) }
func (m *Module) Start(ctx context.Context) error {
	go m.service.Run(ctx)
	return nil
}
func (m *Module) Repository() *BunRepository { return m.repository }
func (m *Module) Service() *Service          { return m.service }
