package attachment

import (
	"context"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

type Module struct {
	db        *bun.DB
	service   *Service
	registrar *authz.Registrar
}

func NewModule(db *bun.DB, nextID func() (guid.ID, error), registrar *authz.Registrar, blobs BlobStore, resources ResourceReferences) *Module {
	if registrar == nil {
		panic("attachment module requires authorization registrar")
	}
	return &Module{db: db, service: NewService(NewRepository(db), blobs, resources, nextID), registrar: registrar}
}

func (m *Module) Migrate(ctx context.Context) error { return Migrate(ctx, m.db) }
func (m *Module) Service() *Service                 { return m.service }
