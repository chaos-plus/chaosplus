package machine

import (
	"context"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	coreid "github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

type ManagedAgent struct {
	ID        coreid.ID `json:"id"`
	Name      string    `json:"name"`
	Runtime   string    `json:"runtime"`
	Status    string    `json:"status"`
	MachineID coreid.ID `json:"machineId"`
}

type AgentDirectory interface {
	ListByMachine(context.Context, coreid.ID) ([]ManagedAgent, error)
	CountByMachine(context.Context) (map[coreid.ID]int, error)
}

type Module struct {
	db         *bun.DB
	repository *BunRepository
	hub        *Hub
	agents     AgentDirectory
	registrar  *authz.Registrar
}

func NewModule(db *bun.DB, hub *Hub, registrar *authz.Registrar) *Module {
	if db == nil || hub == nil || registrar == nil {
		panic("machine module requires database, hub, and authorization registrar")
	}
	repository := NewRepository(db)
	hub.SetRepository(repository)
	return &Module{db: db, repository: repository, hub: hub, registrar: registrar}
}

func (m *Module) Migrate(ctx context.Context) error          { return Migrate(ctx, m.db) }
func (m *Module) Start(ctx context.Context) error            { return m.hub.Start(ctx) }
func (m *Module) Stop(_ context.Context) error               { return m.hub.Close() }
func (m *Module) Repository() Repository                     { return m.repository }
func (m *Module) Hub() *Hub                                  { return m.hub }
func (m *Module) SetAgentDirectory(directory AgentDirectory) { m.agents = directory }
