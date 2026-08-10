package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func openStore(t *testing.T) *Store {
	t.Helper()
	// 临时文件而非 :memory: —— 连接池回收空闲连接会把内存库连表一起丢掉。
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "store.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestWorkItemUpdateCalibrateAndRollup(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)

	parent := &WorkItem{ID: "p1", Type: "requirement", Title: "结算"}
	a := &WorkItem{ID: "c1", Type: "task", Title: "接口", ParentID: "p1", EstimateHours: 2, SpentHours: 3, Progress: 100}
	b := &WorkItem{ID: "c2", Type: "task", Title: "页面", ParentID: "p1", EstimateHours: 4, SpentHours: 1, Progress: 50}
	for _, w := range []*WorkItem{parent, a, b} {
		if err := s.CreateWorkItem(ctx, w); err != nil {
			t.Fatalf("create %s: %v", w.ID, err)
		}
	}

	// 全字段更新。
	a.Title = "接口 v2"
	a.Status = "review"
	a.AssigneeAgent = "ag-1"
	a.ChannelID = "ch-1"
	a.WorkflowRunID = "run-9"
	if err := s.UpdateWorkItem(ctx, a); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := s.GetWorkItem(ctx, "c1")
	if got.Title != "接口 v2" || got.Status != "review" || got.AssigneeAgent != "ag-1" || got.WorkflowRunID != "run-9" {
		t.Fatalf("update lost fields: %+v", got)
	}

	// 汇总:估时 2+4,实耗 3+1,进度 (100+50)/2。
	if err := s.RollupParent(ctx, "p1"); err != nil {
		t.Fatalf("rollup: %v", err)
	}
	p, _ := s.GetWorkItem(ctx, "p1")
	if p.EstimateHours != 6 || p.SpentHours != 4 || p.Progress != 75 {
		t.Fatalf("rollup wrong: est=%v spent=%v prog=%d", p.EstimateHours, p.SpentHours, p.Progress)
	}

	// 无父级 / 无子项时是安全的空操作。
	if err := s.RollupParent(ctx, ""); err != nil {
		t.Fatalf("rollup empty parent: %v", err)
	}
	if err := s.RollupParent(ctx, "c2"); err != nil {
		t.Fatalf("rollup childless: %v", err)
	}

	// 校准:未人工估时的才回填。
	blank := &WorkItem{ID: "c3", Type: "task", Title: "无估时"}
	if err := s.CreateWorkItem(ctx, blank); err != nil {
		t.Fatalf("create blank: %v", err)
	}
	if err := s.CalibrateEstimate(ctx, "c3", 1.25); err != nil {
		t.Fatalf("calibrate: %v", err)
	}
	if g, _ := s.GetWorkItem(ctx, "c3"); g.EstimateHours != 1.25 {
		t.Fatalf("estimate not backfilled: %+v", g)
	}
	// 已有人工估时不被覆盖。
	if err := s.CalibrateEstimate(ctx, "c2", 99); err != nil {
		t.Fatalf("calibrate existing: %v", err)
	}
	if g, _ := s.GetWorkItem(ctx, "c2"); g.EstimateHours != 4 {
		t.Fatalf("manual estimate clobbered: %+v", g)
	}
}

// 关库后所有写操作必须返回错误,不能静默成功。
func TestStoreOperationsFailAfterClose(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "closed.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	checks := map[string]error{
		"CreateWorkItem":        s.CreateWorkItem(ctx, &WorkItem{ID: "x", Title: "t"}),
		"UpdateWorkItem":        s.UpdateWorkItem(ctx, &WorkItem{ID: "x", Title: "t"}),
		"UpdateWorkItemRun":     s.UpdateWorkItemRun(ctx, "x", 1, "open", 0),
		"CalibrateEstimate":     s.CalibrateEstimate(ctx, "x", 1),
		"RollupParent":          s.RollupParent(ctx, "p"),
		"DeleteWorkItem":        s.DeleteWorkItem(ctx, "x"),
		"CreateAttachment":      s.CreateAttachment(ctx, &Attachment{ID: "a", OwnerType: "work_item", OwnerID: "x", Filename: "f", StorePath: "p"}),
		"DeleteAttachment":      s.DeleteAttachment(ctx, "a"),
		"CreateOkr":             s.CreateOkr(ctx, &Okr{ID: "o", Title: "t"}),
		"UpdateOkr":             s.UpdateOkr(ctx, &Okr{ID: "o", Title: "t"}),
		"DeleteOkr":             s.DeleteOkr(ctx, "o"),
		"CreateAgent":           s.CreateAgent(ctx, &AgentSpec{ID: "ag", Name: "n"}),
		"UpdateAgent":           s.UpdateAgent(ctx, &AgentSpec{ID: "ag", Name: "n"}),
		"DeleteAgent":           s.DeleteAgent(ctx, "ag"),
		"CreateChannel":         s.CreateChannel(ctx, &Channel{ID: "ch", Name: "n"}),
		"AddChannelMember":      s.AddChannelMember(ctx, "ch", "m", "agent"),
		"RemoveChannelMember":   s.RemoveChannelMember(ctx, "ch", "m", "agent"),
		"AppendChannelMessage":  s.AppendChannelMessage(ctx, &ChannelMessage{ID: "m1", ChannelID: "ch", IdempotencyKey: "k"}),
		"UpsertMachine":         s.UpsertMachine(ctx, Machine{ID: "m", Address: "127.0.0.1"}),
		"TouchMachineHeartbeat": s.TouchMachineHeartbeat(ctx, "m"),
		"UpdateMachineToken":    s.UpdateMachineToken(ctx, "m", "h"),
		"DeleteMachine":         s.DeleteMachine(ctx, "m"),
	}
	for name, err := range checks {
		if err == nil {
			t.Errorf("%s must fail on a closed store", name)
		}
	}

	// 读操作同样应报错而不是返回空结果。
	if _, err := s.ListWorkItems(ctx, "", "", ""); err == nil {
		t.Error("ListWorkItems must fail on a closed store")
	}
	if _, err := s.GetWorkItem(ctx, "x"); err == nil {
		t.Error("GetWorkItem must fail on a closed store")
	}
	if _, err := s.ListAttachments(ctx, "work_item", "x"); err == nil {
		t.Error("ListAttachments must fail on a closed store")
	}
	if _, err := s.GetAttachment(ctx, "a"); err == nil {
		t.Error("GetAttachment must fail on a closed store")
	}
	if _, err := s.ListOkrs(ctx); err == nil {
		t.Error("ListOkrs must fail on a closed store")
	}
	if _, err := s.GetOkr(ctx, "o"); err == nil {
		t.Error("GetOkr must fail on a closed store")
	}
	if _, err := s.ListAgents(ctx); err == nil {
		t.Error("ListAgents must fail on a closed store")
	}
	if _, err := s.ListChannels(ctx); err == nil {
		t.Error("ListChannels must fail on a closed store")
	}
	if _, err := s.GetChannel(ctx, "ch"); err == nil {
		t.Error("GetChannel must fail on a closed store")
	}
	if _, err := s.ListChannelMembers(ctx, "ch"); err == nil {
		t.Error("ListChannelMembers must fail on a closed store")
	}
	if _, err := s.ListChannelMessages(ctx, "ch", 10); err == nil {
		t.Error("ListChannelMessages must fail on a closed store")
	}
	if _, err := s.ListMachines(ctx); err == nil {
		t.Error("ListMachines must fail on a closed store")
	}
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

func TestReconcileStaleRunningOnlyTouchesInProgress(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)

	running := &WorkItem{ID: "r1", Type: "task", Title: "跑着的", Status: "in_progress", Progress: 30}
	openItem := &WorkItem{ID: "o1", Type: "task", Title: "待办", Status: "open"}
	done := &WorkItem{ID: "d1", Type: "task", Title: "完成", Status: "done", Progress: 100}
	for _, w := range []*WorkItem{running, openItem, done} {
		if err := s.CreateWorkItem(ctx, w); err != nil {
			t.Fatalf("create %s: %v", w.ID, err)
		}
	}

	n, err := s.ReconcileStaleRunning(ctx)
	if err != nil || n != 1 {
		t.Fatalf("reconcile = (%d,%v), want (1,nil)", n, err)
	}
	if g, _ := s.GetWorkItem(ctx, "r1"); g.Status != "review" || g.Progress != 30 {
		t.Fatalf("running item wrong after reconcile: %+v", g)
	}
	if g, _ := s.GetWorkItem(ctx, "o1"); g.Status != "open" {
		t.Fatalf("open item must not change: %+v", g)
	}
	if g, _ := s.GetWorkItem(ctx, "d1"); g.Status != "done" {
		t.Fatalf("done item must not change: %+v", g)
	}

	// 无遗留时为 0,可重复执行。
	if n, err := s.ReconcileStaleRunning(ctx); err != nil || n != 0 {
		t.Fatalf("second reconcile = (%d,%v), want (0,nil)", n, err)
	}
}

func TestSumCostSinceAggregatesReportedCost(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)

	// Append 以服务端时间盖戳(日志时间归服务端),所以这里都是"现在"的事件;
	// 窗口过滤用未来的 since 来验证。
	now := time.Now().UnixMilli()
	rows := []Event{
		{ID: "e1", Type: "spawn-done", IdempotencyKey: "k1", PayloadJSON: `{"type":"spawn-done","costUsd":0.25}`},
		{ID: "e2", Type: "spawn-done", IdempotencyKey: "k2", PayloadJSON: `{"type":"spawn-done","costUsd":0.75}`},
		{ID: "e4", Type: "heartbeat", IdempotencyKey: "k4", PayloadJSON: `{"type":"heartbeat"}`},
	}
	for _, e := range rows {
		if err := s.Append(ctx, e); err != nil {
			t.Fatalf("append %s: %v", e.ID, err)
		}
	}

	total, err := s.SumCostSince(ctx, now-3600*1000)
	if err != nil {
		t.Fatalf("sum: %v", err)
	}
	if total < 0.999 || total > 1.001 {
		t.Fatalf("SumCostSince = %v, want 1.0(心跳无成本,不能计入)", total)
	}

	// 窗口之外返回 0,不能把历史全算进"今日"。
	if future, err := s.SumCostSince(ctx, now+3600*1000); err != nil || future != 0 {
		t.Fatalf("SumCostSince(future) = (%v,%v), want (0,nil)", future, err)
	}

	// 桥接后成本可能嵌在 event 字段里,也要算。
	if err := s.Append(ctx, Event{ID: "e5", TS: now, Type: "spawn-done", IdempotencyKey: "k5", PayloadJSON: `{"event":{"costUsd":0.5}}`}); err != nil {
		t.Fatalf("append nested: %v", err)
	}
	total, _ = s.SumCostSince(ctx, now-3600*1000)
	if total < 1.499 || total > 1.501 {
		t.Fatalf("nested costUsd not counted: %v", total)
	}
}
