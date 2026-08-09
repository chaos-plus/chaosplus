package store

import (
	"context"
	"testing"
)

func openStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestWorkItemCRUDWithSubtask(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)

	parent := &WorkItem{ID: "wi-1", Type: "requirement", Title: "登录", Status: "open"}
	if err := s.CreateWorkItem(ctx, parent); err != nil {
		t.Fatalf("create parent: %v", err)
	}
	child := &WorkItem{ID: "wi-2", Type: "task", Title: "写API", ParentID: "wi-1", EstimateHours: 2}
	if err := s.CreateWorkItem(ctx, child); err != nil {
		t.Fatalf("create child: %v", err)
	}

	// 按 parent 筛选出子任务。
	items, err := s.ListWorkItems(ctx, "", "", "wi-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 || items[0].ID != "wi-2" || items[0].ParentID != "wi-1" || items[0].EstimateHours != 2 {
		t.Fatalf("subtask list wrong: %+v", items)
	}

	// run 投影:progress/status/spent 更新不覆盖人工字段。
	if err := s.UpdateWorkItemRun(ctx, "wi-2", 50, "in_progress", 1.5); err != nil {
		t.Fatalf("update run: %v", err)
	}
	got, err := s.GetWorkItem(ctx, "wi-2")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Progress != 50 || got.Status != "in_progress" || got.SpentHours != 1.5 {
		t.Fatalf("run projection wrong: %+v", got)
	}
	if got.EstimateHours != 2 || got.ParentID != "wi-1" {
		t.Fatalf("manual fields clobbered: %+v", got)
	}

	if err := s.DeleteWorkItem(ctx, "wi-2"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	items, _ = s.ListWorkItems(ctx, "", "", "wi-1")
	if len(items) != 0 {
		t.Fatalf("delete failed: %+v", items)
	}
}
