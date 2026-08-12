package audit

import (
	"context"
	"fmt"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/danielgtaylor/huma/v2"
	"github.com/uptrace/bun"
)

type Module struct {
	service   *Service
	registrar *authz.Registrar
	db        *bun.DB
	anchorCfg Config
	nextID    func() (guid.ID, error)
}

func NewModule(db *bun.DB, registrar *authz.Registrar, cfg Config, nextID func() (guid.ID, error)) *Module {
	if db == nil || registrar == nil || nextID == nil {
		panic("audit module requires database, authz registrar, and id generator")
	}
	return &Module{
		service:   NewService(db, nextID),
		registrar: registrar,
		db:        db,
		anchorCfg: cfg,
		nextID:    nextID,
	}
}

// Start wires the anchor store and root signer from the deferred config.
// Config-derived errors surface here instead of panicking at construction.
func (m *Module) Start(_ context.Context) error {
	if !m.anchorCfg.Anchor.Enabled {
		return nil
	}
	store, err := NewAnchorStore(m.anchorCfg.Anchor)
	if err != nil {
		return fmt.Errorf("audit anchor configuration: %w", err)
	}
	svc := NewServiceWithAnchor(m.db, store, m.nextID)
	if m.anchorCfg.Anchor.SigningKey != "" {
		signer, err := NewRootSigner(m.anchorCfg.Anchor.SigningKey)
		if err != nil {
			return fmt.Errorf("audit anchor signing key: %w", err)
		}
		svc = NewServiceWithAnchorAndSigner(m.db, store, signer, m.nextID)
	}
	m.service = svc
	return nil
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
