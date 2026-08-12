package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	workspace "github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/store"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/workflow"
)

const trackRunMaxDuration = 6 * time.Hour

// WorkspaceExecutor adapts the workspace application port to the workflow/run
// bounded context. It is deliberately outside both domains.
type WorkspaceExecutor struct {
	store    *store.Store
	runs     *RunManager
	notifier workspace.Notifier
	runnerID string
	root     string
}

func NewWorkspaceModule(st *store.Store, runs *RunManager, notifier workspace.Notifier, runnerID, root string) *workspace.Module {
	executor := &WorkspaceExecutor{store: st, runs: runs, notifier: notifier, runnerID: runnerID, root: root}
	artifactRoot := os.Getenv("ARTIFACT_ROOT")
	if artifactRoot == "" {
		artifactRoot = root
	}
	return workspace.NewModule(st, executor, notifier, artifactRoot)
}

func (x *WorkspaceExecutor) ExecuteWorkItem(ctx context.Context, item *workspace.WorkItem) (string, error) {
	if x.runs == nil {
		return "", fmt.Errorf("workflow runtime is unavailable")
	}
	def, contextJSON := taskWorkflowDef(item)
	defJSON, err := json.Marshal(def)
	if err != nil {
		return "", err
	}
	workdir := filepath.Join(x.root, "workitem-"+item.ID)
	if err := os.MkdirAll(workdir, 0o750); err != nil {
		return "", fmt.Errorf("create work item directory: %w", err)
	}
	background := store.WithEntity(context.Background(), store.EntityOf(ctx))
	background = store.WithOwner(background, store.OwnerOf(ctx))
	run, err := x.runs.Launch(background, LaunchRequest{
		WorkflowJSON: defJSON, Context: contextJSON, Workspace: workdir,
		RunnerID: x.runnerID, InstanceID: store.EntityOf(ctx), OwnerID: store.OwnerOf(ctx),
	})
	if err != nil {
		return "", err
	}
	go x.trackRun(background, item, run)
	return run.ID, nil
}

func (x *WorkspaceExecutor) trackRun(ctx context.Context, item *workspace.WorkItem, run *Run) {
	start := time.Now()
	lastProgress, relayed := 0, 0
	deadline := start.Add(trackRunMaxDuration)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		status := run.Status()
		if status == RunCompleted || status == RunFailed || status == RunPaused {
			break
		}
		if time.Now().After(deadline) {
			if err := x.store.UpdateWorkItemRun(ctx, item.ID, lastProgress, "review", time.Since(start).Hours()); err != nil {
				slog.Error("project stalled workspace run", "workItem", item.ID, "err", err)
			}
			item.Status = "review"
			x.notify(ctx, item, "status")
			return
		}
		lastProgress = runProgress(run)
		relayed = x.relayRunEvents(ctx, item, run, relayed)
		if err := x.store.UpdateWorkItemRun(ctx, item.ID, lastProgress, "in_progress", time.Since(start).Hours()); err != nil {
			slog.Error("project workspace run progress", "workItem", item.ID, "err", err)
		}
	}

	final, finalProgress := "done", 100
	if run.Status() == RunFailed || run.Status() == RunPaused {
		final, finalProgress = "review", lastProgress
	}
	x.relayRunEvents(ctx, item, run, relayed)
	spent := time.Since(start).Hours()
	if err := x.store.UpdateWorkItemRun(ctx, item.ID, finalProgress, final, spent); err != nil {
		slog.Error("project workspace run result", "workItem", item.ID, "err", err)
	}
	if final == "done" {
		if err := x.store.CalibrateEstimate(ctx, item.ID, spent); err != nil {
			slog.Warn("calibrate workspace estimate", "workItem", item.ID, "err", err)
		}
	}
	if err := x.store.RollupParent(ctx, item.ParentID); err != nil {
		slog.Warn("roll up workspace parent", "parent", item.ParentID, "err", err)
	}
	item.Status = final
	x.notify(ctx, item, "status")
}

func (x *WorkspaceExecutor) notify(ctx context.Context, item *workspace.WorkItem, reason string) {
	if x.notifier != nil {
		x.notifier.WorkItemChanged(ctx, item, reason)
	}
}

func runProgress(run *Run) int {
	var done, total int
	seen := map[string]bool{}
	for _, event := range run.Events() {
		if event.NodeID == "" || seen[event.NodeID] {
			continue
		}
		seen[event.NodeID] = true
		total++
		if event.Status == workflow.StatusCompleted || event.Status == workflow.StatusFailed {
			done++
		}
	}
	if total == 0 {
		return 0
	}
	return done * 100 / total
}

func (x *WorkspaceExecutor) relayRunEvents(ctx context.Context, item *workspace.WorkItem, run *Run, cursor int) int {
	notifier, ok := x.notifier.(interface {
		RunEvent(context.Context, *workspace.WorkItem, *Run, RunEvent)
	})
	if !ok || item.ChannelID == "" {
		return cursor
	}
	for _, event := range run.Events() {
		if event.Seq <= cursor {
			continue
		}
		cursor = event.Seq
		notifier.RunEvent(ctx, item, run, event)
	}
	return cursor
}

func taskWorkflowDef(item *workspace.WorkItem) (map[string]any, json.RawMessage) {
	def := map[string]any{
		"id": "task-" + item.ID, "version": "1",
		"nodes": []map[string]any{
			{"id": "trigger", "type": "trigger", "trigger": map[string]any{"source": "manual"}},
			{"id": "do", "type": "agent", "agent": map[string]any{
				"id": "do", "role": "executor", "executor": "claude",
				"systemPrompt": "你是任务执行 agent。根据 context 里的任务要求完成工作,并把结果写入 workspace 根目录的 output.json(含 ok、summary、reply)。",
				"outputSpec": map[string]any{
					"produces": []map[string]any{{"id": "output", "path": "output.json", "type": "document"}},
				},
			}},
			{"id": "review", "type": "human_approval", "humanApproval": map[string]any{
				"approvers": "any_human", "timeoutMs": 86400000, "onTimeout": "pause", "onReject": "pause",
			}},
		},
		"edges": []map[string]any{{"from": "trigger", "to": "do"}, {"from": "do", "to": "review"}},
	}
	contextJSON, _ := json.Marshal(map[string]any{"title": item.Title, "description": item.Description, "type": item.Type})
	return def, contextJSON
}
