package agent

import (
	"context"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/machine"
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

func NewModule(db *bun.DB, nextID func() (guid.ID, error), registrar *authz.Registrar) *Module {
	if registrar == nil {
		panic("agent module requires authorization registrar")
	}
	repository := NewRepository(db, nextID)
	return &Module{db: db, repository: repository, service: NewService(repository), registrar: registrar}
}
func (m *Module) Migrate(ctx context.Context) error { return Migrate(ctx, m.db) }
func (m *Module) Service() *Service                 { return m.service }
func (m *Module) Directory() machine.AgentDirectory {
	return machineDirectory{repository: m.repository}
}

type machineDirectory struct{ repository *BunRepository }

func (directory machineDirectory) ListByMachine(ctx context.Context, id guid.ID) ([]machine.ManagedAgent, error) {
	items, err := directory.repository.ListByMachine(ctx, id)
	if err != nil {
		return nil, err
	}
	result := make([]machine.ManagedAgent, 0, len(items))
	for _, item := range items {
		result = append(result, machine.ManagedAgent{ID: item.ID, Name: item.Name, Runtime: item.Runtime, Status: string(item.Status), MachineID: item.MachineID})
	}
	return result, nil
}
func (directory machineDirectory) CountByMachine(ctx context.Context) (map[guid.ID]int, error) {
	return directory.repository.CountByMachine(ctx)
}
