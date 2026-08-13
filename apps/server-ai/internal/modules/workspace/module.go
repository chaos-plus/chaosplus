// Package workspace coordinates independently owned workspace bounded contexts.
package workspace

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/attachment"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/defect"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/objective"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/requirement"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/task"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/testcase"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/testrun"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/goosex"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

//go:embed .migrations/sql/*/*.sql
var legacyMigrations embed.FS

type Dependencies struct {
	Database           *bun.DB
	NextID             func() (guid.ID, error)
	Authorization      *authz.Registrar
	WorkflowLauncher   task.WorkflowLauncher
	Events             task.EventSink
	Blobs              attachment.BlobStore
	ConversationExists func(context.Context, guid.ID) error
}

type Modules struct {
	Items       []any
	Attachments *attachment.Module
}

type legacyMigration struct{ db *bun.DB }

func newLegacyMigration(db *bun.DB) *legacyMigration { return &legacyMigration{db: db} }

func (m *legacyMigration) Migrate(ctx context.Context) error {
	if m == nil || m.db == nil {
		return fmt.Errorf("workspace legacy migration requires database")
	}
	migrations, err := fs.Sub(legacyMigrations, ".migrations")
	if err != nil {
		return fmt.Errorf("resolve workspace legacy migrations: %w", err)
	}
	return goosex.Run(ctx, m.db.DB, migrations, m.db.Dialect().Name().String(), "goose_ai_workspace")
}

func NewModules(dependencies Dependencies) (*Modules, error) {
	if dependencies.Database == nil || dependencies.NextID == nil || dependencies.Authorization == nil || dependencies.Blobs == nil || dependencies.ConversationExists == nil {
		return nil, errors.New("workspace modules require database, ID generator, authorization, blob storage, and conversation references")
	}
	objectiveModule := objective.NewModule(dependencies.Database, dependencies.NextID, dependencies.Authorization)
	requirementModule := requirement.NewModule(dependencies.Database, dependencies.NextID, dependencies.Authorization, objectiveModule.References())
	objectiveModule.SetKeyResultUsage(requirementModule.References())
	taskModule := task.NewModule(dependencies.Database, dependencies.NextID, dependencies.Authorization, dependencies.WorkflowLauncher, dependencies.Events)
	testCaseModule := testcase.NewModule(dependencies.Database, dependencies.NextID, dependencies.Authorization, requirementModule.References())
	testRunModule := testrun.NewModule(dependencies.Database, dependencies.NextID, dependencies.Authorization, testCaseModule.References())
	defectModule := defect.NewModule(dependencies.Database, dependencies.NextID, dependencies.Authorization, requirementModule.References(), taskModule.References(), testCaseModule.References(), testRunModule.References())
	attachmentModule := attachment.NewModule(dependencies.Database, dependencies.NextID, dependencies.Authorization, dependencies.Blobs, resourceReferences{
		objectives: objectiveModule.Service(), requirements: requirementModule.References(), tasks: taskModule.References(),
		testCases: testCaseModule.References(), defects: defectModule.Service(), conversationExists: dependencies.ConversationExists,
	})
	legacyMigration := newLegacyMigration(dependencies.Database)
	return &Modules{Items: []any{
		objectiveModule,
		requirementModule,
		taskModule,
		testCaseModule,
		testRunModule,
		defectModule,
		attachmentModule,
		legacyMigration,
	}, Attachments: attachmentModule}, nil
}

type resourceReferences struct {
	objectives         *objective.Service
	requirements       *requirement.BunRepository
	tasks              *task.BunRepository
	testCases          *testcase.BunRepository
	defects            *defect.Service
	conversationExists func(context.Context, guid.ID) error
}

func (r resourceReferences) Exists(ctx context.Context, resourceType attachment.ResourceType, id guid.ID) error {
	var err error
	switch resourceType {
	case attachment.ResourceObjective:
		_, err = r.objectives.Get(ctx, id)
	case attachment.ResourceRequirement:
		err = r.requirements.Exists(ctx, id)
	case attachment.ResourceTask:
		err = r.tasks.Exists(ctx, id)
	case attachment.ResourceTestCase:
		err = r.testCases.Exists(ctx, id)
	case attachment.ResourceDefect:
		_, err = r.defects.Get(ctx, id)
	case attachment.ResourceConversation:
		err = r.conversationExists(ctx, id)
	default:
		return attachment.ErrInvalid
	}
	if errors.Is(err, objective.ErrNotFound) || errors.Is(err, requirement.ErrNotFound) || errors.Is(err, task.ErrNotFound) ||
		errors.Is(err, testcase.ErrNotFound) || errors.Is(err, defect.ErrNotFound) {
		return attachment.ErrNotFound
	}
	return err
}

func Actions() []authz.Action {
	actions := make([]authz.Action, 0, len(requirement.Actions)+len(task.Actions)+len(objective.Actions)+len(testcase.Actions)+len(testrun.Actions)+len(defect.Actions)+len(attachment.Actions))
	actions = append(actions, requirement.Actions...)
	actions = append(actions, task.Actions...)
	actions = append(actions, objective.Actions...)
	actions = append(actions, testcase.Actions...)
	actions = append(actions, testrun.Actions...)
	actions = append(actions, defect.Actions...)
	actions = append(actions, attachment.Actions...)
	return actions
}

func RegisterI18n() error {
	return errors.Join(requirement.RegisterI18n(), task.RegisterI18n(), objective.RegisterI18n(), testcase.RegisterI18n(), testrun.RegisterI18n(), defect.RegisterI18n(), attachment.RegisterI18n())
}
