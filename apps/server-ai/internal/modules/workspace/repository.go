package workspace

import "context"

// Repository is the persistence port owned by the workspace domain.
type Repository interface {
	CreateWorkItem(context.Context, *WorkItem) error
	ListWorkItems(context.Context, string, string, string) ([]WorkItem, error)
	GetWorkItem(context.Context, string) (*WorkItem, error)
	UpdateWorkItem(context.Context, *WorkItem) error
	DeleteWorkItem(context.Context, string) error

	CreateAttachment(context.Context, *Attachment) error
	ListAttachments(context.Context, string, string) ([]Attachment, error)
	GetAttachment(context.Context, string) (*Attachment, error)

	CreateOkr(context.Context, *Okr) error
	ListOkrs(context.Context) ([]Okr, error)
	GetOkr(context.Context, string) (*Okr, error)
	UpdateOkr(context.Context, *Okr) error
	DeleteOkr(context.Context, string) error
}

// Executor is the workflow application port. Implementations may continue work
// after the request context ends, but must preserve its tenant scope.
type Executor interface {
	ExecuteWorkItem(context.Context, *WorkItem) (string, error)
}

// Notifier is the conversation integration port.
type Notifier interface {
	WorkItemChanged(context.Context, *WorkItem, string)
}

type noopNotifier struct{}

func (noopNotifier) WorkItemChanged(context.Context, *WorkItem, string) {}
