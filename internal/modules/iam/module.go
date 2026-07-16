package iam

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/uptrace/bun"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	iamapi "github.com/chaos-plus/chaosplus/internal/modules/iam/api"
)

// Module exposes the first IAM management surface. It is intentionally read-only
// for now: SpiceDB remains the future source of truth for grants, while these
// endpoints let the admin UI discover the catalog and scope model.
type Module struct {
	service         *Service
	registrar       *authz.Registrar
	db              *bun.DB
	worker          *OutboxWorker
	declarationOnly bool
}

func NewModule(db *bun.DB, registrar *authz.Registrar, writer RelationshipWriter, nextID IDGenerator, cfg OutboxConfig) *Module {
	if db == nil || registrar == nil || writer == nil || nextID == nil {
		panic("iam module requires database, authz registrar, relationship writer, and id generator")
	}
	repo := NewRepository(db, nextID)
	worker := NewOutboxWorker(repo, writer, cfg, nextID)
	return &Module{
		service:   NewService(registrar.Registry(), repo, worker),
		registrar: registrar,
		db:        db,
		worker:    worker,
	}
}

func NewDeclarationOnlyModule(registrar *authz.Registrar) *Module {
	if registrar == nil || !registrar.IsDeclarationOnly() {
		panic("iam declaration module requires a declaration-only authz registrar")
	}
	return &Module{service: newDeclarationService(registrar.Registry()), registrar: registrar, declarationOnly: true}
}

func (m *Module) Migrate(ctx context.Context) error {
	if m.declarationOnly {
		return nil
	}
	return Migrate(ctx, m.db)
}

func (m *Module) Start(ctx context.Context) error {
	if m.declarationOnly {
		return nil
	}
	return m.worker.Start(ctx)
}

func (m *Module) Stop(context.Context) error {
	if !m.declarationOnly {
		m.worker.Stop()
	}
	return nil
}

func (m *Module) RegisterREST(api huma.API) {
	iamapi.RegisterREST(api, m.service, m.registrar)
}
