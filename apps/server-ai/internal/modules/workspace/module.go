// Package workspace coordinates independently owned workspace bounded contexts.
package workspace

import (
	"errors"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/attachment"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/objective"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/requirement"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/task"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

type Dependencies struct {
	Database         *bun.DB
	NextID           func() (guid.ID, error)
	Authorization    *authz.Registrar
	WorkflowLauncher task.WorkflowLauncher
	Events           task.EventSink
	Blobs            attachment.BlobStore
}

type Modules struct {
	Items       []any
	Attachments *attachment.Module
}

func NewModules(dependencies Dependencies) (*Modules, error) {
	if dependencies.Database == nil || dependencies.NextID == nil || dependencies.Authorization == nil || dependencies.Blobs == nil {
		return nil, errors.New("workspace modules require database, ID generator, authorization, and blob storage")
	}
	attachmentModule := attachment.NewModule(dependencies.Database, dependencies.NextID, dependencies.Authorization, dependencies.Blobs)
	return &Modules{Items: []any{
		requirement.NewModule(dependencies.Database, dependencies.NextID, dependencies.Authorization),
		task.NewModule(dependencies.Database, dependencies.NextID, dependencies.Authorization, dependencies.WorkflowLauncher, dependencies.Events),
		objective.NewModule(dependencies.Database, dependencies.NextID, dependencies.Authorization),
		attachmentModule,
	}, Attachments: attachmentModule}, nil
}

func Actions() []authz.Action {
	actions := make([]authz.Action, 0, len(requirement.Actions)+len(task.Actions)+len(objective.Actions)+len(attachment.Actions))
	actions = append(actions, requirement.Actions...)
	actions = append(actions, task.Actions...)
	actions = append(actions, objective.Actions...)
	actions = append(actions, attachment.Actions...)
	return actions
}

func RegisterI18n() error {
	return errors.Join(requirement.RegisterI18n(), task.RegisterI18n(), objective.RegisterI18n(), attachment.RegisterI18n())
}
