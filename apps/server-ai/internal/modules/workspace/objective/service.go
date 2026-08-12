package objective

import (
	"context"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

type Service struct{ repository Repository }

func NewService(repository Repository) *Service {
	if repository == nil {
		panic("objective service requires repository")
	}
	return &Service{repository: repository}
}

func (s *Service) Create(ctx context.Context, input CreateInput) (*Objective, error) {
	value := &Objective{Title: input.Title, Description: input.Description, PeriodStart: input.PeriodStart, PeriodEnd: input.PeriodEnd, Status: StatusDraft}
	if err := validate(value); err != nil {
		return nil, err
	}
	if err := validateKeyResults(input.KeyResults); err != nil {
		return nil, err
	}
	if err := s.repository.Create(ctx, value, input.KeyResults); err != nil {
		return nil, err
	}
	return value, nil
}

func (s *Service) List(ctx context.Context, status Status) ([]Objective, error) {
	if status != "" {
		probe := &Objective{Title: "status", PeriodStart: 1, PeriodEnd: 1, Status: status}
		if validate(probe) != nil {
			return nil, ErrInvalid
		}
	}
	return s.repository.List(ctx, status)
}

func (s *Service) Get(ctx context.Context, id guid.ID) (*Objective, error) {
	if id.Zero() {
		return nil, ErrInvalid
	}
	return s.repository.Get(ctx, id)
}

func (s *Service) Update(ctx context.Context, id guid.ID, input UpdateInput) (*Objective, error) {
	if id.Zero() || input.Version < 1 {
		return nil, ErrInvalid
	}
	value, err := s.repository.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if value.Version != input.Version {
		return nil, ErrVersionConflict
	}
	if input.Title != nil {
		value.Title = *input.Title
	}
	if input.Description != nil {
		value.Description = *input.Description
	}
	if input.PeriodStart != nil {
		value.PeriodStart = *input.PeriodStart
	}
	if input.PeriodEnd != nil {
		value.PeriodEnd = *input.PeriodEnd
	}
	if input.Status != nil {
		value.Status = *input.Status
	}
	if input.OwnerID != nil {
		if input.OwnerID.Zero() {
			return nil, ErrInvalid
		}
		value.OwnerID = *input.OwnerID
	}
	results := make([]KeyResultInput, len(value.KeyResults))
	for i, result := range value.KeyResults {
		results[i] = KeyResultInput{Title: result.Title, TargetValue: result.TargetValue, CurrentValue: result.CurrentValue, Unit: result.Unit}
	}
	if input.KeyResults != nil {
		results = *input.KeyResults
	}
	if err := validate(value); err != nil {
		return nil, err
	}
	if err := validateKeyResults(results); err != nil {
		return nil, err
	}
	if err := s.repository.Update(ctx, value, results, input.Version); err != nil {
		return nil, err
	}
	return value, nil
}

func (s *Service) Delete(ctx context.Context, id guid.ID, version int64) error {
	if id.Zero() || version < 1 {
		return ErrInvalid
	}
	return s.repository.Delete(ctx, id, version)
}
