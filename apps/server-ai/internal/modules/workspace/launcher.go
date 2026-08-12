package workspace

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workflow"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/task"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

type workflowLauncher struct {
	repository *workflow.BunRepository
	manager    *workflow.RunManager
}

// NewWorkflowLauncher coordinates task execution with the workflow bounded
// context without exposing either module's persistence to the other.
func NewWorkflowLauncher(repository *workflow.BunRepository, manager *workflow.RunManager) task.WorkflowLauncher {
	if repository == nil || manager == nil {
		panic("workspace workflow launcher requires workflow repository and manager")
	}
	return workflowLauncher{repository: repository, manager: manager}
}

func (launcher workflowLauncher) LaunchTask(ctx context.Context, value task.Task) (guid.ID, error) {
	if value.WorkflowID == nil || value.ProjectID == nil || value.Workspace == "" {
		return 0, task.ErrStateConflict
	}
	definition, err := launcher.repository.GetWorkflow(ctx, *value.WorkflowID)
	if err != nil {
		return 0, fmt.Errorf("load task workflow: %w", err)
	}
	workflowContext, err := json.Marshal(map[string]any{
		"taskId":        value.ID,
		"requirementId": value.RequirementID,
		"title":         value.Title,
		"description":   value.Description,
	})
	if err != nil {
		return 0, fmt.Errorf("encode task workflow context: %w", err)
	}
	run, err := launcher.manager.Launch(ctx, workflow.LaunchRequest{
		WorkflowJSON: json.RawMessage(definition.DefJSON),
		Workspace:    value.Workspace,
		Context:      workflowContext,
		ProjectID:    *value.ProjectID,
	})
	if err != nil {
		return 0, fmt.Errorf("launch task workflow: %w", err)
	}
	return run.ID, nil
}
