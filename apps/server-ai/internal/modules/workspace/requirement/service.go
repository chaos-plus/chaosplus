package requirement

import (
	"context"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

type Service struct{ repository Repository }

func NewService(repository Repository) *Service {
	if repository == nil {
		panic("requirement service requires repository")
	}
	return &Service{repository: repository}
}

func (s *Service) Create(ctx context.Context, input CreateInput) (*Requirement, error) {
	value := &Requirement{ParentID: input.ParentID, Title: input.Title, Description: input.Description, AcceptanceCriteria: input.AcceptanceCriteria, Status: StatusDraft}
	if err := validate(value); err != nil {
		return nil, err
	}
	if input.ParentID != nil {
		if _, err := s.repository.Get(ctx, *input.ParentID); err != nil {
			return nil, ErrInvalid
		}
	}
	if err := s.repository.Create(ctx, value); err != nil {
		return nil, err
	}
	return value, nil
}

func (s *Service) List(ctx context.Context, status Status, parentID *guid.ID) ([]Requirement, error) {
	if status != "" {
		probe := &Requirement{Title: "probe", Status: status}
		if validate(probe) != nil {
			return nil, ErrInvalid
		}
	}
	return s.repository.List(ctx, status, parentID)
}
func (s *Service) Get(ctx context.Context, id guid.ID) (*Requirement, error) {
	if id.Zero() {
		return nil, ErrInvalid
	}
	return s.repository.Get(ctx, id)
}
func (s *Service) Update(ctx context.Context, id guid.ID, input UpdateInput) (*Requirement, error) {
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
	if input.AcceptanceCriteria != nil {
		value.AcceptanceCriteria = *input.AcceptanceCriteria
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
	if err := validate(value); err != nil {
		return nil, err
	}
	if err := s.repository.Update(ctx, value, input.Version); err != nil {
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
