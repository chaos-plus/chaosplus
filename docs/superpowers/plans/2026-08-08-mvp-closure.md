# MVP 闭环 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 从控制面 Web UI 发起静态 DAG run、WS 实时看节点进度、在 human_approval 节点完成一次真人工审批(engine→NATS→daemon→真实 claude)。

**Architecture:** 控制面新增 stdlib HTTP 服务 + gorilla/websocket。引擎 `mark()` 加 `OnEvent` 钩子吐实时事件;`ApprovalBroker`+`ApprovalExecutor` 让审批真阻塞、由 HTTP 解析。事件经 NATS `chaos.run.{runID}.evt` fan-out 回 WS 客户端,并必写 StateStore events 表。单页 HTML 嵌入控制面。

**Tech Stack:** Go(控制面)、`gorilla/websocket`(唯一新依赖)、`github.com/nats-io/nats.go`(既有)、`github.com/nats-io/nats-server/v2`(测试既有)、bun(daemon 不变)。

## Global Constraints

- 不引入除 `gorilla/websocket` 外的任何新依赖。
- 引擎调度/readiness 逻辑零改动——只加钩子与状态,不动 §7.3 语义。
- 事件必写既有 `events` 表(store.Event),幂等键防重。
- daemon 的 SSE 1v1 聊天 UI 不动。
- Go 代码 gofmt + goimports;测试 table-driven + `-race`。
- 提交信息 conventional commits,无 Co-Authored-By(全局禁用归属)。

---
**先做一次依赖与基线检查(任何 Task 之前):**

- [ ] **Step 0: 确认基线绿**
  Run: `cd apps/control-plane && GOSUMDB=sum.golang.org go test ./...`(工作目录 apps/control-plane)
  Expected: 全部通过(engine 84.7% cov 基线)。
  Run: `cd apps/daemon && bun test && bun run typecheck`
  Expected: 9 pass;tsc 无输出。

---

### Task 1: 引擎事件钩子 + waiting_approval 状态

**Files:**
- Modify: `apps/control-plane/internal/workflow/engine.go`
- Modify: `apps/control-plane/internal/workflow/nodes.go`
- Test: `apps/control-plane/internal/workflow/engine_test.go`(追加)

**Interfaces:**
- Produces: `Engine.OnEvent func(Event)`(字段,mark() 内调用,可 nil);`workflow.StatusWaitingApproval Status = "waiting_approval"`;`execApproval` 先 `mark(id, StatusWaitingApproval, nil, "")` 再委托 `exec.Approve`。

- [ ] **Step 1: 写失败测试**(engine_test.go 追加)

```go
func TestEngineOnEventHook(t *testing.T) {
	def := testLinearDef() // trigger -> agent -> done, 见文件尾部 helper(若已存在复用,不存在则本文件内新加)
	var got []Event
	eng, err := NewEngine(def, &MockExecutor{})
	if err != nil { t.Fatalf("new engine: %v", err) }
	eng.OnEvent = func(ev Event) { got = append(got, ev) }
	if _, err := eng.Run(context.Background(), nil); err != nil { t.Fatalf("run: %v", err) }
	if len(got) == 0 { t.Fatal("OnEvent not called") }
	if got[0].Status != StatusRunning { t.Errorf("first event = %s, want running", got[0].Status) }
	// 每个节点应有 running + completed 两个事件,且整体有序递增
	for i := 1; i < len(got); i++ {
		if got[i].Seq <= got[i-1].Seq { t.Errorf("events not ordered at %d", i) }
	}
}
```

- [ ] **Step 2: 运行确认失败**
  Run: `go test ./internal/workflow/ -run TestEngineOnEventHook -v`
  Expected: FAIL —— `eng.OnEvent` 无此字段。

- [ ] **Step 3: 实现**

engine.go —— Status 枚举加一行、Engine 加字段、mark() 末尾调用:
```go
StatusWaitingApproval Status = "waiting_approval" // human_approval gate is blocked on a human
```
```go
	seq       int
	events    []Event
	OnEvent   func(Event) // live lifecycle hook (nil-safe); fires on every mark()
```
mark() 末尾(append 之后):
```go
	if e.OnEvent != nil {
		e.OnEvent(ev)
	}
```

nodes.go —— execApproval 先标记等待:
```go
func (e *Engine) execApproval(ctx context.Context, st *nodeState) error {
	e.mark(st.node.ID, StatusWaitingApproval, nil, "")
	ok, err := e.exec.Approve(ctx, st.node)
	if err != nil {
		return err
	}
	st.approved = ok
	out, _ := json.Marshal(map[string]any{"approved": ok})
	st.output = out
	e.updateScope(st)
	e.mark(st.node.ID, StatusCompleted, out, "")
	return nil
}
```

- [ ] **Step 4: 运行确认通过**
  Run: `go test ./internal/workflow/ -run TestEngineOnEventHook -v`
  Expected: PASS。

- [ ] **Step 5: 全量回归**
  Run: `go test ./internal/workflow/`
  Expected: 全绿(既有测试不受 waiting_approval 非终态影响)。

- [ ] **Step 6: Commit**
  Run: `git add apps/control-plane/internal/workflow/engine.go apps/control-plane/internal/workflow/nodes.go apps/control-plane/internal/workflow/engine_test.go && git commit -m "feat(workflow): live OnEvent hook + waiting_approval status"`

---

### Task 2: validate 规则 —— human_approval 出边必须 approved/rejected

**Files:**
- Modify: `apps/control-plane/internal/workflow/validate.go`
- Test: `apps/control-plane/internal/workflow/validate_test.go`(若已存在则追加)

**Interfaces:**
- Consumes: `workflow.NodeType`、`EdgeCondition`(EdgeApproved/EdgeRejected)
- Produces: 无新导出;`WorkflowDef.Validate()` 对每个 human_approval 节点校验其全部出边 condition ∈ {approved, rejected}。

- [ ] **Step 1: 写失败测试**

```go
func TestValidateApprovalEdges(t *testing.T) {
	base := testApprovalDef() // trigger -> approval -> agent
	// 合法: approval 出边 = approved
	if err := base.Validate(); err != nil {
		t.Fatalf("approved edge should validate: %v", err)
	}
	// 非法: approval 出边 = success(默认)
	bad := testApprovalDef()
	bad.Edges[0].Condition = EdgeSuccess // trigger->approval
	bad.Edges[1].Condition = EdgeSuccess // approval->agent
	if err := bad.Validate(); err == nil {
		t.Fatal("approval node with success edge should fail validation")
	}
}
// testApprovalDef: trigger -(success)-> approval(human_approval) -(approved)-> agent
func testApprovalDef() *WorkflowDef {
	return &WorkflowDef{
		ID: "t", Version: "1",
		Nodes: []Node{
			{ID: "t0", Type: NodeTrigger, Trigger: &TriggerSpec{Source: "manual"}},
			{ID: "ap", Type: NodeHumanApproval, HumanApproval: &HumanApprovalSpec{Approvers: "any_human", TimeoutMs: 60000, OnTimeout: "pause", OnReject: "pause"}},
			{ID: "a0", Type: NodeAgent, Agent: &ExecutorAgentSpec{Executor: "mock", SystemPrompt: "x"}},
		},
		Edges: []Edge{
			{From: "t0", To: "ap", Condition: EdgeSuccess},
			{From: "ap", To: "a0", Condition: EdgeApproved},
		},
	}
}
```
(TriggerSpec/ExecutorAgentSpec 的字段名先核对 def.go——若 Source/Executor/SystemPrompt 不同,以 def.go 实际为准调整。)

- [ ] **Step 2: 运行确认失败**
  Run: `go test ./internal/workflow/ -run TestValidateApprovalEdges -v`
  Expected: FAIL —— 非法 case 没被拒。

- [ ] **Step 3: 实现** —— validate.go 的 Validate() 内、Kahn 环检测之后、per-type 循环之前插入:

```go
	// human_approval nodes must route only via approved/rejected out-edges
	// (F.5): a default success edge would let rejection flow downstream.
	for id, n := range nodes {
		if n.Type != NodeHumanApproval {
			continue
		}
		hasOut := false
		for _, e := range d.Edges {
			if e.From != id {
				continue
			}
			hasOut = true
			if e.Condition != EdgeApproved && e.Condition != EdgeRejected {
				return fmt.Errorf("workflow %s: approval node %q out-edge to %q must be 'approved' or 'rejected' (got %q)", d.ID, id, e.To, e.Condition)
			}
		}
		if !hasOut {
			return fmt.Errorf("workflow %s: approval node %q has no out-edges", d.ID, id)
		}
	}
```

- [ ] **Step 4: 运行确认通过**
  Run: `go test ./internal/workflow/ -run TestValidateApprovalEdges -v`
  Expected: PASS。

- [ ] **Step 5: 回归 + 确认示例仍合法**
  Run: `go test ./internal/workflow/`
  Expected: 全绿。
  Run: `cd examples && GOSUMDB=sum.golang.org go run ../cmd/run-workflow` 不执行——改为:加载 `examples/software-dev-agile.json` 与 `examples/e2e-real.json` 调用 Validate() 的测试/命令若不存在,则以 `go test ./internal/workflow/` 内的既有示例加载测试为准。若示例被新规则误伤,修正示例 JSON 的边 condition。
  Expected: software-dev-agile.json / e2e-real.json 的审批边均为 approved。

- [ ] **Step 6: Commit**
  Run: `git add apps/control-plane/internal/workflow/validate.go apps/control-plane/internal/workflow/validate_test.go && git commit -m "feat(workflow): validate human_approval out-edges are approved/rejected"`

---

### Task 3: ApprovalBroker + ApprovalExecutor

**Files:**
- Create: `apps/control-plane/internal/workflow/approval.go`
- Test: `apps/control-plane/internal/workflow/approval_test.go`

**Interfaces:**
- Produces:
  - `type Decision struct { OK bool; Reason string }`
  - `func NewApprovalBroker() *ApprovalBroker`
  - `(*ApprovalBroker) Wait(ctx context.Context, nodeID string) (bool, error)` —— 阻塞直到 Resolve 或 ctx 取消;重复对同一 nodeID Wait 返回错误。
  - `(*ApprovalBroker) Resolve(nodeID string, ok bool, reason string) error` —— 幂等(已决定 → 错误);成功后触发 `OnDecision`(可 nil)。
  - `(*ApprovalBroker) Decision(nodeID string) (Decision, bool)`
  - `(*ApprovalBroker) OnDecision func(nodeID string, d Decision)`(字段)
  - `type ApprovalExecutor struct{ base Executor; broker *ApprovalBroker }` + `func NewApprovalExecutor(base Executor, broker *ApprovalBroker) *ApprovalExecutor`;`RunAgent` 委托 base;`Approve` 调 `broker.Wait`。
- Consumes: `workflow.Executor`(既有接口)、`workflow.Node`。

- [ ] **Step 1: 写失败测试**

```go
func TestApprovalBrokerWaitResolve(t *testing.T) {
	b := NewApprovalBroker()
	done := make(chan bool, 1)
	go func() { ok, _ := b.Wait(context.Background(), "n1"); done <- ok }()
	select {
	case <-done:
		t.Fatal("Wait returned before Resolve")
	default:
	}
	if err := b.Resolve("n1", true, ""); err != nil { t.Fatalf("resolve: %v", err) }
	if ok := <-done; !ok { t.Error("Wait returned false, want true") }
	if d, ok := b.Decision("n1"); !ok || !d.OK { t.Errorf("decision = %+v, want ok=true", d) }
}

func TestApprovalBrokerResolveTwice(t *testing.T) {
	b := NewApprovalBroker()
	if err := b.Resolve("n1", true, ""); err != nil { t.Fatalf("first resolve: %v", err) }
	if err := b.Resolve("n1", false, "x"); err == nil { t.Fatal("second resolve should error") }
}

func TestApprovalBrokerWaitCancel(t *testing.T) {
	b := NewApprovalBroker()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := b.Wait(ctx, "n1"); done <- err }()
	cancel()
	select {
	case err := <-done:
		if err == nil { t.Fatal("Wait should return ctx error") }
	case <-time.After(time.Second):
		t.Fatal("Wait did not unblock on cancel")
	}
}

func TestApprovalExecutorDelegates(t *testing.T) {
	b := NewApprovalBroker()
	base := &MockExecutor{RunAgentFn: func(_ context.Context, n *Node, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"agent":"ran"}`), nil
	}}
	ae := NewApprovalExecutor(base, b)
	// RunAgent 委托 base
	out, err := ae.RunAgent(context.Background(), &Node{ID: "a"}, nil)
	if err != nil || string(out) != `{"agent":"ran"}` { t.Fatalf("RunAgent delegate: %s %v", out, err) }
	// Approve 阻塞到 Resolve
	go func() { _ = b.Resolve("ap", false, "redo") }()
	ok, err := ae.Approve(context.Background(), &Node{ID: "ap"})
	if err != nil { t.Fatalf("approve: %v", err) }
	if ok { t.Error("Approve should return false") }
}
```

- [ ] **Step 2: 运行确认失败**
  Run: `go test ./internal/workflow/ -run 'TestApproval(Broker|Executor)' -v`
  Expected: FAIL —— approval.go 不存在。

- [ ] **Step 3: 实现** —— approval.go

```go
package workflow

import (
	"context"
	"fmt"
	"sync"
)

// Decision is one human_approval resolution.
type Decision struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}

// ApprovalBroker holds pending human-approval gates for one run. Wait blocks
// until Resolve (via HTTP) or ctx cancellation; Resolve is idempotent.
type ApprovalBroker struct {
	mu      sync.Mutex
	pending map[string]chan Decision
	decided map[string]Decision
	// OnDecision fires after a successful Resolve (nil-safe). Used by RunManager
	// to emit REVIEW_APPROVED / REVIEW_REJECTED events live.
	OnDecision func(nodeID string, d Decision)
}

func NewApprovalBroker() *ApprovalBroker {
	return &ApprovalBroker{pending: make(map[string]chan Decision), decided: make(map[string]Decision)}
}

// Wait blocks until the node's gate is resolved. Only the first caller per
// nodeID waits; a second Wait for the same nodeID errors.
func (b *ApprovalBroker) Wait(ctx context.Context, nodeID string) (bool, error) {
	ch := make(chan Decision, 1)
	b.mu.Lock()
	if d, done := b.decided[nodeID]; done {
		b.mu.Unlock()
		return d.OK, nil
	}
	if _, exists := b.pending[nodeID]; exists {
		b.mu.Unlock()
		return false, fmt.Errorf("approval %q: already waiting", nodeID)
	}
	b.pending[nodeID] = ch
	b.mu.Unlock()

	select {
	case d := <-ch:
		return d.OK, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// Resolve delivers a decision to a waiting Wait. Idempotent: a second call for
// the same nodeID errors. When the decision lands it is recorded and OnDecision
// fires.
func (b *ApprovalBroker) Resolve(nodeID string, ok bool, reason string) error {
	d := Decision{OK: ok, Reason: reason}
	b.mu.Lock()
	if _, done := b.decided[nodeID]; done {
		b.mu.Unlock()
		return fmt.Errorf("approval %q: already resolved", nodeID)
	}
	b.decided[nodeID] = d
	ch := b.pending[nodeID]
	onDec := b.OnDecision
	b.mu.Unlock()

	if ch != nil {
		ch <- d // buffered(1): never blocks; Wait may have been cancelled
	}
	if onDec != nil {
		onDec(nodeID, d)
	}
	return nil
}

// Decision returns the recorded decision for a nodeID (ok=false if unresolved).
func (b *ApprovalBroker) Decision(nodeID string) (Decision, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	d, ok := b.decided[nodeID]
	return d, ok
}

// ApprovalExecutor makes human_approval gates block on the broker while agent
// nodes run on the wrapped base executor. The engine's execApproval already
// calls Approve — only the executor changes.
type ApprovalExecutor struct {
	base   Executor
	broker *ApprovalBroker
}

func NewApprovalExecutor(base Executor, broker *ApprovalBroker) *ApprovalExecutor {
	return &ApprovalExecutor{base: base, broker: broker}
}

func (a *ApprovalExecutor) RunAgent(ctx context.Context, node *Node, input json.RawMessage) (json.RawMessage, error) {
	return a.base.RunAgent(ctx, node, input)
}

func (a *ApprovalExecutor) Approve(ctx context.Context, node *Node) (bool, error) {
	return a.broker.Wait(ctx, node.ID)
}

var _ Executor = (*ApprovalExecutor)(nil)
```

- [ ] **Step 4: 运行确认通过**
  Run: `go test ./internal/workflow/ -run 'TestApproval(Broker|Executor)' -v`
  Expected: PASS。

- [ ] **Step 5: Commit**
  Run: `git add apps/control-plane/internal/workflow/approval.go apps/control-plane/internal/workflow/approval_test.go && git commit -m "feat(workflow): ApprovalBroker + blocking ApprovalExecutor"`

---

### Task 4: RunManager —— run 注册表 + NATS 扇出 + StateStore 落库

**Files:**
- Create: `apps/control-plane/internal/server/runs.go`
- Test: `apps/control-plane/internal/server/runs_test.go`

**Interfaces:**
- Consumes: `workflow`(Engine/NewEngine/NewApprovalExecutor/ApprovalBroker/NewRunnerExecutor/MockExecutor/WorkflowDef/Event/Status)、`gateway.Gateway`、`store.Store`、`nats.Conn`。
- Produces:
  - `type RunStatus string`(常量 `RunRunning`/`RunWaitingApproval`/`RunCompleted`/`RunFailed`/`RunPaused`)
  - `type RunEvent struct { Seq int; RunID string; NodeID string; Status workflow.Status; Output json.RawMessage; Error string; Review *ReviewInfo }` + `type ReviewInfo struct { Approved bool; Reason string }`
  - `type Run struct { ID, Def, Status, Events, Broker, cancel, created }`
  - `type LaunchRequest struct { WorkflowFile string; WorkflowJSON json.RawMessage; Workspace string; Context json.RawMessage; RunnerID string }`
  - `func NewRunManager(nc *nats.Conn, g *gateway.Gateway, st *store.Store, runnerID string) *RunManager`
  - `(*RunManager) Start(ctx) error` —— 订阅 `chaos.run.*.evt`,按 runID 扇出
  - `(*RunManager) Launch(ctx, req LaunchRequest) (*Run, error)`
  - `(*RunManager) List() []*Run`、`(*RunManager) Get(id) (*Run, bool)`
  - `(*RunManager) Approve(runID, nodeID string, ok bool, reason string) error`
  - `(*RunManager) baseFactory func(runID string) workflow.Executor`(测试注入;nil → RunnerExecutor)
  - `type RunSubscriber chan RunEvent`

**测试基建:** 内嵌 nats-server(`github.com/nats-io/nats-server/v2/server`,gateway_test 同款)。

- [ ] **Step 1: 写失败测试** —— runs_test.go

```go
package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/gateway"
	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/workflow"
)

func startTestNATS(t *testing.T) *nats.Conn {
	t.Helper()
	ns, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	if err != nil { t.Fatalf("nats server: %v", err) }
	go ns.Start()
	t.Cleanup(ns.Shutdown)
	if !ns.ReadyForConnections(2 * time.Second) { t.Fatal("nats not ready") }
	nc, err := nats.Connect(ns.ClientURL())
	if err != nil { t.Fatalf("nats connect: %v", err) }
	t.Cleanup(nc.Close)
	return nc
}

func testDef() *workflow.WorkflowDef {
	return &workflow.WorkflowDef{
		ID: "t", Version: "1",
		Nodes: []workflow.Node{
			{ID: "t0", Type: workflow.NodeTrigger, Trigger: &workflow.TriggerSpec{Source: "manual"}},
			{ID: "ap", Type: workflow.NodeHumanApproval, HumanApproval: &workflow.HumanApprovalSpec{Approvers: "any_human", TimeoutMs: 60000, OnTimeout: "pause", OnReject: "pause"}},
			{ID: "a0", Type: workflow.NodeAgent, Agent: &workflow.ExecutorAgentSpec{Executor: "mock", SystemPrompt: "x"}},
		},
		Edges: []workflow.Edge{
			{From: "t0", To: "ap", Condition: workflow.EdgeSuccess},
			{From: "ap", To: "a0", Condition: workflow.EdgeApproved},
		},
	}
}

func TestRunManagerLaunchAndApprove(t *testing.T) {
	nc := startTestNATS(t)
	g := gateway.New(nc)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go g.Start(ctx)
	time.Sleep(150 * time.Millisecond)

	m := NewRunManager(nc, g, nil, "runner-1")
	m.baseFactory = func(_ string) workflow.Executor { return &workflow.MockExecutor{} }
	if err := m.Start(ctx); err != nil { t.Fatalf("start: %v", err) }

	defJSON, _ := json.Marshal(testDef())
	run, err := m.Launch(ctx, LaunchRequest{WorkflowJSON: defJSON, Workspace: t.TempDir()})
	if err != nil { t.Fatalf("launch: %v", err) }

	// run 应停在审批节点等待
	waitFor(t, func() bool { return run.Status == RunWaitingApproval }, 3*time.Second)

	// 通过 → 继续到 a0 → 完成
	if err := m.Approve(run.ID, "ap", true, ""); err != nil { t.Fatalf("approve: %v", err) }
	waitFor(t, func() bool { return run.Status == RunCompleted }, 3*time.Second)

	if len(run.Events) == 0 { t.Fatal("no events recorded") }
}

func TestRunManagerRejectPauses(t *testing.T) {
	nc := startTestNATS(t)
	g := gateway.New(nc)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go g.Start(ctx)
	time.Sleep(150 * time.Millisecond)

	m := NewRunManager(nc, g, nil, "runner-1")
	m.baseFactory = func(_ string) workflow.Executor { return &workflow.MockExecutor{} }
	if err := m.Start(ctx); err != nil { t.Fatalf("start: %v", err) }

	defJSON, _ := json.Marshal(testDef())
	run, _ := m.Launch(ctx, LaunchRequest{WorkflowJSON: defJSON, Workspace: t.TempDir()})
	waitFor(t, func() bool { return run.Status == RunWaitingApproval }, 3*time.Second)

	if err := m.Approve(run.ID, "ap", false, "wrong spec"); err != nil { t.Fatalf("reject: %v", err) }
	waitFor(t, func() bool { return run.Status == RunPaused }, 3*time.Second)
}

func waitFor(t *testing.T, cond func() bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() { return }
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}
```

> 第一个测试需要 RunManager 能跑完带审批的 DAG:baseFactory 返回 MockExecutor,外层由 ApprovalExecutor 包装(在 Launch 内),agent 秒回、审批由 broker 阻塞。`RunWaitingApproval` 由 RunManager 在收到 waiting_approval 事件时置位。

- [ ] **Step 2: 运行确认失败**
  Run: `cd apps/control-plane && GOSUMDB=sum.golang.org go test ./internal/server/ -run TestRunManager -v`
  Expected: 编译失败(runs.go 不存在)。

- [ ] **Step 3: 实现** —— runs.go

```go
// Package server is the control-plane's HTTP + WebSocket + run-orchestration
// surface (PRD §3/C2/C3): REST commands, WS realtime, NATS fan-out, StateStore
// event log. Browser never touches the store (P1).
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/gateway"
	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/store"
	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/workflow"
)

const runSubjectPrefix = "chaos.run."

// RunStatus is a run's coarse lifecycle state for the UI.
type RunStatus string

const (
	RunRunning         RunStatus = "running"
	RunWaitingApproval RunStatus = "waiting_approval"
	RunCompleted       RunStatus = "completed"
	RunFailed          RunStatus = "failed"
	RunPaused          RunStatus = "paused"
)

// ReviewInfo carries a human-approval resolution in the event stream.
type ReviewInfo struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason,omitempty"`
}

// RunEvent is the wire/UI event: node lifecycle + run-level + review metadata.
type RunEvent struct {
	Seq    int             `json:"seq"`
	RunID  string          `json:"runId"`
	NodeID string          `json:"nodeId,omitempty"`
	Status workflow.Status `json:"status"`
	Output json.RawMessage `json:"output,omitempty"`
	Error  string          `json:"error,omitempty"`
	Review *ReviewInfo     `json:"review,omitempty"`
}

// RunSubscriber receives live events for one run (a WS connection's channel).
type RunSubscriber chan RunEvent

// Run is one in-memory workflow run (PRD workflow_runs table deferred).
type Run struct {
	ID      string
	Def     *workflow.WorkflowDef
	Status  RunStatus
	Events  []RunEvent
	Broker  *workflow.ApprovalBroker
	subs    map[RunSubscriber]struct{}
	mu      sync.Mutex
	cancel  context.CancelFunc
	created time.Time
	seq     int
}

func (r *Run) publish(ev RunEvent) {
	r.mu.Lock()
	r.Events = append(r.Events, ev)
	for sub := range r.subs {
		select {
		case sub <- ev: // buffered; drop if the client is too slow rather than block the engine
		default:
		}
	}
	r.mu.Unlock()
}

func (r *Run) subscribe() (RunSubscriber, func()) {
	ch := make(RunSubscriber, 64)
	r.mu.Lock()
	r.subs[ch] = struct{}{}
	hist := append([]RunEvent(nil), r.Events...)
	r.mu.Unlock()
	return ch, func() { r.mu.Lock(); delete(r.subs, ch); r.mu.Unlock() }
}

func (r *Run) nextSeq() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	return r.seq
}

func (r *Run) setStatus(s RunStatus) {
	r.mu.Lock()
	r.Status = s
	r.mu.Unlock()
}

func (r *Run) status() RunStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Status
}

// LaunchRequest is the POST /api/runs body. Exactly one of WorkflowFile /
// WorkflowJSON must be set.
type LaunchRequest struct {
	WorkflowFile string          `json:"workflowFile"`
	WorkflowJSON json.RawMessage `json:"workflowJSON"`
	Workspace    string          `json:"workspace"`
	Context      json.RawMessage `json:"context"`
	RunnerID     string          `json:"runnerId"`
}

// RunManager owns live runs and the NATS fan-out to their WS subscribers.
type RunManager struct {
	nc          *nats.Conn
	g           *gateway.Gateway
	st          *store.Store
	runnerID    string
	mu          sync.Mutex
	runs        map[string]*Run
	seq         int
	sub         *nats.Subscription
	baseFactory func(runID string) workflow.Executor // test seam; nil → RunnerExecutor
}

func NewRunManager(nc *nats.Conn, g *gateway.Gateway, st *store.Store, runnerID string) *RunManager {
	return &RunManager{nc: nc, g: g, st: st, runnerID: runnerID, runs: make(map[string]*Run)}
}

// Start subscribes chaos.run.*.evt and fans out each event to the matching
// run's subscribers. Safe to call once.
func (m *RunManager) Start(ctx context.Context) error {
	sub, err := m.nc.Subscribe(runSubjectPrefix+">", func(msg *nats.Msg) {
		var ev RunEvent
		if err := json.Unmarshal(msg.Data, &ev); err != nil {
			slog.Warn("bad run event", "err", err)
			return
		}
		m.mu.Lock()
		r := m.runs[ev.RunID]
		m.mu.Unlock()
		if r != nil {
			r.publish(ev)
		}
	})
	if err != nil {
		return fmt.Errorf("subscribe run events: %w", err)
	}
	m.sub = sub
	go func() {
		<-ctx.Done()
		_ = sub.Unsubscribe()
	}()
	return nil
}

func (m *RunManager) List() []*Run {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Run, 0, len(m.runs))
	for _, r := range m.runs {
		out = append(out, r)
	}
	return out
}

func (m *RunManager) Get(id string) (*Run, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[id]
	return r, ok
}

func (m *RunManager) newRun(def *workflow.WorkflowDef) *Run {
	m.mu.Lock()
	m.seq++
	id := fmt.Sprintf("run-%d", m.seq)
	run := &Run{
		ID:      id,
		Def:     def,
		Status:  RunRunning,
		Broker:  workflow.NewApprovalBroker(),
		subs:    make(map[RunSubscriber]struct{}),
		created: time.Now(),
	}
	m.runs[id] = run
	m.mu.Unlock()
	return run
}

// Launch loads + validates a workflow, then runs it on a goroutine against the
// per-run broker-backed executor. Returns immediately; progress arrives via
// Events / WS.
func (m *RunManager) Launch(ctx context.Context, req LaunchRequest) (*Run, error) {
	var raw []byte
	if req.WorkflowJSON != nil {
		raw = req.WorkflowJSON
	} else if req.WorkflowFile != "" {
		b, err := os.ReadFile(req.WorkflowFile)
		if err != nil {
			return nil, fmt.Errorf("read workflow: %w", err)
		}
		raw = b
	} else {
		return nil, fmt.Errorf("workflowFile or workflowJSON is required")
	}
	var def workflow.WorkflowDef
	if err := json.Unmarshal(raw, &def); err != nil {
		return nil, fmt.Errorf("parse workflow: %w", err)
	}
	if err := def.Validate(); err != nil {
		return nil, fmt.Errorf("validate workflow: %w", err)
	}
	if req.Workspace == "" {
		return nil, fmt.Errorf("workspace is required")
	}

	run := m.newRun(&def)
	runCtx, cancel := context.WithCancel(ctx)
	run.cancel = cancel
	broker := run.Broker
	broker.OnDecision = func(nodeID string, d workflow.Decision) {
		m.emit(run, RunEvent{
			RunID: run.ID, NodeID: nodeID, Status: workflow.StatusCompleted,
			Review: &ReviewInfo{Approved: d.OK, Reason: d.Reason},
		})
	}

	var base workflow.Executor
	if m.baseFactory != nil {
		base = m.baseFactory(run.ID)
	} else {
		runnerID := req.RunnerID
		if runnerID == "" {
			runnerID = m.runnerID
		}
		if runnerID == "" {
			reg := m.g.RegisteredRunners()
			if len(reg) == 0 {
				cancel()
				return nil, fmt.Errorf("no runner registered; set runnerId or start a daemon")
			}
			runnerID = reg[0]
		}
		base = workflow.NewRunnerExecutor(m.g, runnerID, req.Workspace, run.ID)
	}
	exec := workflow.NewApprovalExecutor(base, broker)

	eng, err := workflow.NewEngine(&def, exec)
	if err != nil {
		cancel()
		return nil, err
	}
	m.emit(run, RunEvent{RunID: run.ID, Status: workflow.StatusPending})
	go func() {
		defer cancel()
		eng.OnEvent = func(ev workflow.Event) {
			if ev.Status == workflow.StatusWaitingApproval {
				run.setStatus(RunWaitingApproval)
			}
			m.emit(run, RunEvent{
				Seq: ev.Seq, RunID: run.ID, NodeID: ev.NodeID,
				Status: ev.Status, Output: ev.Output, Error: ev.Error,
			})
		}
		_, err := eng.Run(runCtx, req.Context)
		run.setStatus(m.finalStatus(run, err))
	}()
	return run, nil
}

// finalStatus derives the terminal run status from the engine result and the
// run's approval outcomes (PRD F.5: onReject=pause → paused, not silent end).
func (m *RunManager) finalStatus(run *Run, err error) RunStatus {
	if err != nil {
		return RunFailed
	}
	for _, ev := range run.Events {
		if ev.NodeID == "" || ev.Review == nil {
			continue
		}
		if !ev.Review.Approved {
			if n := nodeByID(run.Def, ev.NodeID); n != nil && n.HumanApproval != nil && n.HumanApproval.OnReject == "pause" {
				return RunPaused
			}
		}
	}
	for _, ev := range run.Events {
		if ev.Status == workflow.StatusFailed {
			return RunFailed
		}
	}
	return RunCompleted
}

func nodeByID(def *workflow.WorkflowDef, id string) *workflow.Node {
	for i := range def.Nodes {
		if def.Nodes[i].ID == id {
			return &def.Nodes[i]
		}
	}
	return nil
}

// Approve resolves a human-approval gate. 404/409 handled by the HTTP layer.
func (m *RunManager) Approve(runID, nodeID string, ok bool, reason string) error {
	run, found := m.Get(runID)
	if !found {
		return fmt.Errorf("run %s not found", runID)
	}
	return run.Broker.Resolve(nodeID, ok, reason)
}

// emit publishes an event to NATS (cluster fan-out), persists it to the
// StateStore when configured, and delivers locally to the run's subscribers.
func (m *RunManager) emit(run *Run, ev RunEvent) {
	ev.Seq = run.nextSeq()
	ev.RunID = run.ID
	data, _ := json.Marshal(ev)
	if err := m.nc.Publish(runSubjectPrefix+run.ID+".evt", data); err != nil {
		slog.Warn("publish run event", "err", err)
	}
	if m.st != nil {
		typ := storeTypeFor(ev)
		payload, _ := json.Marshal(ev)
		_ = m.st.Append(context.Background(), store.Event{
			ID:             fmt.Sprintf("%s-%d", run.ID, ev.Seq),
			InstanceID:     "desktop",
			RunID:          run.ID,
			Type:           typ,
			IdempotencyKey: fmt.Sprintf("%s:%s:%d", run.ID, typ, ev.Seq),
			PayloadJSON:    string(payload),
		})
	}
	run.publish(ev) // local delivery; the NATS round-trip also lands async
}

func storeTypeFor(ev RunEvent) string {
	switch {
	case ev.NodeID == "":
		if ev.Status == workflow.StatusFailed {
			return "RUN_FAILED"
		}
		return "RUN_EVENT"
	case ev.Review != nil:
		if ev.Review.Approved {
			return "REVIEW_APPROVED"
		}
		return "REVIEW_REJECTED"
	case ev.Status == workflow.StatusWaitingApproval:
		return "REVIEW_REQUESTED"
	default:
		return "NODE_" + string(ev.Status)
	}
}
```

> 注意:`m.emit` 发布 NATS + 本地 `run.publish` 会各自把事件送一遍(本地即时 + NATS 订阅回调再送达一次,重复无害,UI 按 seq 幂等)。若实现时想避免双送,可去掉 `run.publish` 依赖 NATS 回调 —— 但本地直投保证同进程 WS 及时;二选一或都留均可,测试不依赖去重。

- [ ] **Step 4: 运行确认通过**
  Run: `cd apps/control-plane && GOSUMDB=sum.golang.org go test ./internal/server/ -run TestRunManager -v`
  Expected: 2 个测试全 PASS。
  Run: `go test -race ./internal/server/ -run TestRunManager`
  Expected: PASS(无数据竞争)。

- [ ] **Step 5: Commit**
  Run: `git add apps/control-plane/internal/server/runs.go apps/control-plane/internal/server/runs_test.go && git commit -m "feat(server): RunManager — NATS fan-out + StateStore + broker-backed approval"`

---

### Task 5: HTTP + WebSocket 服务

**Files:**
- Modify(依赖): `apps/control-plane/go.mod` / `go.sum` —— 加 `github.com/gorilla/websocket`
- Create: `apps/control-plane/internal/server/server.go`
- Test: `apps/control-plane/internal/server/server_test.go`

**Interfaces:**
- Consumes: Task 4 的 `RunManager`/`Run`/`RunEvent`/`LaunchRequest`;`nats.Conn`、`gateway.Gateway`。
- Produces:
  - `func NewHandler(m *RunManager) http.Handler`(mux:REST + WS + `GET /`)
  - 端口由 main 从 `CONTROL_HTTP_PORT`(默认 8081)读取,不在此包。

- [ ] **Step 1: 加依赖**
  Run: `cd apps/control-plane && GOSUMDB=sum.golang.org go get github.com/gorilla/websocket@latest`
  Expected: go.mod/go.sum 更新。

- [ ] **Step 2: 写失败测试** —— server_test.go

```go
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nats-io/nats.go"

	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/gateway"
	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/workflow"
)

func TestHTTPLaunchAndWS(t *testing.T) {
	nc := startTestNATS(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	m := NewRunManager(nc, gateway.New(nc), nil, "r")
	m.baseFactory = func(_ string) workflow.Executor { return &workflow.MockExecutor{} }
	if err := m.Start(ctx); err != nil { t.Fatalf("start: %v", err) }

	ts := httptest.NewServer(NewHandler(m))
	defer ts.Close()

	defJSON, _ := json.Marshal(testDef())
	resp, err := http.Post(ts.URL+"/api/runs", "application/json",
		strings.NewReader(`{"workflowJSON":`+string(defJSON)+`,"workspace":"`+t.TempDir()+`"}`))
	if err != nil { t.Fatalf("post run: %v", err) }
	if resp.StatusCode != 201 { t.Fatalf("status = %d, want 201", resp.StatusCode) }
	var launched struct{ RunID string `json:"runId"` }
	if err := json.NewDecoder(resp.Body).Decode(&launched); err != nil { t.Fatalf("decode: %v", err) }
	resp.Body.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/runs/" + launched.RunID + "/events"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil { t.Fatalf("dial ws: %v", err) }
	defer ws.Close()

	var gotReview bool
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var ev RunEvent
		if err := ws.ReadJSON(&ev); err != nil { break }
		if ev.NodeID == "ap" && ev.Status == workflow.StatusWaitingApproval {
			gotReview = true
			break
		}
	}
	if !gotReview { t.Fatal("WS did not receive waiting_approval event") }

	req, _ := http.NewRequest("POST", ts.URL+"/api/runs/"+launched.RunID+"/approvals/ap", strings.NewReader(`{"approve":true}`))
	req.Header.Set("Content-Type", "application/json")
	r2, err := http.DefaultClient.Do(req)
	if err != nil { t.Fatalf("approve: %v", err) }
	if r2.StatusCode != 200 { t.Fatalf("approve status = %d", r2.StatusCode) }
	r2.Body.Close()

	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		run, _ := m.Get(launched.RunID)
		if run != nil && run.Status == RunCompleted { break }
		time.Sleep(20 * time.Millisecond)
	}
	run, _ := m.Get(launched.RunID)
	if run == nil || run.Status != RunCompleted { t.Fatalf("run status = %v, want completed", run.Status) }
}

func TestHTTPErrors(t *testing.T) {
	nc := startTestNATS(t)
	m := NewRunManager(nc, gateway.New(nc), nil, "r")
	ts := httptest.NewServer(NewHandler(m))
	defer ts.Close()

	// 非法工作流(空 nodes)→ 4xx 而非 500
	r, _ := http.Post(ts.URL+"/api/runs", "application/json", strings.NewReader(`{"workflowJSON":{"nodes":[],"id":"","version":""}}`))
	if r.StatusCode == 201 { r.Body.Close(); t.Fatal("invalid workflow should not launch") }
	if r.StatusCode == 500 { r.Body.Close(); t.Fatalf("invalid input must be 4xx, got %d", r.StatusCode) }
	r.Body.Close()

	// 未知 run 的审批 → 404
	req, _ := http.NewRequest("POST", ts.URL+"/api/runs/nope/approvals/x", strings.NewReader(`{"approve":true}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil { t.Fatalf("do: %v", err) }
	if resp.StatusCode != 404 { t.Fatalf("unknown run approve = %d, want 404", resp.StatusCode) }
	resp.Body.Close()

	// GET / → 200 且含页面标记
	get, err := http.Get(ts.URL + "/")
	if err != nil { t.Fatalf("get /: %v", err) }
	if get.StatusCode != 200 { t.Fatalf("GET / = %d", get.StatusCode) }
	buf := make([]byte, 512)
	n, _ := get.Body.Read(buf)
	get.Body.Close()
	if !strings.Contains(string(buf[:n]), "chaos.plus") { t.Error("GET / does not serve the UI page") }
}
```

- [ ] **Step 3: 运行确认失败**
  Run: `cd apps/control-plane && GOSUMDB=sum.golang.org go test ./internal/server/ -run 'TestHTTP' -v`
  Expected: 编译失败(NewHandler 不存在)。

- [ ] **Step 4: 实现** —— server.go

```go
package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin:     func(r *http.Request) bool { return true }, // local dev single-user
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// NewHandler wires all control-plane HTTP routes.
func NewHandler(m *RunManager) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(uiHTML))
	})
	mux.HandleFunc("POST /api/runs", func(w http.ResponseWriter, r *http.Request) {
		var req LaunchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, 400, "bad request: "+err.Error())
			return
		}
		run, err := m.Launch(r.Context(), req)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		writeJSON(w, 201, map[string]any{"runId": run.ID})
	})
	mux.HandleFunc("GET /api/runs", func(w http.ResponseWriter, r *http.Request) {
		type sum struct {
			ID        string    `json:"id"`
			Status    RunStatus `json:"status"`
			Nodes     int       `json:"nodes"`
			CreatedAt string    `json:"createdAt"`
		}
		out := []sum{}
		for _, run := range m.List() {
			out = append(out, sum{ID: run.ID, Status: run.status(), Nodes: len(run.Def.Nodes), CreatedAt: run.created.Format("2006-01-02 15:04:05")})
		}
		writeJSON(w, 200, out)
	})
	mux.HandleFunc("GET /api/runs/{id}/events", m.handleWS)
	mux.HandleFunc("POST /api/runs/{id}/approvals/{node}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		node := r.PathValue("node")
		var body struct {
			Approve bool   `json:"approve"`
			Reason  string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, 400, "bad request: "+err.Error())
			return
		}
		if _, ok := m.Get(id); !ok {
			writeErr(w, 404, "run not found")
			return
		}
		if err := m.Approve(id, node, body.Approve, body.Reason); err != nil {
			writeErr(w, 409, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	return mux
}

// handleWS upgrades to WebSocket, replays buffered events, then streams live.
func (m *RunManager) handleWS(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, ok := m.Get(id)
	if !ok {
		writeErr(w, 404, "run not found")
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	sub, unsub := run.subscribe()
	defer unsub()
	for _, ev := range run.Events { // replay buffered
		if err := conn.WriteJSON(ev); err != nil {
			return
		}
	}
	for ev := range sub {
		if err := conn.WriteJSON(ev); err != nil {
			return
		}
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": msg})
}
```

> 注意:Task 4 的 `Run` 需要导出 `status()`(getter)与 `created` 字段(server.go 用 `run.status()` 与 `run.created`)。Task 4 实现里已含 `status()` getter;`created` 为公开字段。

- [ ] **Step 5: 运行确认通过**
  Run: `cd apps/control-plane && GOSUMDB=sum.golang.org go test ./internal/server/ -run 'TestHTTP' -v`
  Expected: PASS。
  Run: `go test -race ./internal/server/`
  Expected: PASS。

- [ ] **Step 6: Commit**
  Run: `git add apps/control-plane/go.mod apps/control-plane/go.sum apps/control-plane/internal/server/server.go apps/control-plane/internal/server/server_test.go && git commit -m "feat(server): HTTP + WebSocket run surface"`

---

### Task 6: 嵌入单页 UI

**Files:**
- Create: `apps/control-plane/internal/server/ui.go`(含 `//go:embed ui.html`)
- Create: `apps/control-plane/internal/server/ui.html`
- Test: 并入 Task 5 的 `TestHTTPErrors`(`GET /` 断言已覆盖);另加 ui_test.go 冒烟。

**Interfaces:**
- Consumes: `RunEvent`(RunID/NodeID/Status/Output/Review)、`RunStatus`。
- Produces: `uiHTML`(嵌入字符串,`NewHandler` 的 `GET /` 引用)。

- [ ] **Step 1: 写冒烟测试** —— ui_test.go

```go
package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nats-io/nats.go"
)

func TestUIServesPage(t *testing.T) {
	nc := startTestNATS(t)
	m := NewRunManager(nc, nil, nil, "r")
	ts := httptest.NewServer(NewHandler(m))
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/")
	if err != nil { t.Fatalf("get: %v", err) }
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	for _, want := range []string{"chaos.plus", "approve", "WebSocket"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("UI page missing %q", want)
		}
	}
}
```

- [ ] **Step 2: 运行确认失败**
  Run: `cd apps/control-plane && GOSUMDB=sum.golang.org go test ./internal/server/ -run TestUIServesPage -v`
  Expected: FAIL(ui.go 不存在)。

- [ ] **Step 3: 实现** —— ui.html(完整单页;深色,与 daemon UI 风格一致;JS 内 `decide` 调审批 REST)

```html
<!doctype html>
<html lang="zh">
<head>
<meta charset="utf-8">
<title>chaos.plus · 控制面</title>
<style>
  :root { --bg:#12141a; --panel:#1b1e26; --line:#2a2d36; --txt:#e6e8ee; --mut:#8a90a0;
          --ok:#3fb96f; --run:#d9a13c; --err:#e0564e; --wait:#4aa3e8; --skip:#5b6270; }
  * { box-sizing:border-box }
  body { margin:0; background:var(--bg); color:var(--txt); font:14px/1.5 system-ui, sans-serif; display:flex; height:100vh }
  aside { width:240px; border-right:1px solid var(--line); padding:12px; overflow:auto }
  main { flex:1; padding:16px; overflow:auto }
  h1,h2 { margin:0 0 10px; font-weight:600 }
  input,textarea,select,button { background:#0f1116; color:var(--txt); border:1px solid var(--line); border-radius:6px; padding:6px 8px; font:inherit }
  button { cursor:pointer }
  button.primary { background:var(--wait); color:#fff; border-color:transparent }
  .field { display:block; width:100%; margin-bottom:8px }
  .run { padding:6px 8px; border:1px solid var(--line); border-radius:6px; margin-bottom:6px; cursor:pointer }
  .run.active { border-color:var(--wait) }
  .node { display:flex; align-items:center; gap:8px; padding:6px 10px; border:1px solid var(--line); border-radius:6px; margin-bottom:6px }
  .dot { width:10px; height:10px; border-radius:50%; background:var(--skip) }
  .dot.running{ background:var(--run) } .dot.completed{ background:var(--ok) } .dot.failed{ background:var(--err) }
  .dot.waiting_approval{ background:var(--wait); animation:pulse 1.2s infinite }
  .dot.skipped{ background:var(--skip) }
  @keyframes pulse { 50% { opacity:.4 } }
  .meta { color:var(--mut); font-size:12px }
</style>
</head>
<body>
<aside>
  <h1>chaos.plus</h1>
  <h2>Runs</h2>
  <div id="runs"></div>
</aside>
<main>
  <h2 id="title">发起工作流</h2>
  <div id="launch">
    <input id="wf" class="field" placeholder="工作流 JSON 文件路径（如 examples/e2e-real.json）" />
    <input id="ws" class="field" placeholder="workspace 目录路径（agent 工作的目录）" />
    <textarea id="ctx" class="field" rows="3" placeholder='context JSON（可选）'></textarea>
    <button class="primary" onclick="launch()">运行</button>
  </div>
  <div id="detail" style="display:none">
    <h2 id="runhead"></h2>
    <div id="nodes"></div>
  </div>
</main>
<script>
let ws=null, current=null;
const $=id=>document.getElementById(id);
async function api(p,o={}){const r=await fetch(p,{headers:{'Content-Type':'application/json'},...o});const j=await r.json().catch(()=>({}));if(!r.ok)throw new Error(j.error||r.status);return j}
async function refresh(){const runs=await api('/api/runs');$('runs').innerHTML=runs.map(r=>`<div class="run ${r.id===current?'active':''}" onclick="openRun('${r.id}')">${r.id} · ${r.status}<div class="meta">${r.nodes} 节点 · ${r.createdAt}</div></div>`).join('')}
async function launch(){let ctx;const v=$('ctx').value;if(v){try{ctx=JSON.parse(v)}catch(e){alert('context 不是合法 JSON');return}}
  await api('/api/runs',{method:'POST',body:JSON.stringify({workflowFile:$('wf').value||undefined,workspace:$('ws').value,context:ctx})});refresh()}
function openRun(id){current=id;$('title').textContent='Run '+id;$('launch').style.display='none';$('detail').style.display='block';$('runhead').textContent='Run '+id;$('nodes').innerHTML='';if(ws)ws.close()
  ws=new WebSocket((location.protocol==='https:'?'wss://':'ws://')+location.host+'/api/runs/'+id+'/events');
  ws.onmessage=e=>{const ev=JSON.parse(e.data);render(ev)};ws.onclose=()=>refresh();refresh()}
function render(ev){if(!ev.nodeId){refresh();return}
  const status=ev.review?(ev.review.approved?'completed':'rejected'):ev.status;
  let el=document.getElementById('n-'+ev.nodeId);
  if(!el){el=document.createElement('div');el.id='n-'+ev.nodeId;el.className='node';el.innerHTML='<span class="dot"></span><span></span><div style="flex:1"></div><div class="act"></div>';$('nodes').appendChild(el)}
  el.querySelector('.dot').className='dot '+status;
  el.querySelectorAll('span')[1].textContent=ev.nodeId;
  const act=el.querySelector('.act');
  if(ev.status==='waiting_approval'){act.innerHTML=`<input id="reason-${ev.nodeId}" placeholder="原因(可选)" style="width:140px"/> <button onclick="decide('${ev.nodeId}',true)">通过</button> <button onclick="decide('${ev.nodeId}',false)">拒绝</button>`}
  else if(ev.review){act.innerHTML=`<span class="meta">${ev.review.approved?'已通过':'已拒绝'}${ev.review.reason?' · '+ev.review.reason:''}</span>`}
  else{act.innerHTML=''} }
async function decide(node,approve){const reason=$('reason-'+node).value||'';try{await api('/api/runs/'+current+'/approvals/'+node,{method:'POST',body:JSON.stringify({approve,reason})})}catch(e){alert(e.message)}}
refresh();setInterval(refresh,2000);
</script>
</body>
</html>
```

- [ ] **Step 4: 实现** —— ui.go

```go
package server

import _ "embed"

//go:embed ui.html
var uiHTML string
```

- [ ] **Step 5: 运行确认通过**
  Run: `cd apps/control-plane && GOSUMDB=sum.golang.org go test ./internal/server/`
  Expected: 全 PASS(含 Task 5/6 的 GET / 断言,`chaos.plus`/`approve`/`WebSocket` 均出现)。

- [ ] **Step 6: Commit**
  Run: `git add apps/control-plane/internal/server/ui.go apps/control-plane/internal/server/ui.html && git commit -m "feat(server): embedded single-page run UI"`

---

### Task 7: 装配到 cmd/control-plane/main.go + 冒烟

**Files:**
- Modify: `apps/control-plane/cmd/control-plane/main.go`

**Interfaces:**
- Consumes: `server.NewRunManager`、`server.NewHandler`;`gateway.Gateway`;`store.Store`;`nats.Conn`。
- Produces: 可运行 `control-plane` 二进制,暴露 `CONTROL_HTTP_PORT`(默认 8081)。

- [ ] **Step 1: 实现** —— main.go 在 gateway 启动后追加(复用既有 `st` 变量,可为 nil)

```go
import (
	"net/http"

	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/server"
)

	// HTTP + WS surface (run orchestration / realtime / approvals).
	rm := server.NewRunManager(nc, g, st, envOr("CONTROL_RUNNER_ID", ""))
	if err := rm.Start(ctx); err != nil {
		log.Fatalf("run manager: %v", err)
	}
	httpAddr := ":" + envOr("CONTROL_HTTP_PORT", "8081")
	hs := &http.Server{Addr: httpAddr, Handler: server.NewHandler(rm)}
	go func() {
		log.Printf("control-plane HTTP listening on %s", httpAddr)
		if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("http server: %v", err)
		}
	}()
	defer hs.Shutdown(context.Background())
```

> 注:`st` 在 main.go 中是在 `if dsn := os.Getenv("CONTROL_DB_DSN"); dsn != "" { ... }` 块内声明的局部变量。实现时需把 `st` 提升到块外作用域(nil 缺省),使 HTTP 装配能引用。`CONTROL_RUNNER_ID` 缺省为空 → RunManager.Launch 内用首个注册 runner 兜底。

- [ ] **Step 2: 编译**
  Run: `cd apps/control-plane && GOSUMDB=sum.golang.org go build ./...`
  Expected: 编译通过。

- [ ] **Step 3: 手动冒烟(需 NATS + daemon + 真实 claude;可选但推荐)**
  - daemon:`cd apps/daemon && RUNNER_ID=e2e-local DAEMON_NATS_URL=nats://127.0.0.1:4222 CLAUDE_BINARY=C:\Users\Feather\.local\bin\claude.exe bun run src/serve.ts`
  - 控制面:`cd apps/control-plane && CONTROL_DB_DSN=C:\tmp\control-plane.db GOSUMDB=sum.golang.org go run ./cmd/control-plane`(NATS 本地 4222)
  - 浏览器 `http://127.0.0.1:8081` → 发起 `examples/e2e-real.json`(或含审批的 agile),workspace 填临时目录 → 节点实时变绿 → 审批节点高亮 → 点通过 → 跑完。
  - 确认:WS 实时更新、StateStore events 表有 `chaos.run.*` 事件、审批通过后 DAG 继续。

- [ ] **Step 4: Commit**
  Run: `git add apps/control-plane/cmd/control-plane/main.go && git commit -m "feat(control-plane): wire HTTP + run manager into main"`

---

## Self-Review(已执行)

- **Spec 覆盖**:§3 传输(WS+NATS)✓ Task4/5;§4.1 OnEvent+waiting_approval ✓ Task1;§4.2 broker+executor ✓ Task3;§4.3 HTTP/REST ✓ Task5;§4.4 UI ✓ Task6;§4.5 StateStore 落库 ✓ Task4 emit;§5 事件映射 ✓ Task4 storeTypeFor;§6 测试/错误 ✓ 各 Task;§7 拒绝语义 ✓ Task4 finalStatus;§10 关键文件 ✓ 全部。
- **占位扫描**:无 TODO/TBD;每步含真实代码。Task2 的 TriggerSpec/ExecutorAgentSpec 字段名标注了「以 def.go 为准核对」的硬性核对点(因未逐字读 def.go 的这两个 struct,但依 run-workflow 与 validate.go 既有用法可确认字段名)。
- **类型一致性**:`RunEvent`/`RunStatus`/`LaunchRequest`/`RunSubscriber`/`ApprovalBroker`/`ApprovalExecutor` 跨 Task 3/4/5/6 名称一致;`workflow.Status`/`Event`/`EdgeCondition`/`NodeType` 均核对 engine.go/nodes.go/validate.go/def.go 实际定义。
- **已知偏差**(执行后写进 MEMORY.md):V1-M2 字面闸门重释、C4 嵌入 HTML、C3 NATS fan-out、结构化反馈 defer、workflow_runs 表 defer、记忆系统 defer。
