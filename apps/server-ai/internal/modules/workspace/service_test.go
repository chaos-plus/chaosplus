package workspace

import (
	"context"
	"errors"
	"testing"
)

type memoryRepository struct {
	items       map[string]WorkItem
	okrs        map[string]Okr
	attachments map[string]Attachment
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{items: map[string]WorkItem{}, okrs: map[string]Okr{}, attachments: map[string]Attachment{}}
}

func (r *memoryRepository) CreateWorkItem(_ context.Context, item *WorkItem) error {
	r.items[item.ID] = *item
	return nil
}
func (r *memoryRepository) ListWorkItems(context.Context, string, string, string) ([]WorkItem, error) {
	out := make([]WorkItem, 0, len(r.items))
	for _, item := range r.items {
		out = append(out, item)
	}
	return out, nil
}
func (r *memoryRepository) GetWorkItem(_ context.Context, id string) (*WorkItem, error) {
	item, ok := r.items[id]
	if !ok {
		return nil, ErrNotFound
	}
	return &item, nil
}
func (r *memoryRepository) UpdateWorkItem(_ context.Context, item *WorkItem) error {
	if _, ok := r.items[item.ID]; !ok {
		return ErrNotFound
	}
	r.items[item.ID] = *item
	return nil
}
func (r *memoryRepository) DeleteWorkItem(_ context.Context, id string) error {
	delete(r.items, id)
	return nil
}
func (r *memoryRepository) CreateAttachment(_ context.Context, attachment *Attachment) error {
	r.attachments[attachment.ID] = *attachment
	return nil
}
func (r *memoryRepository) ListAttachments(context.Context, string, string) ([]Attachment, error) {
	return []Attachment{}, nil
}
func (r *memoryRepository) GetAttachment(_ context.Context, id string) (*Attachment, error) {
	attachment, ok := r.attachments[id]
	if !ok {
		return nil, ErrNotFound
	}
	return &attachment, nil
}
func (r *memoryRepository) CreateOkr(_ context.Context, okr *Okr) error {
	r.okrs[okr.ID] = *okr
	return nil
}
func (r *memoryRepository) ListOkrs(context.Context) ([]Okr, error) { return []Okr{}, nil }
func (r *memoryRepository) GetOkr(_ context.Context, id string) (*Okr, error) {
	okr, ok := r.okrs[id]
	if !ok {
		return nil, ErrNotFound
	}
	return &okr, nil
}
func (r *memoryRepository) UpdateOkr(_ context.Context, okr *Okr) error {
	r.okrs[okr.ID] = *okr
	return nil
}
func (r *memoryRepository) DeleteOkr(_ context.Context, id string) error {
	delete(r.okrs, id)
	return nil
}

type executorStub struct {
	runID string
	err   error
}

func (e executorStub) ExecuteWorkItem(context.Context, *WorkItem) (string, error) {
	return e.runID, e.err
}

type notifierSpy struct{ reasons []string }

func (n *notifierSpy) WorkItemChanged(_ context.Context, _ *WorkItem, reason string) {
	n.reasons = append(n.reasons, reason)
}

func TestServiceOwnsWorkItemRulesAndExecutionUseCase(t *testing.T) {
	repo := newMemoryRepository()
	notifier := &notifierSpy{}
	service := NewService(repo, executorStub{runID: "run-1"}, notifier, t.TempDir())

	item, err := service.CreateWorkItem(context.Background(), WorkItem{Title: "  Ship API  ", Type: "task"})
	if err != nil {
		t.Fatal(err)
	}
	if item.Title != "Ship API" || item.Status != "open" || item.ID == "" {
		t.Fatalf("domain normalization failed: %+v", item)
	}

	runID, err := service.ExecuteWorkItem(context.Background(), item.ID)
	if err != nil || runID != "run-1" {
		t.Fatalf("execute = (%q, %v)", runID, err)
	}
	persisted := repo.items[item.ID]
	if persisted.Status != "in_progress" || persisted.WorkflowRunID != "run-1" {
		t.Fatalf("execution projection failed: %+v", persisted)
	}
	if len(notifier.reasons) != 2 || notifier.reasons[0] != "created" || notifier.reasons[1] != "status" {
		t.Fatalf("notifications = %v", notifier.reasons)
	}
	if _, err := service.ExecuteWorkItem(context.Background(), item.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate execution error = %v, want conflict", err)
	}
}

func TestServiceRejectsInvalidDomainValues(t *testing.T) {
	service := NewService(newMemoryRepository(), nil, nil, t.TempDir())
	for _, item := range []WorkItem{
		{Title: ""},
		{Title: "x", Type: "unknown"},
		{Title: "x", Progress: 101},
		{Title: "x", EstimateHours: -1},
	} {
		if _, err := service.CreateWorkItem(context.Background(), item); !errors.Is(err, ErrInvalid) {
			t.Errorf("CreateWorkItem(%+v) error = %v", item, err)
		}
	}
	if _, err := service.CreateOkr(context.Background(), Okr{Title: "Q3", KeyResults: `[{"title":"MAU"}]`}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("malformed key results error = %v", err)
	}
}
