package server

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/store"
	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/workflow"
)

// newWorkspaceTestServer wires a real store + RunManager (mock executor) behind
// the real HTTP mux, so these tests exercise the production routes.
func newWorkspaceTestServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	// 用临时文件而非 :memory: —— 连接池回收空闲连接会把内存库连同表一起丢掉。
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	nc := startTestNATS(t)
	ctx, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)

	rm := NewRunManager(nc, nil, st, "runner-1")
	rm.baseFactory = func(_ string) workflow.Executor { return &workflow.MockExecutor{} }
	if err := rm.Start(ctx); err != nil {
		t.Fatalf("run manager: %v", err)
	}

	cs := NewChatService(st, nil, nil, rm, "runner-1", t.TempDir())
	mux := http.NewServeMux()
	cs.register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, st
}

func doJSON(t *testing.T, method, url string, body any, out any) int {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatalf("req: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer res.Body.Close()
	if out != nil {
		_ = json.NewDecoder(res.Body).Decode(out)
	}
	return res.StatusCode
}

func TestWorkItemHTTPCRUDAndSubtaskFilter(t *testing.T) {
	srv, _ := newWorkspaceTestServer(t)

	var parent store.WorkItem
	if code := doJSON(t, "POST", srv.URL+"/api/work-items", map[string]any{"type": "requirement", "title": "login"}, &parent); code != 201 {
		t.Fatalf("create parent: %d", code)
	}
	if parent.ID == "" {
		t.Fatalf("bad parent: %+v", parent)
	}

	var child store.WorkItem
	if code := doJSON(t, "POST", srv.URL+"/api/work-items",
		map[string]any{"type": "task", "title": "api", "parentId": parent.ID, "estimateHours": 3}, &child); code != 201 {
		t.Fatalf("create child: %d", code)
	}

	// parent 过滤只返回子任务。
	var kids []store.WorkItem
	if code := doJSON(t, "GET", srv.URL+"/api/work-items?parent="+parent.ID, nil, &kids); code != 200 {
		t.Fatalf("list kids: %d", code)
	}
	if len(kids) != 1 || kids[0].ID != child.ID || kids[0].EstimateHours != 3 {
		t.Fatalf("subtask filter wrong: %+v", kids)
	}

	// type 过滤。
	var reqs []store.WorkItem
	doJSON(t, "GET", srv.URL+"/api/work-items?type=requirement", nil, &reqs)
	if len(reqs) != 1 || reqs[0].ID != parent.ID {
		t.Fatalf("type filter wrong: %+v", reqs)
	}

	// 单查 + 更新 + 删除。
	var got store.WorkItem
	if code := doJSON(t, "GET", srv.URL+"/api/work-items/"+child.ID, nil, &got); code != 200 || got.Title != "api" {
		t.Fatalf("get: %d %+v", code, got)
	}
	if code := doJSON(t, "PUT", srv.URL+"/api/work-items/"+child.ID, map[string]any{"title": "api v2", "status": "review"}, nil); code != 200 {
		t.Fatalf("update: %d", code)
	}
	doJSON(t, "GET", srv.URL+"/api/work-items/"+child.ID, nil, &got)
	if got.Title != "api v2" || got.Status != "review" {
		t.Fatalf("update not applied: %+v", got)
	}
	if code := doJSON(t, "DELETE", srv.URL+"/api/work-items/"+child.ID, nil, nil); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	if code := doJSON(t, "GET", srv.URL+"/api/work-items/"+child.ID, nil, nil); code != 404 {
		t.Fatalf("expected 404 after delete, got %d", code)
	}
}

func TestWorkItemValidationErrors(t *testing.T) {
	srv, _ := newWorkspaceTestServer(t)

	if code := doJSON(t, "POST", srv.URL+"/api/work-items", map[string]any{"title": ""}, nil); code != 400 {
		t.Fatalf("empty title should 400, got %d", code)
	}
	if code := doJSON(t, "PUT", srv.URL+"/api/work-items/nope", map[string]any{"title": "x"}, nil); code != 404 {
		t.Fatalf("update missing should 404, got %d", code)
	}
	if code := doJSON(t, "POST", srv.URL+"/api/work-items/nope/execute", nil, nil); code != 404 {
		t.Fatalf("execute missing should 404, got %d", code)
	}
}

func TestExecuteWorkItemDrivesWorkflowAndProjectsProgress(t *testing.T) {
	srv, st := newWorkspaceTestServer(t)

	var it store.WorkItem
	doJSON(t, "POST", srv.URL+"/api/work-items", map[string]any{"type": "task", "title": "ship it"}, &it)

	var ex struct {
		RunID string `json:"runId"`
	}
	if code := doJSON(t, "POST", srv.URL+"/api/work-items/"+it.ID+"/execute", nil, &ex); code != 200 {
		t.Fatalf("execute: %d", code)
	}
	if ex.RunID == "" {
		t.Fatal("no runId returned")
	}

	// 立刻置为 in_progress 且绑定 run。
	got, err := st.GetWorkItem(context.Background(), it.ID)
	if err != nil || got.WorkflowRunID != ex.RunID {
		t.Fatalf("run not bound: %+v %v", got, err)
	}

	// run 完成后投影回 done/100 并校准估时。
	waitFor(t, func() bool {
		g, e := st.GetWorkItem(context.Background(), it.ID)
		return e == nil && g.Status == "done" && g.Progress == 100
	}, 20*time.Second)

	g, _ := st.GetWorkItem(context.Background(), it.ID)
	if g.EstimateHours <= 0 {
		t.Fatalf("estimate not calibrated from actual: %+v", g)
	}
}

func TestTaskWorkflowDefIsValid(t *testing.T) {
	it := &store.WorkItem{ID: "wi-x", Type: "task", Title: "t", Description: "d"}
	defMap, ctxJSON := taskWorkflowDef(it)

	raw, err := json.Marshal(defMap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var def workflow.WorkflowDef
	if err := json.Unmarshal(raw, &def); err != nil {
		t.Fatalf("unmarshal def: %v", err)
	}
	if _, err := workflow.NewEngine(&def, &workflow.MockExecutor{}); err != nil {
		t.Fatalf("generated def must pass engine validation: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(ctxJSON, &got); err != nil {
		t.Fatalf("ctx json: %v", err)
	}
	if got["title"] != "t" || got["description"] != "d" {
		t.Fatalf("context missing work item fields: %v", got)
	}
}

func TestAttachmentUploadServeAndList(t *testing.T) {
	srv, _ := newWorkspaceTestServer(t)

	var it store.WorkItem
	doJSON(t, "POST", srv.URL+"/api/work-items", map[string]any{"type": "bug", "title": "crash"}, &it)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "note.txt")
	_, _ = fw.Write([]byte("attachment body"))
	_ = mw.Close()

	res, err := http.Post(srv.URL+"/api/work-items/"+it.ID+"/attachments", mw.FormDataContentType(), &buf)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	var att store.Attachment
	_ = json.NewDecoder(res.Body).Decode(&att)
	res.Body.Close()
	if res.StatusCode != 201 || att.ID == "" || att.Filename != "note.txt" || att.SizeBytes != 15 {
		t.Fatalf("upload wrong: %d %+v", res.StatusCode, att)
	}

	// 下载返回原始内容。
	dl, err := http.Get(srv.URL + "/api/attachments/" + att.ID)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	body := make([]byte, 64)
	n, _ := dl.Body.Read(body)
	dl.Body.Close()
	if string(body[:n]) != "attachment body" {
		t.Fatalf("content mismatch: %q", body[:n])
	}

	// 列表按 owner 返回。
	var list []store.Attachment
	doJSON(t, "GET", srv.URL+"/api/work-items/"+it.ID+"/attachments", nil, &list)
	if len(list) != 1 || list[0].ID != att.ID {
		t.Fatalf("list wrong: %+v", list)
	}

	// 未知 id 404(存储路径只来自 DB,客户端无法指定)。
	if code := doJSON(t, "GET", srv.URL+"/api/attachments/att-missing", nil, nil); code != 404 {
		t.Fatalf("missing attachment should 404, got %d", code)
	}

	// 缺 file 字段 → 400。
	if code := doJSON(t, "POST", srv.URL+"/api/work-items/"+it.ID+"/attachments", map[string]any{}, nil); code != 400 {
		t.Fatalf("missing file should 400, got %d", code)
	}
}

func TestOkrHTTPCRUD(t *testing.T) {
	srv, _ := newWorkspaceTestServer(t)

	var o store.Okr
	if code := doJSON(t, "POST", srv.URL+"/api/okrs",
		map[string]any{"title": "Q3", "objective": "growth", "period": "2026-Q3"}, &o); code != 201 {
		t.Fatalf("create: %d", code)
	}
	if o.ID == "" || o.KeyResults != "[]" {
		t.Fatalf("bad okr: %+v", o)
	}
	if code := doJSON(t, "POST", srv.URL+"/api/okrs", map[string]any{"title": ""}, nil); code != 400 {
		t.Fatalf("empty title should 400, got %d", code)
	}

	if code := doJSON(t, "PUT", srv.URL+"/api/okrs/"+o.ID,
		map[string]any{"title": "Q3", "objective": "revenue", "keyResults": `[{"title":"MAU","target":10,"progress":5,"unit":"w"}]`}, nil); code != 200 {
		t.Fatalf("update: %d", code)
	}

	var list []store.Okr
	doJSON(t, "GET", srv.URL+"/api/okrs", nil, &list)
	if len(list) != 1 || list[0].Objective != "revenue" {
		t.Fatalf("list wrong: %+v", list)
	}

	if code := doJSON(t, "DELETE", srv.URL+"/api/okrs/"+o.ID, nil, nil); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	doJSON(t, "GET", srv.URL+"/api/okrs", nil, &list)
	if len(list) != 0 {
		t.Fatalf("not deleted: %+v", list)
	}
}

func TestChannelWorkItemNotifiesSubscribers(t *testing.T) {
	srv, st := newWorkspaceTestServer(t)

	var ch store.Channel
	doJSON(t, "POST", srv.URL+"/api/channels", map[string]any{"name": "dev"}, &ch)

	var it store.WorkItem
	if code := doJSON(t, "POST", srv.URL+"/api/channels/"+ch.ID+"/work-items",
		map[string]any{"type": "bug", "title": "npe on save"}, &it); code != 201 {
		t.Fatalf("create from channel: %d", code)
	}
	if it.ChannelID != ch.ID {
		t.Fatalf("channel not linked: %+v", it)
	}

	msgs, err := st.ListChannelMessages(context.Background(), ch.ID, 50)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	found := false
	for _, m := range msgs {
		if m.AuthorKind == "workflow" && bytes.Contains([]byte(m.PayloadJSON), []byte("npe on save")) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no subscription notification in channel: %+v", msgs)
	}

	// 状态变更也要通知。
	before := len(msgs)
	doJSON(t, "PUT", srv.URL+"/api/work-items/"+it.ID, map[string]any{"title": it.Title, "status": "done", "channelId": ch.ID}, nil)
	msgs, _ = st.ListChannelMessages(context.Background(), ch.ID, 50)
	if len(msgs) <= before {
		t.Fatal("status change did not notify channel")
	}
}
