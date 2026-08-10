package store

import (
	"context"
	"testing"
)

func TestMachineStoreCRUD(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	m := Machine{ID: "m1", InstanceID: "desktop", Address: "127.0.0.1:8081", Status: "confirmed", TokenHash: "abc"}
	if err := s.UpsertMachine(ctx, m); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	ms, err := s.ListMachines(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(ms) != 1 || ms[0].ID != "m1" {
		t.Fatalf("got %+v, want [m1]", ms)
	}

	// 幂等 upsert(重复 confirm 不报错、不重复)。
	if err := s.UpsertMachine(ctx, m); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	ms, _ = s.ListMachines(ctx)
	if len(ms) != 1 {
		t.Fatalf("upsert duplicated: %d rows", len(ms))
	}

	if err := s.TouchMachineHeartbeat(ctx, "m1"); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	ms, _ = s.ListMachines(ctx)
	if ms[0].LastHeartbeatAt == 0 {
		t.Fatal("heartbeat not written")
	}

	// 手动轮换长期 token。
	if err := s.UpdateMachineToken(ctx, "m1", "newhash"); err != nil {
		t.Fatalf("update token: %v", err)
	}
	ms, _ = s.ListMachines(ctx)
	if ms[0].TokenHash != "newhash" {
		t.Fatalf("token hash not rotated: %+v", ms[0])
	}

	if err := s.DeleteMachine(ctx, "m1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	ms, _ = s.ListMachines(ctx)
	if len(ms) != 0 {
		t.Fatalf("delete failed, %d rows remain", len(ms))
	}
}
