package task

import (
	"context"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

type WorkflowLauncher interface {
	LaunchTask(context.Context, Task) (guid.ID, error)
}
type EventSink interface {
	TaskChanged(context.Context, Task, string) error
}
type Service struct {
	repository Repository
	launcher   WorkflowLauncher
	events     EventSink
}

func NewService(r Repository, l WorkflowLauncher, e EventSink) *Service {
	if r == nil {
		panic("task service requires repository")
	}
	return &Service{repository: r, launcher: l, events: e}
}
func (s *Service) emit(ctx context.Context, v Task, event string) error {
	if s.events == nil {
		return nil
	}
	return s.events.TaskChanged(ctx, v, event)
}
func (s *Service) Create(ctx context.Context, in CreateInput) (*Task, error) {
	v := &Task{RequirementID: in.RequirementID, ParentID: in.ParentID, Kind: in.Kind, Title: in.Title, Description: in.Description, EstimateMS: in.EstimateMS, AssigneeID: in.AssigneeID, ChannelID: in.ChannelID, WorkflowID: in.WorkflowID, ProjectID: in.ProjectID, Workspace: in.Workspace, Status: StatusOpen}
	if err := validate(v); err != nil {
		return nil, err
	}
	if in.ParentID != nil {
		if _, err := s.repository.Get(ctx, *in.ParentID); err != nil {
			return nil, ErrInvalid
		}
	}
	if err := s.repository.Create(ctx, v); err != nil {
		return nil, err
	}
	if err := s.emit(ctx, *v, "created"); err != nil {
		return nil, err
	}
	return v, nil
}
func (s *Service) List(ctx context.Context, k Kind, st Status, rid *guid.ID) ([]Task, error) {
	probe := &Task{Title: "probe", Kind: k, Status: st}
	if k != "" || st != "" {
		if k == "" {
			probe.Kind = KindTask
		}
		if st == "" {
			probe.Status = StatusOpen
		}
		if validate(probe) != nil {
			return nil, ErrInvalid
		}
	}
	return s.repository.List(ctx, k, st, rid)
}
func (s *Service) Get(ctx context.Context, id guid.ID) (*Task, error) {
	if id.Zero() {
		return nil, ErrInvalid
	}
	return s.repository.Get(ctx, id)
}
func (s *Service) Update(ctx context.Context, id guid.ID, in UpdateInput) (*Task, error) {
	if id.Zero() || in.Version < 1 {
		return nil, ErrInvalid
	}
	v, err := s.repository.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if v.Version != in.Version {
		return nil, ErrVersionConflict
	}
	if in.Title != nil {
		v.Title = *in.Title
	}
	if in.Description != nil {
		v.Description = *in.Description
	}
	if in.Status != nil {
		v.Status = *in.Status
	}
	if in.EstimateMS != nil {
		v.EstimateMS = *in.EstimateMS
	}
	if in.SpentMS != nil {
		v.SpentMS = *in.SpentMS
	}
	if in.Progress != nil {
		v.Progress = *in.Progress
	}
	if in.AssigneeID != nil {
		if in.AssigneeID.Zero() {
			return nil, ErrInvalid
		}
		v.AssigneeID = in.AssigneeID
	}
	if in.OwnerID != nil {
		if in.OwnerID.Zero() {
			return nil, ErrInvalid
		}
		v.OwnerID = *in.OwnerID
	}
	if in.WorkflowID != nil {
		if in.WorkflowID.Zero() {
			return nil, ErrInvalid
		}
		v.WorkflowID = in.WorkflowID
	}
	if in.ProjectID != nil {
		if in.ProjectID.Zero() {
			return nil, ErrInvalid
		}
		v.ProjectID = in.ProjectID
	}
	if in.Workspace != nil {
		v.Workspace = *in.Workspace
	}
	if err := validate(v); err != nil {
		return nil, err
	}
	if err := s.repository.Update(ctx, v, in.Version); err != nil {
		return nil, err
	}
	if err := s.emit(ctx, *v, "updated"); err != nil {
		return nil, err
	}
	return v, nil
}
func (s *Service) Delete(ctx context.Context, id guid.ID, version int64) error {
	if id.Zero() || version < 1 {
		return ErrInvalid
	}
	return s.repository.Delete(ctx, id, version)
}
func (s *Service) Execute(ctx context.Context, id guid.ID, version int64) (guid.ID, error) {
	if s.launcher == nil {
		return 0, ErrStateConflict
	}
	v, err := s.Get(ctx, id)
	if err != nil {
		return 0, err
	}
	if v.Version != version || v.Status == StatusInProgress || v.Status == StatusDone || v.Status == StatusCancelled {
		return 0, ErrStateConflict
	}
	if v.WorkflowID == nil || v.ProjectID == nil || v.Workspace == "" {
		return 0, ErrStateConflict
	}
	runID, err := s.launcher.LaunchTask(ctx, *v)
	if err != nil {
		return 0, err
	}
	v.WorkflowRunID = &runID
	v.Status = StatusInProgress
	if err := s.repository.Update(ctx, v, version); err != nil {
		return 0, err
	}
	if err := s.emit(ctx, *v, "executed"); err != nil {
		return 0, err
	}
	return runID, nil
}
