package audit

import (
	"context"
	"fmt"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/danielgtaylor/huma/v2"
	"github.com/uptrace/bun"
)

type Module struct {
	service   *Service
	registrar *authz.Registrar
	db        *bun.DB
	anchorCfg Config
}

func NewModule(db *bun.DB, registrar *authz.Registrar, cfg Config) *Module {
	if db == nil || registrar == nil {
		panic("audit module requires database and authz registrar")
	}
	return &Module{
		service:   NewService(db),
		registrar: registrar,
		db:        db,
		anchorCfg: cfg,
	}
}

// Start wires the anchor store and root signer from the deferred config.
// Config-derived errors surface here instead of panicking at construction.
func (m *Module) Start(ctx context.Context) error {
	if !m.anchorCfg.Anchor.Enabled {
		return nil
	}
	store, err := NewAnchorStore(m.anchorCfg.Anchor)
	if err != nil {
		return fmt.Errorf("audit anchor configuration: %w", err)
	}
	svc := NewServiceWithAnchor(m.db, store)
	if m.anchorCfg.Anchor.SigningKey != "" {
		signer, err := NewRootSigner(m.anchorCfg.Anchor.SigningKey)
		if err != nil {
			return fmt.Errorf("audit anchor signing key: %w", err)
		}
		svc = NewServiceWithAnchorAndSigner(m.db, store, signer)
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
