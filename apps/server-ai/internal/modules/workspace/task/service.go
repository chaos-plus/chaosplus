package task

import (
	"context"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

type WorkflowLauncher interface {
	LaunchTask(context.Context, Task) (guid.ID, error)
	ExecutionMetrics(context.Context, []guid.ID) (map[guid.ID]ExecutionMetric, error)
}
type ExecutionMetric struct {
	SpentMS      int64
	LatestStatus string
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
	v := &Task{RequirementID: in.RequirementID, ParentID: in.ParentID, Title: in.Title, Description: in.Description, Priority: in.Priority, DueAt: in.DueAt, EstimateMS: in.EstimateMS, AssigneeID: in.AssigneeID, ChannelID: in.ChannelID, WorkflowID: in.WorkflowID, ProjectID: in.ProjectID, Workspace: in.Workspace, Status: StatusOpen}
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
func (s *Service) List(ctx context.Context, st Status, rid *guid.ID) ([]Task, error) {
	if st != "" {
		probe := &Task{Title: "probe", Status: st}
		if validate(probe) != nil {
			return nil, ErrInvalid
		}
	}
	items, err := s.repository.List(ctx, "", nil)
	if err != nil {
		return nil, err
	}
	if err := s.decorate(ctx, items); err != nil {
		return nil, err
	}
	filtered := make([]Task, 0, len(items))
	for _, item := range items {
		if st != "" && item.Status != st || rid != nil && (item.RequirementID == nil || *item.RequirementID != *rid) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered, nil
}
func (s *Service) Get(ctx context.Context, id guid.ID) (*Task, error) {
	if id.Zero() {
		return nil, ErrInvalid
	}
	items, err := s.List(ctx, "", nil)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].ID == id {
			return &items[i], nil
		}
	}
	return nil, ErrNotFound
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
	if in.Priority != nil {
		v.Priority = *in.Priority
	}
	if in.DueAt != nil {
		v.DueAt = *in.DueAt
	}
	if in.EstimateMS != nil {
		v.EstimateMS = *in.EstimateMS
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

func (s *Service) decorate(ctx context.Context, items []Task) error {
	metrics := map[guid.ID]ExecutionMetric{}
	if s.launcher != nil && len(items) > 0 {
		ids := make([]guid.ID, len(items))
		for i := range items {
			ids[i] = items[i].ID
		}
		var err error
		metrics, err = s.launcher.ExecutionMetrics(ctx, ids)
		if err != nil {
			return err
		}
	}
	applyRollups(items, metrics)
	return nil
}

func applyRollups(items []Task, metrics map[guid.ID]ExecutionMetric) {
	byID := make(map[guid.ID]int, len(items))
	children := make(map[guid.ID][]guid.ID)
	for i := range items {
		byID[items[i].ID] = i
		if metric, ok := metrics[items[i].ID]; ok {
			items[i].SpentMS = metric.SpentMS
			switch metric.LatestStatus {
			case "completed":
				items[i].Status = StatusDone
			case "failed", "paused", "waiting_approval":
				items[i].Status = StatusReview
			case "cancelled":
				items[i].Status = StatusCancelled
			case "running":
				items[i].Status = StatusInProgress
			}
		}
		if items[i].Status == StatusDone {
			items[i].Progress = 100
		} else {
			items[i].Progress = 0
		}
		if items[i].ParentID != nil {
			children[*items[i].ParentID] = append(children[*items[i].ParentID], items[i].ID)
		}
	}
	visited := make(map[guid.ID]bool, len(items))
	active := make(map[guid.ID]bool, len(items))
	var rollup func(guid.ID)
	rollup = func(id guid.ID) {
		if visited[id] || active[id] {
			return
		}
		active[id] = true
		childIDs := children[id]
		if len(childIDs) > 0 {
			var estimate, spent, weighted int64
			progressSum, childCount := 0, 0
			for _, childID := range childIDs {
				index, ok := byID[childID]
				if !ok || active[childID] {
					continue
				}
				rollup(childID)
				child := items[index]
				estimate += child.EstimateMS
				spent += child.SpentMS
				weighted += child.EstimateMS * int64(child.Progress)
				progressSum += child.Progress
				childCount++
			}
			if index, ok := byID[id]; ok && childCount > 0 {
				items[index].EstimateMS, items[index].SpentMS = estimate, spent
				if estimate > 0 {
					items[index].Progress = int(weighted / estimate)
				} else {
					items[index].Progress = progressSum / childCount
				}
			}
		}
		active[id], visited[id] = false, true
	}
	// ponytail: O(n) in-memory rollup is intentional while task lists are unpaginated; move to a projection when measured entity size requires it.
	for id := range byID {
		rollup(id)
	}
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
