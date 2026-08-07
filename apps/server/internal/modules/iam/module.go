package iam

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/uptrace/bun"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
)

// Module exposes IAM management backed by the primary relational database.
type Module struct {
	service         *Service
	registrar       *authz.Registrar
	db              *bun.DB
	declarationOnly bool
}

func NewModule(db *bun.DB, registrar *authz.Registrar, checker AuthorizationEvaluator, audit auditx.Appender, nextID IDGenerator) *Module {
	if db == nil || registrar == nil || checker == nil || audit == nil || nextID == nil {
		panic("iam module requires database, authz registrar, authorizer, audit appender, and id generator")
	}
	repo := NewRepository(db, nextID)
	return &Module{
		service:   NewService(registrar.Registry(), repo, checker, audit),
		registrar: registrar,
		db:        db,
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

func (m *Module) RegisterREST(api huma.API) {
	RegisterREST(api, m.service, m.registrar)
}
