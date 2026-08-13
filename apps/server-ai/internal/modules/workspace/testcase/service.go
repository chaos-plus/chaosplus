package testcase

import (
	"context"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

type RequirementReferences interface {
	Exists(context.Context, guid.ID) error
}

type Service struct {
	repository   Repository
	requirements RequirementReferences
}

func NewService(repository Repository, requirements RequirementReferences) *Service {
	if repository == nil {
		panic("test case service requires repository")
	}
	return &Service{repository: repository, requirements: requirements}
}

func (s *Service) Create(ctx context.Context, input CreateInput) (*TestCase, error) {
	value := &TestCase{RequirementID: input.RequirementID, Title: input.Title, Description: input.Description, Preconditions: input.Preconditions, Priority: input.Priority, Status: StatusDraft, AssigneeID: input.AssigneeID}
	if len(input.Steps) == 0 {
		return nil, ErrInvalid
	}
	if err := validate(value, input.Steps); err != nil || input.AssigneeID != nil && input.AssigneeID.Zero() {
		return nil, ErrInvalid
	}
	if input.RequirementID != nil && (input.RequirementID.Zero() || s.requirements == nil || s.requirements.Exists(ctx, *input.RequirementID) != nil) {
		return nil, ErrInvalid
	}
	if err := s.repository.Create(ctx, value, input.Steps); err != nil {
		return nil, err
	}
	return value, nil
}

func (s *Service) List(ctx context.Context, status Status, requirementID *guid.ID) ([]TestCase, error) {
	if status != "" {
		probe := &TestCase{Title: "probe", Status: status, Priority: PriorityMedium}
		if validate(probe, []StepInput{{Action: "a", ExpectedResult: "b"}}) != nil {
			return nil, ErrInvalid
		}
	}
	return s.repository.List(ctx, status, requirementID)
}

func (s *Service) Get(ctx context.Context, id guid.ID) (*TestCase, error) {
	if id.Zero() {
		return nil, ErrInvalid
	}
	return s.repository.Get(ctx, id)
}

func (s *Service) Update(ctx context.Context, id guid.ID, input UpdateInput) (*TestCase, error) {
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
	steps := make([]StepInput, len(value.Steps))
	for i := range value.Steps {
		steps[i] = StepInput{Action: value.Steps[i].Action, ExpectedResult: value.Steps[i].ExpectedResult}
	}
	if input.Title != nil {
		value.Title = *input.Title
	}
	if input.Description != nil {
		value.Description = *input.Description
	}
	if input.Preconditions != nil {
		value.Preconditions = *input.Preconditions
	}
	if input.Priority != nil {
		value.Priority = *input.Priority
	}
	if input.AssigneeID != nil {
		if input.AssigneeID.Zero() {
			return nil, ErrInvalid
		}
		value.AssigneeID = input.AssigneeID
	}
	if input.OwnerID != nil {
		if input.OwnerID.Zero() {
			return nil, ErrInvalid
		}
		value.OwnerID = *input.OwnerID
	}
	if input.Steps != nil {
		steps = *input.Steps
	}
	if input.Status != nil {
		if !validTransition(value.Status, *input.Status) {
			return nil, ErrStateConflict
		}
		value.Status = *input.Status
	}
	if err := validate(value, steps); err != nil {
		return nil, err
	}
	if err := s.repository.Update(ctx, value, steps, input.Version); err != nil {
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
