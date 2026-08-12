package server

import (
	"context"
	"fmt"
	"log/slog"

	workspace "github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/store"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/workflow"
)

func (cs *ChatService) WorkItemChanged(ctx context.Context, item *workspace.WorkItem, reason string) {
	if item.ChannelID == "" {
		return
	}
	text := fmt.Sprintf("%s #%s「%s」", workItemTypeLabel(item.Type), item.ID, item.Title)
	if reason == "created" {
		text += " 已创建(" + workItemStatusLabel(item.Status) + ")"
	} else if reason == "status" {
		text += " 状态 → " + workItemStatusLabel(item.Status)
	}
	cs.publishWorkflowMessage(ctx, item.ChannelID, fmt.Sprintf("%s:workflow:%s", item.ChannelID, randHex(8)), map[string]any{"text": text})
}

func (cs *ChatService) RunEvent(ctx context.Context, item *workspace.WorkItem, run *Run, event RunEvent) {
	payload := map[string]any{"runId": run.ID, "nodeId": event.NodeID, "workItemId": item.ID}
	switch {
	case event.Status == workflow.StatusWaitingApproval && event.NodeID != "":
		payload["kind"] = "approval"
		payload["text"] = fmt.Sprintf("节点「%s」等待人工审批", event.NodeID)
		payload["summary"] = item.Title
		payload["artifacts"] = runArtifacts(run, event.NodeID)
	case event.NodeID == "" && event.Status == workflow.StatusRunning:
		payload["kind"] = "run"
		payload["status"] = "running"
		payload["text"] = fmt.Sprintf("工作流已启动(%s)", run.ID)
	case event.Status == workflow.StatusFailed:
		payload["kind"] = "run"
		payload["status"] = "failed"
		payload["text"] = fmt.Sprintf("节点「%s」失败:%s", event.NodeID, event.Error)
	case event.NodeID != "" && event.Status == workflow.StatusCompleted:
		payload["kind"] = "run"
		payload["status"] = "completed"
		payload["text"] = fmt.Sprintf("节点「%s」完成", event.NodeID)
	default:
		return
	}
	cs.publishWorkflowMessage(ctx, item.ChannelID, fmt.Sprintf("%s:%s:%d", item.ChannelID, run.ID, event.Seq), payload)
}

func (cs *ChatService) publishWorkflowMessage(ctx context.Context, channelID, key string, payload map[string]any) {
	message := &store.ChannelMessage{ID: "msg-" + randHex(8), ChannelID: channelID,
		AuthorMemberID: "workflow", AuthorKind: "workflow", IdempotencyKey: key, PayloadJSON: mustJSON(payload)}
	if err := cs.st.AppendChannelMessage(ctx, message); err != nil {
		slog.Warn("append workspace channel message", "channel", channelID, "err", err)
		return
	}
	cs.hub.publish(channelID, *message)
}

func workItemTypeLabel(value string) string {
	switch value {
	case "requirement":
		return "需求"
	case "bug":
		return "缺陷"
	default:
		return "任务"
	}
}

func workItemStatusLabel(value string) string {
	switch value {
	case "in_progress":
		return "进行中"
	case "review":
		return "评审中"
	case "done":
		return "已完成"
	default:
		return "待办"
	}
}

func runArtifacts(run *Run, nodeID string) []string {
	out := []string{}
	if run.Def == nil {
		return out
	}
	upstream := map[string]bool{nodeID: true}
	for _, edge := range run.Def.Edges {
		if edge.To == nodeID {
			upstream[edge.From] = true
		}
	}
	seen := map[string]bool{}
	for i := range run.Def.Nodes {
		node := &run.Def.Nodes[i]
		if !upstream[node.ID] || node.Agent == nil || node.Agent.OutputSpec == nil {
			continue
		}
		for _, artifact := range node.Agent.OutputSpec.Produces {
			if artifact.Path != "" && !seen[artifact.Path] {
				seen[artifact.Path] = true
				out = append(out, artifact.Path)
			}
		}
	}
	return out
}
