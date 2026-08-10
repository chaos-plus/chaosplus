package provisioning

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/danielgtaylor/huma/v2"
	"github.com/uptrace/bun"
)

type Module struct {
	service         *Service
	registrar       *authz.Registrar
	db              *bun.DB
	declarationOnly bool
	cfg             Config
	// sync management
	syncCancel context.CancelFunc
	syncDone   chan struct{}
	syncMu     sync.Mutex
}

func NewModule(db *bun.DB, registrar *authz.Registrar, audit auditx.Appender, nextID IDGenerator, identities IdentityProvisioner, groups GroupProvisioner, cfg Config, key []byte) *Module {
	if db == nil || registrar == nil {
		panic("provisioning module requires database and authorization registrar")
	}
	return &Module{
		service:   NewService(db, audit, nextID, identities, groups, cfg, key),
		registrar: registrar,
		db:        db,
		cfg:       cfg,
		syncDone:  make(chan struct{}),
	}
}

func NewDeclarationOnlyModule(registrar *authz.Registrar) *Module {
	if registrar == nil || !registrar.IsDeclarationOnly() {
		panic("provisioning declaration module requires declaration-only authorization registrar")
	}
	return &Module{registrar: registrar, declarationOnly: true}
}

// Start begins the background SCIM sync worker when an interval is configured.
func (m *Module) Start(ctx context.Context) error {
	if m.declarationOnly || m.cfg.SyncInterval <= 0 {
		return nil
	}
	m.syncMu.Lock()
	defer m.syncMu.Unlock()
	ctx, m.syncCancel = context.WithCancel(ctx)
	go m.syncLoop(ctx)
	return nil
}

// Stop cancels the sync worker and waits for it to drain.
func (m *Module) Stop(ctx context.Context) error {
	m.syncMu.Lock()
	defer m.syncMu.Unlock()
	if m.syncCancel != nil {
		m.syncCancel()
		select {
		case <-m.syncDone:
		case <-ctx.Done():
		}
	}
	return nil
}

func (m *Module) syncLoop(ctx context.Context) {
	defer close(m.syncDone)
	ticker := time.NewTicker(m.cfg.SyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.reconcileAll(ctx)
		}
	}
}

func (m *Module) reconcileAll(ctx context.Context) {
	var tenants []string
	if err := m.db.NewSelect().Table("iam_tenants").Column("id").Where("status = 'active'").Scan(ctx, &tenants); err != nil {
		slog.Warn("scim sync: list tenants failed", "error", err)
		return
	}
	for _, tenantID := range tenants {
		targets, err := m.service.ListTargets(ctx, tenantID)
		if err != nil {
			slog.Warn("scim sync: list targets failed", "tenant", tenantID, "error", err)
			continue
		}
		for _, t := range targets {
			if t.Status != TargetActive {
				continue
			}
			n, err := m.service.SyncTarget(ctx, t.TenantID, t.ID)
			if err != nil {
				slog.Warn("scim sync: target reconcile failed", "target", t.ID, "error", err)
				continue
			}
			if n > 0 {
				slog.Info("scim sync: resources pushed", "target", t.ID, "count", n)
			}
		}
	}
}

func (m *Module) Migrate(ctx context.Context) error {
	if m.declarationOnly {
		return nil
	}
	return Migrate(ctx, m.db)
}

func (m *Module) RegisterREST(api huma.API) {
	RegisterAdminREST(api, m.service, m.registrar)
	RegisterTargetREST(api, m.service, m.registrar)
	RegisterSCIMREST(api, m.service)
}
