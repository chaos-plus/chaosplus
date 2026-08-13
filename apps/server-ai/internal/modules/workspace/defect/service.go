package defect

import (
	"context"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

type References interface {
	Exists(context.Context, guid.ID) error
}
type Service struct {
	repository   Repository
	requirements References
	tasks        References
	testCases    References
	testRuns     References
}

func NewService(repository Repository, requirements, tasks, testCases, testRuns References) *Service {
	if repository == nil {
		panic("defect service requires repository")
	}
	return &Service{repository: repository, requirements: requirements, tasks: tasks, testCases: testCases, testRuns: testRuns}
}
func (s *Service) validateReferences(ctx context.Context, value *Defect) error {
	for _, item := range []struct {
		id         *guid.ID
		references References
	}{{value.RequirementID, s.requirements}, {value.TaskID, s.tasks}, {value.TestCaseID, s.testCases}, {value.TestRunID, s.testRuns}} {
		if item.id != nil && (item.references == nil || item.references.Exists(ctx, *item.id) != nil) {
			return ErrInvalid
		}
	}
	return nil
}
func (s *Service) Create(ctx context.Context, input CreateInput) (*Defect, error) {
	value := &Defect{RequirementID: input.RequirementID, TaskID: input.TaskID, TestCaseID: input.TestCaseID, TestRunID: input.TestRunID, Title: input.Title, Description: input.Description, ReproductionSteps: input.ReproductionSteps, ExpectedResult: input.ExpectedResult, ActualResult: input.ActualResult, Severity: input.Severity, Priority: input.Priority, Status: StatusOpen, AssigneeID: input.AssigneeID}
	if err := validate(value); err != nil {
		return nil, err
	}
	if err := s.validateReferences(ctx, value); err != nil {
		return nil, err
	}
	if err := s.repository.Create(ctx, value); err != nil {
		return nil, err
	}
	return value, nil
}
func (s *Service) List(ctx context.Context, status Status, severity Severity) ([]Defect, error) {
	if status != "" {
		probe := &Defect{Title: "p", ReproductionSteps: "r", ExpectedResult: "e", ActualResult: "a", Status: status, Severity: SeverityMajor, Priority: PriorityMedium, TaskID: ptr(1)}
		if validate(probe) != nil {
			return nil, ErrInvalid
		}
	}
	if severity != "" {
		switch severity {
		case SeverityBlocker, SeverityCritical, SeverityMajor, SeverityMinor, SeverityTrivial:
		default:
			return nil, ErrInvalid
		}
	}
	return s.repository.List(ctx, status, severity)
}
func ptr(id guid.ID) *guid.ID { return &id }
func (s *Service) Get(ctx context.Context, id guid.ID) (*Defect, error) {
	if id.Zero() {
		return nil, ErrInvalid
	}
	return s.repository.Get(ctx, id)
}
func (s *Service) Update(ctx context.Context, id guid.ID, input UpdateInput) (*Defect, error) {
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
	if input.ReproductionSteps != nil {
		value.ReproductionSteps = *input.ReproductionSteps
	}
	if input.ExpectedResult != nil {
		value.ExpectedResult = *input.ExpectedResult
	}
	if input.ActualResult != nil {
		value.ActualResult = *input.ActualResult
	}
	if input.Severity != nil {
		value.Severity = *input.Severity
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
	if input.Status != nil {
		if !validTransition(value.Status, *input.Status) {
			return nil, ErrStateConflict
		}
		value.Status = *input.Status
		if value.Status == StatusReopened || value.Status == StatusRejected {
			value.Resolution = ""
			value.ResolutionNote = ""
		}
	}
	if input.Resolution != nil {
		value.Resolution = *input.Resolution
	}
	if input.ResolutionNote != nil {
		value.ResolutionNote = *input.ResolutionNote
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
