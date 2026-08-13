package testrun

import (
	"context"
	"time"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

type TestCaseReferences interface {
	Exists(context.Context, guid.ID) error
}

type Service struct {
	repository Repository
	testCases  TestCaseReferences
}

func NewService(repository Repository, testCases TestCaseReferences) *Service {
	if repository == nil {
		panic("test run service requires repository")
	}
	return &Service{repository: repository, testCases: testCases}
}

func (s *Service) Create(ctx context.Context, input CreateInput) (*TestRun, error) {
	if input.TestCaseID.Zero() || s.testCases == nil || s.testCases.Exists(ctx, input.TestCaseID) != nil {
		return nil, ErrInvalid
	}
	value := &TestRun{TestCaseID: input.TestCaseID, WorkflowRunID: input.WorkflowRunID, Environment: input.Environment, Status: StatusQueued}
	if err := s.repository.Create(ctx, value); err != nil {
		return nil, err
	}
	return value, nil
}
func (s *Service) List(ctx context.Context, testCaseID *guid.ID, status Status) ([]TestRun, error) {
	if testCaseID != nil && testCaseID.Zero() {
		return nil, ErrInvalid
	}
	if status != "" {
		switch status {
		case StatusQueued, StatusRunning, StatusPassed, StatusFailed, StatusBlocked, StatusCancelled:
		default:
			return nil, ErrInvalid
		}
	}
	return s.repository.List(ctx, testCaseID, status)
}
func (s *Service) Get(ctx context.Context, id guid.ID) (*TestRun, error) {
	if id.Zero() {
		return nil, ErrInvalid
	}
	return s.repository.Get(ctx, id)
}
func (s *Service) Update(ctx context.Context, id guid.ID, input UpdateInput) (*TestRun, error) {
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
	if !validTransition(value.Status, input.Status) {
		return nil, ErrStateConflict
	}
	now := time.Now().UTC().UnixMilli()
	value.Status = input.Status
	if input.Status == StatusRunning {
		value.StartedAt = now
	}
	if terminal(input.Status) {
		value.CompletedAt = now
		if value.StartedAt == 0 {
			value.StartedAt = now
		}
	}
	if input.ObservedResult != nil {
		value.ObservedResult = *input.ObservedResult
	}
	if input.FailureSummary != nil {
		value.FailureSummary = *input.FailureSummary
	}
	if err := validate(value); err != nil {
		return nil, err
	}
	if err := s.repository.Update(ctx, value, input.Version); err != nil {
		return nil, err
	}
	return value, nil
}
