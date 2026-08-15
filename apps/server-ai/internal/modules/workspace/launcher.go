package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workflow"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/task"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

type workflowLauncher struct {
	repository *workflow.BunRepository
	manager    *workflow.RunManager
}

func (launcher workflowLauncher) ExecutionMetrics(ctx context.Context, taskIDs []guid.ID) (map[guid.ID]task.ExecutionMetric, error) {
	wanted := make(map[guid.ID]struct{}, len(taskIDs))
	for _, id := range taskIDs {
		wanted[id] = struct{}{}
	}
	runs, err := launcher.repository.ListRunMetrics(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().UnixMilli()
	result := make(map[guid.ID]task.ExecutionMetric)
	for _, run := range runs {
		var value struct {
			TaskID guid.ID `json:"taskId"`
		}
		if json.Unmarshal([]byte(run.ContextJSON), &value) != nil || value.TaskID.Zero() {
			continue
		}
		if _, ok := wanted[value.TaskID]; !ok {
			continue
		}
		end := run.UpdatedAt
		if run.Status == workflow.RunRunning || run.Status == workflow.RunWaitingApproval {
			end = now
		}
		metric := result[value.TaskID]
		if end > run.CreatedAt {
			metric.SpentMS += end - run.CreatedAt
		}
		metric.LatestStatus = string(run.Status)
		result[value.TaskID] = metric
	}
	return result, nil
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
