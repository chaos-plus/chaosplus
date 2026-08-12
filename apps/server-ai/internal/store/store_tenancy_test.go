package store

import (
	"context"
	"errors"
	"os"
	"testing"

	workspace "github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace"
)

func TestWorkflowIdentityAndDeleteAreTenantScoped(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	for _, instanceID := range []string{"tenant-a", "tenant-b"} {
		if err := st.SaveWorkflow(ctx, WorkflowDefModel{ID: "same-id", Version: "1", Name: instanceID, DefJSON: `{}`, InstanceID: instanceID}); err != nil {
			t.Fatal(err)
		}
	}
	listA, err := st.ListWorkflows(ctx, "tenant-a")
	if err != nil || len(listA) != 1 || listA[0].Name != "tenant-a" {
		t.Fatalf("tenant-a list = (%+v, %v)", listA, err)
	}
	ctxA := WithEntity(ctx, "tenant-a")
	if err := st.DeleteWorkflow(ctxA, "same-id"); err != nil {
		t.Fatal(err)
	}
	listA, _ = st.ListWorkflows(ctx, "tenant-a")
	listB, _ := st.ListWorkflows(ctx, "tenant-b")
	if len(listA) != 0 || len(listB) != 1 || listB[0].Name != "tenant-b" {
		t.Fatalf("scoped delete leaked: tenant-a=%+v tenant-b=%+v", listA, listB)
	}
}

func TestWorkspaceResourcesAreTenantScopedByID(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctxA := WithEntity(ctx, "tenant-a")
	ctxB := WithEntity(ctx, "tenant-b")
	item := &workspace.WorkItem{ID: "wi-a", Type: "task", Title: "tenant A"}
	okr := &workspace.Okr{ID: "okr-a", Title: "tenant A", KeyResults: "[]"}
	attachmentPath := t.TempDir() + "/attachment"
	if err := os.WriteFile(attachmentPath, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	attachment := &workspace.Attachment{ID: "att-a", OwnerType: "work_item", OwnerID: item.ID, Filename: "secret.txt", StorePath: attachmentPath}
	if err := st.CreateWorkItem(ctxA, item); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateOkr(ctxA, okr); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateAttachment(ctxA, attachment); err != nil {
		t.Fatal(err)
	}

	if _, err := st.GetWorkItem(ctxB, item.ID); !errors.Is(err, workspace.ErrNotFound) {
		t.Fatalf("cross-tenant work item read = %v", err)
	}
	if _, err := st.GetOkr(ctxB, okr.ID); !errors.Is(err, workspace.ErrNotFound) {
		t.Fatalf("cross-tenant OKR read = %v", err)
	}
	if _, err := st.GetAttachment(ctxB, attachment.ID); !errors.Is(err, workspace.ErrNotFound) {
		t.Fatalf("cross-tenant attachment read = %v", err)
	}
	if err := st.DeleteWorkItem(ctxB, item.ID); !errors.Is(err, workspace.ErrNotFound) {
		t.Fatalf("cross-tenant work item delete = %v", err)
	}
	if err := st.DeleteOkr(ctxB, okr.ID); !errors.Is(err, workspace.ErrNotFound) {
		t.Fatalf("cross-tenant OKR delete = %v", err)
	}
	if err := st.DeleteAttachment(ctxB, attachment.ID); !errors.Is(err, workspace.ErrNotFound) {
		t.Fatalf("cross-tenant attachment delete = %v", err)
	}

	if _, err := st.GetWorkItem(ctxA, item.ID); err != nil {
		t.Fatalf("tenant A work item disappeared: %v", err)
	}
	if _, err := st.GetOkr(ctxA, okr.ID); err != nil {
		t.Fatalf("tenant A OKR disappeared: %v", err)
	}
	if _, err := st.GetAttachment(ctxA, attachment.ID); err != nil {
		t.Fatalf("tenant A attachment disappeared: %v", err)
	}
}

func TestConversationResourcesAreTenantScopedByID(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctxA := WithEntity(ctx, "tenant-a")
	ctxB := WithEntity(ctx, "tenant-b")

	agent := &AgentSpec{ID: "agent-a", Name: "A", Runtime: "claude"}
	channel := &Channel{ID: "channel-a", Name: "A"}
	if err := st.CreateAgent(ctxA, agent); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateChannel(ctxA, channel); err != nil {
		t.Fatal(err)
	}
	if err := st.AddChannelMember(ctxA, channel.ID, agent.ID, "agent"); err != nil {
		t.Fatal(err)
	}
	message := &ChannelMessage{ID: "message-a", ChannelID: channel.ID, IdempotencyKey: "tenant-a:1", PayloadJSON: `{}`}
	if err := st.AppendChannelMessage(ctxA, message); err != nil {
		t.Fatal(err)
	}

	if _, err := st.GetAgent(ctxB, agent.ID); err == nil {
		t.Fatal("cross-tenant agent read succeeded")
	}
	if _, err := st.GetChannel(ctxB, channel.ID); err == nil {
		t.Fatal("cross-tenant channel read succeeded")
	}
	if _, err := st.ListChannelMembers(ctxB, channel.ID); err == nil {
		t.Fatal("cross-tenant channel members read succeeded")
	}
	if _, err := st.ListChannelMessages(ctxB, channel.ID, 10); err == nil {
		t.Fatal("cross-tenant channel messages read succeeded")
	}
	if err := st.SetAgentStatus(ctxB, agent.ID, "running"); err != nil {
		t.Fatalf("scoped no-op returned infrastructure error: %v", err)
	}
	if err := st.DeleteAgent(ctxB, agent.ID); err != nil {
		t.Fatalf("scoped no-op returned infrastructure error: %v", err)
	}

	got, err := st.GetAgent(ctxA, agent.ID)
	if err != nil || got.Status == "running" {
		t.Fatalf("cross-tenant agent mutation leaked: %+v %v", got, err)
	}
	if err := st.DeleteChannel(ctxB, channel.ID); err == nil {
		t.Fatal("cross-tenant channel delete succeeded")
	}
	if _, err := st.GetChannel(ctxA, channel.ID); err != nil {
		t.Fatalf("tenant A channel disappeared: %v", err)
	}
	messages, err := st.ListChannelMessages(ctxA, channel.ID, 10)
	if err != nil || len(messages) != 1 {
		t.Fatalf("tenant A messages = %+v, %v", messages, err)
	}
}
