package agent

import (
	"context"
	"strings"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

type Service struct{ repository Repository }

func NewService(repository Repository) *Service {
	if repository == nil {
		panic("agent service requires repository")
	}
	return &Service{repository: repository}
}

func (s *Service) Create(ctx context.Context, input CreateInput) (*Agent, error) {
	value := &Agent{Name: input.Name, Kind: input.Kind, Runtime: input.Runtime, Model: input.Model, Provider: input.Provider, SystemPrompt: input.SystemPrompt, Description: input.Description, MachineID: input.MachineID, Status: StatusStopped}
	if err := validate(value); err != nil {
		return nil, err
	}
	if err := s.repository.Create(ctx, value); err != nil {
		return nil, err
	}
	return value, nil
}

func (s *Service) List(ctx context.Context) ([]Agent, error) { return s.repository.List(ctx) }

func (s *Service) Get(ctx context.Context, id guid.ID) (*Agent, error) {
	if id.Zero() {
		return nil, ErrInvalid
	}
	return s.repository.Get(ctx, id)
}

// IsAvailable exposes the narrow cross-module capability needed when another
// bounded context binds an active conversation agent.
func (s *Service) IsAvailable(ctx context.Context, id guid.ID) (bool, error) {
	value, err := s.Get(ctx, id)
	if err != nil {
		return false, err
	}
	return value.Status != StatusRetired && value.DeletedAt == 0, nil
}

func (s *Service) Update(ctx context.Context, id guid.ID, input UpdateInput) (*Agent, error) {
	if id.Zero() || input.Version < 1 {
		return nil, ErrInvalid
	}
	value, err := s.repository.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if value.Version != input.Version || value.Status == StatusRetired {
		return nil, ErrVersionConflict
	}
	if input.Name != nil {
		value.Name = *input.Name
	}
	if input.Kind != nil {
		value.Kind = *input.Kind
	}
	if input.Runtime != nil {
		value.Runtime = *input.Runtime
	}
	if input.Model != nil {
		value.Model = *input.Model
	}
	if input.Provider != nil {
		value.Provider = *input.Provider
	}
	if input.SystemPrompt != nil {
		value.SystemPrompt = *input.SystemPrompt
	}
	if input.Description != nil {
		value.Description = *input.Description
	}
	if input.MachineID != nil {
		value.MachineID = *input.MachineID
	}
	if err := validate(value); err != nil {
		return nil, err
	}
	if err := s.repository.Update(ctx, value, input.Version); err != nil {
		return nil, err
	}
	return value, nil
}

func (s *Service) SetStatus(ctx context.Context, id guid.ID, status Status, version int64) (*Agent, error) {
	if id.Zero() || version < 1 || (status != StatusRunning && status != StatusStopped) {
		return nil, ErrInvalid
	}
	value, err := s.repository.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if value.Version != version || value.Status == StatusRetired || value.MachineID.Zero() {
		return nil, ErrStateConflict
	}
	return s.repository.SetLifecycle(ctx, id, status, "", version)
}

func (s *Service) Retire(ctx context.Context, id guid.ID, handover string, version int64) (*Agent, error) {
	handover = strings.TrimSpace(handover)
	if id.Zero() || version < 1 || handover == "" || len(handover) > 65535 {
		return nil, ErrInvalid
	}
	value, err := s.repository.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if value.Version != version || value.Status == StatusRetired {
		return nil, ErrStateConflict
	}
	return s.repository.SetLifecycle(ctx, id, StatusRetired, handover, version)
}

func (s *Service) Delete(ctx context.Context, id guid.ID, version int64) error {
	if id.Zero() || version < 1 {
		return ErrInvalid
	}
	value, err := s.repository.Get(ctx, id)
	if err != nil {
		return err
	}
	if value.Status != StatusRetired {
		return ErrStateConflict
	}
	return s.repository.Delete(ctx, id, version)
}
