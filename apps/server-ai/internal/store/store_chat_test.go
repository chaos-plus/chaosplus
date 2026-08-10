package store

import (
	"context"
	"testing"
)

func TestAgentCRUD(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)

	a := &AgentSpec{ID: "ag-1", Name: "dev", Runtime: "claude", SystemPrompt: "你是后端"}
	if err := s.CreateAgent(ctx, a); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.GetAgent(ctx, "ag-1")
	if err != nil || got.Name != "dev" || got.Runtime != "claude" {
		t.Fatalf("get wrong: %+v %v", got, err)
	}
	got.SystemPrompt = "你是前端"
	if err := s.UpdateAgent(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	if g, _ := s.GetAgent(ctx, "ag-1"); g.SystemPrompt != "你是前端" {
		t.Fatalf("update not applied: %+v", g)
	}
	list, _ := s.ListAgents(ctx)
	if len(list) != 1 {
		t.Fatalf("list wrong: %+v", list)
	}
	if err := s.DeleteAgent(ctx, "ag-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetAgent(ctx, "ag-1"); err == nil {
		t.Fatal("deleted agent still found")
	}
}

func TestChannelMembersMessages(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)

	c := &Channel{ID: "ch-1", Name: "dev"}
	if err := s.CreateChannel(ctx, c); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	chs, _ := s.ListChannels(ctx)
	if len(chs) != 1 || chs[0].Name != "dev" {
		t.Fatalf("list channels wrong: %+v", chs)
	}
	if _, err := s.GetChannel(ctx, "ch-1"); err != nil {
		t.Fatalf("get channel: %v", err)
	}

	if err := s.AddChannelMember(ctx, "ch-1", "ag-1", "agent"); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if err := s.AddChannelMember(ctx, "ch-1", "ag-1", "agent"); err != nil { // 幂等
		t.Fatalf("add member dup: %v", err)
	}
	members, _ := s.ListChannelMembers(ctx, "ch-1")
	if len(members) != 1 {
		t.Fatalf("members wrong: %+v", members)
	}
	if err := s.RemoveChannelMember(ctx, "ch-1", "ag-1", "agent"); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	members, _ = s.ListChannelMembers(ctx, "ch-1")
	if len(members) != 0 {
		t.Fatalf("members not removed: %+v", members)
	}

	msg := &ChannelMessage{ID: "msg-1", ChannelID: "ch-1", AuthorKind: "human", IdempotencyKey: "ch-1:1", PayloadJSON: `{"text":"hi"}`}
	if err := s.AppendChannelMessage(ctx, msg); err != nil {
		t.Fatalf("append: %v", err)
	}
	msgs, _ := s.ListChannelMessages(ctx, "ch-1", 10)
	if len(msgs) != 1 || msgs[0].ID != "msg-1" {
		t.Fatalf("messages wrong: %+v", msgs)
	}
	// 幂等:同一 idempotency key 不重复。
	if err := s.AppendChannelMessage(ctx, msg); err != nil {
		t.Fatalf("append dup: %v", err)
	}
	msgs, _ = s.ListChannelMessages(ctx, "ch-1", 10)
	if len(msgs) != 1 {
		t.Fatalf("duplicate inserted: %+v", msgs)
	}
}
