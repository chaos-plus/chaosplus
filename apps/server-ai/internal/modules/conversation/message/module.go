package message

import (
	"context"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/secure"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/nats-io/nats.go"
	"github.com/uptrace/bun"
)

const activationTimeout = 5 * time.Second

type Module struct {
	db         *bun.DB
	repository *BunRepository
	service    *Service
	hub        *Hub
	registrar  *authz.Registrar
	origin     secure.OriginPolicy
}

func NewModule(db *bun.DB, nextID func() (guid.ID, error), registrar *authz.Registrar, origin secure.OriginPolicy, nc *nats.Conn, channels ChannelAccess, attachments AttachmentDirectory) *Module {
	if registrar == nil {
		panic("message module requires authorization registrar")
	}
	repository := NewRepository(db, nextID)
	hub := NewHub(nc)
	return &Module{db: db, repository: repository, service: NewService(repository, channels, attachments, hub, nextID), hub: hub, registrar: registrar, origin: origin}
}

func (m *Module) Migrate(ctx context.Context) error { return Migrate(ctx, m.db) }
func (m *Module) Start(ctx context.Context) error {
	if err := m.repository.RebuildProjectionIfInconsistent(ctx); err != nil {
		return err
	}
	return m.hub.Start(ctx)
}
func (m *Module) Stop(ctx context.Context) error { return m.hub.Stop(ctx) }
func (m *Module) Service() *Service              { return m.service }
