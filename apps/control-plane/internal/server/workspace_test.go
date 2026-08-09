package server

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/store"
	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/workflow"
)

// newWorkspaceTestServer wires a real store + RunManager (mock executor) behind
// the real HTTP mux, so these tests exercise the production routes.
func newWorkspaceTestServerWithManager(t *testing.T) (*httptest.Server, *store.Store, *RunManager) {
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
	return srv, st, rm
}

func newWorkspaceTestServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	srv, st, _ := newWorkspaceTestServerWithManager(t)
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
	srv, st, rm := newWorkspaceTestServerWithManager(t)

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

	// PRD V1-M2:执行完成后停在人工审批,工作项此时还不能是 done。
	waitFor(t, func() bool {
		for _, r := range rm.List() {
			if r.ID == ex.RunID && r.Status() == RunWaitingApproval {
				return true
			}
		}
		return false
	}, 20*time.Second)
	if g, _ := st.GetWorkItem(context.Background(), it.ID); g.Status == "done" {
		t.Fatal("work item must not be done before the approval gate is resolved")
	}

	// 通过审批 → run 完成 → 投影回 done/100 并校准估时。
	// 审批路由挂在 NewHandler 上,这里直接走 RunManager(同一条 broker 路径)。
	if err := rm.Approve(ex.RunID, "review", true, "", nil); err != nil {
		t.Fatalf("approve: %v", err)
	}
	// 校准发生在状态落盘之后,等校准值本身,别只等 done(否则测试抢跑)。
	waitFor(t, func() bool {
		g, e := st.GetWorkItem(context.Background(), it.ID)
		return e == nil && g.Status == "done" && g.Progress == 100 && g.EstimateHours > 0
	}, 20*time.Second)
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

// 部分更新:只带 status 的 PUT 不能清掉估时/进度/父级(前端就是这么调的)。
func TestUpdateWorkItemIsPartialAndPreservesUntouchedFields(t *testing.T) {
	srv, st := newWorkspaceTestServer(t)

	var parent store.WorkItem
	doJSON(t, "POST", srv.URL+"/api/work-items", map[string]any{"type": "requirement", "title": "父"}, &parent)
	var it store.WorkItem
	doJSON(t, "POST", srv.URL+"/api/work-items",
		map[string]any{"type": "task", "title": "子", "parentId": parent.ID, "estimateHours": 3, "description": "细节"}, &it)
	if err := st.UpdateWorkItemRun(context.Background(), it.ID, 40, "in_progress", 1.25); err != nil {
		t.Fatalf("seed run projection: %v", err)
	}

	if code := doJSON(t, "PUT", srv.URL+"/api/work-items/"+it.ID, map[string]any{"status": "review"}, nil); code != 200 {
		t.Fatalf("status-only PUT: %d", code)
	}

	got, err := st.GetWorkItem(context.Background(), it.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != "review" {
		t.Fatalf("status not applied: %+v", got)
	}
	if got.EstimateHours != 3 || got.Progress != 40 || got.SpentHours != 1.25 {
		t.Fatalf("status-only PUT wiped run/estimate fields: %+v", got)
	}
	if got.ParentID != parent.ID || got.Description != "细节" || got.Type != "task" || got.Title != "子" {
		t.Fatalf("status-only PUT wiped manual fields: %+v", got)
	}

	// 显式传空字符串描述是有意清空,应生效。
	doJSON(t, "PUT", srv.URL+"/api/work-items/"+it.ID, map[string]any{"description": ""}, nil)
	got, _ = st.GetWorkItem(context.Background(), it.ID)
	if got.Description != "" {
		t.Fatalf("explicit empty description should clear it: %+v", got)
	}

	// 自引用父级被拒。
	if code := doJSON(t, "PUT", srv.URL+"/api/work-items/"+it.ID, map[string]any{"parentId": it.ID}, nil); code != 400 {
		t.Fatalf("self-parent should 400, got %d", code)
	}
}

// 附件回放必须按扩展名判定类型,不能信客户端 Content-Type(否则同源存储型 XSS)。
func TestAttachmentServingIgnoresClientMimeAndHardensHeaders(t *testing.T) {
	srv, _ := newWorkspaceTestServer(t)
	var it store.WorkItem
	doJSON(t, "POST", srv.URL+"/api/work-items", map[string]any{"type": "task", "title": "t"}, &it)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="file"; filename="evil.html"`)
	h.Set("Content-Type", "text/html") // 攻击者声明的类型
	part, _ := mw.CreatePart(h)
	_, _ = part.Write([]byte("<script>alert(1)</script>"))
	_ = mw.Close()

	res, err := http.Post(srv.URL+"/api/work-items/"+it.ID+"/attachments", mw.FormDataContentType(), &buf)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	var att store.Attachment
	_ = json.NewDecoder(res.Body).Decode(&att)
	res.Body.Close()
	if att.Mime == "text/html" {
		t.Fatalf("client-declared text/html must not be stored verbatim: %+v", att)
	}

	dl, err := http.Get(srv.URL + "/api/attachments/" + att.ID)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer dl.Body.Close()
	if ct := dl.Header.Get("Content-Type"); strings.HasPrefix(ct, "text/html") {
		t.Fatalf("html must not be served as text/html, got %q", ct)
	}
	if dl.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing X-Content-Type-Options: nosniff")
	}
	if cd := dl.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") {
		t.Errorf("unsafe type must download, not render; got %q", cd)
	}

	// 图片仍应内联展示。
	var ibuf bytes.Buffer
	imw := multipart.NewWriter(&ibuf)
	ifw, _ := imw.CreateFormFile("file", "pic.png")
	_, _ = ifw.Write([]byte("\x89PNG\r\n\x1a\n"))
	_ = imw.Close()
	ires, _ := http.Post(srv.URL+"/api/work-items/"+it.ID+"/attachments", imw.FormDataContentType(), &ibuf)
	var iatt store.Attachment
	_ = json.NewDecoder(ires.Body).Decode(&iatt)
	ires.Body.Close()
	idl, _ := http.Get(srv.URL + "/api/attachments/" + iatt.ID)
	defer idl.Body.Close()
	if idl.Header.Get("Content-Type") != "image/png" {
		t.Errorf("png should serve as image/png, got %q", idl.Header.Get("Content-Type"))
	}
	if !strings.HasPrefix(idl.Header.Get("Content-Disposition"), "inline") {
		t.Errorf("png should be inline, got %q", idl.Header.Get("Content-Disposition"))
	}
}

func TestOkrRejectsMalformedKeyResults(t *testing.T) {
	srv, _ := newWorkspaceTestServer(t)

	for _, bad := range []string{`{"not":"an array"}`, `[{"target":1,"progress":0,"unit":"x"}]`, `[{"title":"MAU","unit":"x"}]`} {
		if code := doJSON(t, "POST", srv.URL+"/api/okrs", map[string]any{"title": "Q3", "keyResults": bad}, nil); code != 400 {
			t.Errorf("keyResults %s should 400, got %d", bad, code)
		}
	}
	var o store.Okr
	if code := doJSON(t, "POST", srv.URL+"/api/okrs",
		map[string]any{"title": "Q3", "keyResults": `[{"title":"MAU","target":100,"progress":40,"unit":"万"}]`}, &o); code != 201 {
		t.Fatalf("valid keyResults should succeed, got %d", code)
	}
	if code := doJSON(t, "PUT", srv.URL+"/api/okrs/"+o.ID, map[string]any{"title": "Q3", "keyResults": `[{"bad":1}]`}, nil); code != 400 {
		t.Fatalf("malformed update should 400, got %d", code)
	}
}

// 启动对账:重启后遗留的 in_progress 必须被解开,否则 /execute 永远 409。
func TestReconcileStaleRunningUnsticksWorkItems(t *testing.T) {
	srv, st := newWorkspaceTestServer(t)
	ctx := context.Background()

	var it store.WorkItem
	doJSON(t, "POST", srv.URL+"/api/work-items", map[string]any{"type": "task", "title": "卡住的"}, &it)
	if err := st.UpdateWorkItemRun(ctx, it.ID, 30, "in_progress", 0.5); err != nil {
		t.Fatalf("seed: %v", err)
	}

	n, err := st.ReconcileStaleRunning(ctx)
	if err != nil || n != 1 {
		t.Fatalf("reconcile = (%d,%v), want (1,nil)", n, err)
	}
	got, _ := st.GetWorkItem(ctx, it.ID)
	if got.Status != "review" {
		t.Fatalf("stale in_progress not reconciled: %+v", got)
	}
	if got.Progress != 30 {
		t.Fatalf("reconcile must not fabricate progress: %+v", got)
	}
}

func TestUploadRejectsOversizeBody(t *testing.T) {
	srv, _ := newWorkspaceTestServer(t)
	var it store.WorkItem
	doJSON(t, "POST", srv.URL+"/api/work-items", map[string]any{"type": "task", "title": "t"}, &it)

	// 超过 maxUploadBytes 必须被拒,而不是写满磁盘。
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "big.bin")
	_, _ = fw.Write(bytes.Repeat([]byte("A"), maxUploadBytes+1024))
	_ = mw.Close()

	res, err := http.Post(srv.URL+"/api/work-items/"+it.ID+"/attachments", mw.FormDataContentType(), &buf)
	if err == nil {
		defer res.Body.Close()
		if res.StatusCode == 201 {
			t.Fatal("oversize upload must be rejected")
		}
	}
}

func TestUpdateWorkItemRejectsMalformedBody(t *testing.T) {
	srv, _ := newWorkspaceTestServer(t)
	var it store.WorkItem
	doJSON(t, "POST", srv.URL+"/api/work-items", map[string]any{"type": "task", "title": "t"}, &it)

	req, _ := http.NewRequest("PUT", srv.URL+"/api/work-items/"+it.ID, bytes.NewReader([]byte(`{bad`)))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatalf("malformed PUT should 400, got %d", res.StatusCode)
	}
}

func TestSafeMimeClassification(t *testing.T) {
	cases := []struct {
		file   string
		mime   string
		inline bool
	}{
		{"a.PNG", "image/png", true},
		{"a.jpeg", "image/jpeg", true},
		{"a.jpg", "image/jpeg", true},
		{"a.gif", "image/gif", true},
		{"a.webp", "image/webp", true},
		{"a.mp4", "video/mp4", true},
		{"a.webm", "video/webm", true},
		{"a.pdf", "application/pdf", true},
		{"evil.html", "application/octet-stream", false},
		{"evil.svg", "application/octet-stream", false},
		{"noext", "application/octet-stream", false},
	}
	for _, c := range cases {
		mime, inline := safeMime(c.file)
		if mime != c.mime || inline != c.inline {
			t.Errorf("safeMime(%q) = (%q,%v), want (%q,%v)", c.file, mime, inline, c.mime, c.inline)
		}
	}
}

// 频道附件走同一套存储与回放,owner 归属必须是 channel。
func TestChannelAttachmentUploadAndList(t *testing.T) {
	srv, _ := newWorkspaceTestServer(t)

	var ch store.Channel
	doJSON(t, "POST", srv.URL+"/api/channels", map[string]any{"name": "dev"}, &ch)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "shot.png")
	_, _ = fw.Write([]byte("\x89PNG\r\n\x1a\n"))
	_ = mw.Close()

	res, err := http.Post(srv.URL+"/api/channels/"+ch.ID+"/attachments", mw.FormDataContentType(), &buf)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	var att store.Attachment
	_ = json.NewDecoder(res.Body).Decode(&att)
	res.Body.Close()
	if res.StatusCode != 201 || att.OwnerType != "channel" || att.OwnerID != ch.ID || att.Mime != "image/png" {
		t.Fatalf("channel attachment wrong: %d %+v", res.StatusCode, att)
	}

	var list []store.Attachment
	doJSON(t, "GET", srv.URL+"/api/channels/"+ch.ID+"/attachments", nil, &list)
	if len(list) != 1 || list[0].ID != att.ID {
		t.Fatalf("channel attachment list wrong: %+v", list)
	}

	// 消息可引用该附件。
	if code := doJSON(t, "POST", srv.URL+"/api/channels/"+ch.ID+"/messages", map[string]any{
		"text":        "看图",
		"attachments": []map[string]string{{"id": att.ID, "filename": att.Filename, "mime": att.Mime}},
	}, nil); code != 200 {
		t.Fatalf("post with attachment: %d", code)
	}
	var msgs []store.ChannelMessage
	doJSON(t, "GET", srv.URL+"/api/channels/"+ch.ID+"/messages", nil, &msgs)
	found := false
	for _, m := range msgs {
		if strings.Contains(m.PayloadJSON, att.ID) {
			found = true
		}
	}
	if !found {
		t.Fatalf("message did not carry the attachment ref: %+v", msgs)
	}

	// 只有附件没有文字也应被接受。
	if code := doJSON(t, "POST", srv.URL+"/api/channels/"+ch.ID+"/messages", map[string]any{
		"attachments": []map[string]string{{"id": att.ID, "filename": att.Filename, "mime": att.Mime}},
	}, nil); code != 200 {
		t.Fatalf("attachment-only message should be accepted, got %d", code)
	}
}

// PRD D.5:run 事件与审批卡片必须回灌到关联频道,人可以直接在聊天里审批。
func TestRunEventsAndApprovalCardRelayedToChannel(t *testing.T) {
	srv, st, rm := newWorkspaceTestServerWithManager(t)
	ctx := context.Background()

	var ch store.Channel
	doJSON(t, "POST", srv.URL+"/api/channels", map[string]any{"name": "relay"}, &ch)
	var it store.WorkItem
	doJSON(t, "POST", srv.URL+"/api/work-items",
		map[string]any{"type": "task", "title": "带审批的任务", "channelId": ch.ID}, &it)

	var ex struct {
		RunID string `json:"runId"`
	}
	if code := doJSON(t, "POST", srv.URL+"/api/work-items/"+it.ID+"/execute", nil, &ex); code != 200 {
		t.Fatalf("execute: %d", code)
	}

	// 频道里应出现审批卡片(kind=approval,带 runId/nodeId 供前端回调)。
	var card map[string]any
	waitFor(t, func() bool {
		msgs, err := st.ListChannelMessages(ctx, ch.ID, 100)
		if err != nil {
			return false
		}
		for _, m := range msgs {
			if m.AuthorKind != "workflow" {
				continue
			}
			var p map[string]any
			if json.Unmarshal([]byte(m.PayloadJSON), &p) != nil {
				continue
			}
			if p["kind"] == "approval" {
				card = p
				return true
			}
		}
		return false
	}, 30*time.Second)

	if card["runId"] != ex.RunID || card["nodeId"] != "review" {
		t.Fatalf("approval card missing run/node reference: %+v", card)
	}
	arts, _ := card["artifacts"].([]any)
	if len(arts) == 0 || arts[0] != "output.json" {
		t.Fatalf("approval card must list produced artifacts, got %+v", card["artifacts"])
	}

	// 也应有节点完成这类工作流事件。
	msgs, _ := st.ListChannelMessages(ctx, ch.ID, 100)
	sawRunEvent := false
	for _, m := range msgs {
		var p map[string]any
		_ = json.Unmarshal([]byte(m.PayloadJSON), &p)
		if p["kind"] == "run" {
			sawRunEvent = true
		}
	}
	if !sawRunEvent {
		t.Fatal("no workflow run event relayed to the channel")
	}

	// 在聊天里通过审批 → run 走完 → 工作项 done。
	if err := rm.Approve(ex.RunID, "review", true, "", nil); err != nil {
		t.Fatalf("approve: %v", err)
	}
	waitFor(t, func() bool {
		g, e := st.GetWorkItem(ctx, it.ID)
		return e == nil && g.Status == "done"
	}, 20*time.Second)
}

// 没有关联频道的工作项不应该产生任何频道消息。
func TestRunEventsNotRelayedWithoutChannel(t *testing.T) {
	srv, st, _ := newWorkspaceTestServerWithManager(t)
	ctx := context.Background()

	var ch store.Channel
	doJSON(t, "POST", srv.URL+"/api/channels", map[string]any{"name": "quiet"}, &ch)
	var it store.WorkItem
	doJSON(t, "POST", srv.URL+"/api/work-items", map[string]any{"type": "task", "title": "无频道"}, &it)
	doJSON(t, "POST", srv.URL+"/api/work-items/"+it.ID+"/execute", nil, nil)

	time.Sleep(4 * time.Second)
	msgs, _ := st.ListChannelMessages(ctx, ch.ID, 50)
	for _, m := range msgs {
		if m.AuthorKind == "workflow" {
			t.Fatalf("unrelated channel received a run event: %+v", m)
		}
	}
}
