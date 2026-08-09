package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/store"
	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/workflow"
)

func TestRunManagerListReportsLaunchedRuns(t *testing.T) {
	nc := startTestNATS(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	m := NewRunManager(nc, nil, nil, "runner-1")
	m.baseFactory = func(_ string) workflow.Executor { return &workflow.MockExecutor{} }
	if err := m.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	if got := m.List(); len(got) != 0 {
		t.Fatalf("expected no runs initially, got %+v", got)
	}
	run, err := m.Launch(ctx, LaunchRequest{WorkflowJSON: json.RawMessage(testDefRaw), Workspace: t.TempDir()})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	list := m.List()
	if len(list) != 1 || list[0].ID != run.ID {
		t.Fatalf("List wrong: %+v", list)
	}
}

func TestLaunchRejectsInvalidWorkflow(t *testing.T) {
	nc := startTestNATS(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	m := NewRunManager(nc, nil, nil, "runner-1")
	m.baseFactory = func(_ string) workflow.Executor { return &workflow.MockExecutor{} }
	if err := m.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	if _, err := m.Launch(ctx, LaunchRequest{WorkflowJSON: json.RawMessage(`{bad`), Workspace: t.TempDir()}); err == nil {
		t.Fatal("malformed workflow JSON must be rejected")
	}
	// 合法 JSON 但 DAG 非法(边指向不存在的节点)。
	bad := `{"id":"x","version":"1","nodes":[{"id":"t0","type":"trigger","trigger":{"source":"manual"}}],"edges":[{"from":"t0","to":"ghost"}]}`
	if _, err := m.Launch(ctx, LaunchRequest{WorkflowJSON: json.RawMessage(bad), Workspace: t.TempDir()}); err == nil {
		t.Fatal("invalid DAG must be rejected")
	}
}

func TestNewHandlerServesRunsAndChatRoutes(t *testing.T) {
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()

	nc := startTestNATS(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	m := NewRunManager(nc, nil, st, "runner-1")
	m.baseFactory = func(_ string) workflow.Executor { return &workflow.MockExecutor{} }
	if err := m.Start(ctx); err != nil {
		t.Fatalf("run manager: %v", err)
	}
	cs := NewChatService(st, nil, nil, m, "runner-1", t.TempDir())

	srv := httptest.NewServer(NewHandler(m, nil, cs))
	defer srv.Close()

	for _, path := range []string{"/api/runs", "/api/agents", "/api/channels", "/api/work-items", "/api/okrs"} {
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body := make([]byte, 4)
		n, _ := res.Body.Read(body)
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Errorf("GET %s = %d", path, res.StatusCode)
		}
		// 列表端点必须返回数组,不能是 null(前端会 .map 崩)。
		if n > 0 && body[0] != '[' {
			t.Errorf("GET %s should return a JSON array, got %q", path, body[:n])
		}
	}
}

func TestWorkItemLabelsCoverAllTypesAndStatuses(t *testing.T) {
	types := map[string]string{"requirement": "需求", "bug": "缺陷", "task": "任务", "test": "任务"}
	for in, want := range types {
		if got := workItemTypeLabel(in); got != want {
			t.Errorf("workItemTypeLabel(%q) = %q, want %q", in, got, want)
		}
	}
	statuses := map[string]string{"open": "待办", "in_progress": "进行中", "review": "评审中", "done": "已完成", "weird": "待办"}
	for in, want := range statuses {
		if got := workItemStatusLabel(in); got != want {
			t.Errorf("workItemStatusLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCreateWorkItemFromChannelValidatesAndDefaultsType(t *testing.T) {
	srv, st := newWorkspaceTestServer(t)

	var ch store.Channel
	doJSON(t, "POST", srv.URL+"/api/channels", map[string]any{"name": "dev"}, &ch)

	if code := doJSON(t, "POST", srv.URL+"/api/channels/"+ch.ID+"/work-items", map[string]any{"title": ""}, nil); code != 400 {
		t.Fatalf("empty title should 400, got %d", code)
	}

	var it store.WorkItem
	doJSON(t, "POST", srv.URL+"/api/channels/"+ch.ID+"/work-items", map[string]any{"title": "无类型"}, &it)
	if it.Type != "task" {
		t.Fatalf("type should default to task, got %q", it.Type)
	}
	got, err := st.GetWorkItem(context.Background(), it.ID)
	if err != nil || got.ChannelID != ch.ID {
		t.Fatalf("channel link not persisted: %+v %v", got, err)
	}
}

func TestUploadAttachmentRejectsNonMultipart(t *testing.T) {
	srv, _ := newWorkspaceTestServer(t)
	var it store.WorkItem
	doJSON(t, "POST", srv.URL+"/api/work-items", map[string]any{"type": "task", "title": "t"}, &it)

	res, err := http.Post(srv.URL+"/api/work-items/"+it.ID+"/attachments", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatalf("non-multipart upload should 400, got %d", res.StatusCode)
	}

	// 空 multipart(无 file 字段)同样 400。
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("other", "x")
	_ = mw.Close()
	res2, err := http.Post(srv.URL+"/api/work-items/"+it.ID+"/attachments", mw.FormDataContentType(), &buf)
	if err != nil {
		t.Fatalf("post2: %v", err)
	}
	res2.Body.Close()
	if res2.StatusCode != 400 {
		t.Fatalf("multipart without file should 400, got %d", res2.StatusCode)
	}
}

func TestOkrUpdateRejectsMalformedBody(t *testing.T) {
	srv, _ := newWorkspaceTestServer(t)
	var o store.Okr
	doJSON(t, "POST", srv.URL+"/api/okrs", map[string]any{"title": "Q3"}, &o)

	req, _ := http.NewRequest("PUT", srv.URL+"/api/okrs/"+o.ID, bytes.NewReader([]byte(`{bad`)))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatalf("malformed OKR body should 400, got %d", res.StatusCode)
	}
}

// run 被拒绝/暂停时,工作项应落到 review 而不是 done。
func TestExecuteWorkItemPausedRunLandsInReview(t *testing.T) {
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "rev.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()

	nc := startTestNATS(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	m := NewRunManager(nc, nil, st, "runner-1")
	m.baseFactory = func(_ string) workflow.Executor {
		return &workflow.MockExecutor{RunAgentFn: func(_ context.Context, _ *workflow.Node, _ json.RawMessage) (json.RawMessage, error) {
			return nil, errors.New("agent blew up")
		}}
	}
	if err := m.Start(ctx); err != nil {
		t.Fatalf("run manager: %v", err)
	}
	cs := NewChatService(st, nil, nil, m, "runner-1", t.TempDir())
	mux := http.NewServeMux()
	cs.register(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	var it store.WorkItem
	doJSON(t, "POST", srv.URL+"/api/work-items", map[string]any{"type": "task", "title": "会失败"}, &it)
	if code := doJSON(t, "POST", srv.URL+"/api/work-items/"+it.ID+"/execute", nil, nil); code != 200 {
		t.Fatalf("execute: %d", code)
	}
	waitFor(t, func() bool {
		g, e := st.GetWorkItem(context.Background(), it.ID)
		return e == nil && g.Status == "review"
	}, 20*time.Second)
}

// 长任务:run 还在跑时,工作项应被持续投影为 in_progress + 中间进度。
func TestTrackRunProgressProjectsWhileRunning(t *testing.T) {
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "slow.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()

	nc := startTestNATS(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	m := NewRunManager(nc, nil, st, "runner-1")
	m.baseFactory = func(_ string) workflow.Executor {
		return &workflow.MockExecutor{RunAgentFn: func(c context.Context, _ *workflow.Node, _ json.RawMessage) (json.RawMessage, error) {
			select {
			case <-time.After(5 * time.Second): // 跨过至少两次 2s 轮询
			case <-c.Done():
			}
			return json.RawMessage(`{"ok":true}`), nil
		}}
	}
	if err := m.Start(ctx); err != nil {
		t.Fatalf("run manager: %v", err)
	}
	cs := NewChatService(st, nil, nil, m, "runner-1", t.TempDir())
	mux := http.NewServeMux()
	cs.register(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	var it store.WorkItem
	doJSON(t, "POST", srv.URL+"/api/work-items", map[string]any{"type": "task", "title": "长任务"}, &it)
	var ex struct {
		RunID string `json:"runId"`
	}
	doJSON(t, "POST", srv.URL+"/api/work-items/"+it.ID+"/execute", nil, &ex)

	// 运行途中已有耗时投影。
	waitFor(t, func() bool {
		g, e := st.GetWorkItem(context.Background(), it.ID)
		return e == nil && g.Status == "in_progress" && g.SpentHours > 0
	}, 20*time.Second)

	// 执行完停在终审门:通过后才收敛到完成。
	waitFor(t, func() bool {
		r, ok := m.Get(ex.RunID)
		return ok && r.Status() == RunWaitingApproval
	}, 30*time.Second)
	if err := m.Approve(ex.RunID, "review", true, "", nil); err != nil {
		t.Fatalf("approve: %v", err)
	}
	waitFor(t, func() bool {
		g, e := st.GetWorkItem(context.Background(), it.ID)
		return e == nil && g.Progress == 100 && g.Status == "done"
	}, 30*time.Second)
}
