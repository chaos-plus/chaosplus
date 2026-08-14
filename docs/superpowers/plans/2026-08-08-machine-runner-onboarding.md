# Machine Runner 接入 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development (recommended) or superpowers:executing-plans. Steps use checkbox (`- [ ]`).

**Goal:** runner↔控制面传输改为 WS+token(PRD §5.3.1 接入流程),NATS 内部化;claude/codex CLI 自动检测。外部只暴露控制面 HTTP/WS。

**Architecture:** runner 只实现 `machine/WsDaemonTransport`，中立 command/event contract 归 `machine/protocol.ts`，启动参数仅 `--server/--token/--name`。WebSocket 是稳定的 runner wire protocol；workflow 只依赖 `RunnerLink`，内部 cluster bus adapter 由 composition root 注入且不暴露给 runner，可由 NATS、MQTT 或其他满足合同的实现提供。多实例最终态使用共享 token/connection directory、单活动 lease 与 fencing。

**Tech Stack:** Go 控制面(gorilla/websocket 已引入)、TS runner(bun 自带 WebSocket)、SQLite(bunx/goosex 既有)。

## Global Constraints

- runner 侧只保留中立 `DaemonTransport` contract 和 WebSocket 实现，不保留 broker transport。
- 命令/事件契约复用现有 `RunnerCommand`/`RunnerEvent`(TS)与 `Spawn`/`RunnerEvent`(Go)类型。
- 执行器 Agent 环境不注入 token(§17.2)。
- 心跳 15s;接入 token 300s(±5s)。
- 不引入新 Go 依赖(gorilla 已有);runner 不新增 npm 依赖(bun WebSocket 内置)。
- Go:gofmt/goimports + `-race`;TS:typecheck + bun test。
- conventional commits,无 Co-Authored-By。

---
**Step 0(任何 Task 前):基线**
- `cd apps/server-ai && GOSUMDB=sum.golang.org go test ./...` 全绿
- `cd apps/runner && bun run typecheck && bun test` 全绿

---

### Task 1: runner — WsDaemonTransport + CLI 解析

**Files:**
- Create: `apps/runner/src/machine/client.ts`
- Create: `apps/runner/src/machine/client.test.ts`
- Modify: `apps/runner/src/serve.ts`(CLI 解析 + 换传输)
- Create: `apps/runner/src/machine/protocol.ts`（拥有 `DaemonTransport`/`RunnerCommand`/`RunnerEvent`）
- Delete: `apps/runner/src/nats/` 与 runner `nats` dependency

**Interfaces:**
- `DaemonTransport`（归 machine protocol owner）:
  ```ts
  interface DaemonTransport {
    connect(handler: (cmd: RunnerCommand, reply: (ok: boolean, data?: unknown) => void) => Promise<void> | void): Promise<void>;
    register(meta: Record<string, string>): Promise<void>;
    publish(event: RunnerEvent): void;
    close(): void;
  }
  ```
- WS 协议(`ws://{host}/api/machines/ws?token=<t>&name=<n>`):
  - 控制→runner:`{type:"cmd", reqId, cmd:<RunnerCommand>}`
  - runner→控制 reply:`{type:"reply", reqId, ok, data?}`
  - runner→控制 event:`{type:"event", event:<RunnerEvent>}`
  - 控制→runner ping:`{type:"ping"}` → runner `{type:"pong"}`

- [ ] **Step 1: 写失败测试**：由真实 server-ai machine hub/HTTP listener 与真实 runner client 组成跨进程 harness，禁止自建控制面对端。
  测试用例:
  1. `connect` 后收到 `spawn` cmd 时 handler 被调用,`reply(true,{x:1})` 发送回 `{type:"reply",reqId,...}`。
  2. `publish({type:"heartbeat",ts:1})` 发送 `{type:"event",event:{...}}`。
  3. `register(meta)` 发送 `{type:"register",meta}`。
- [ ] **Step 2: 运行确认失败**
  Run: `cd apps/runner && bun test src/machine/` → FAIL(client.ts 不存在)。
- [ ] **Step 3: 实现 client.ts**(见下)

```ts
import type { RunnerCommand, RunnerEvent } from "./protocol";

export type WsReply = (ok: boolean, data?: unknown) => void;

export class WsDaemonTransport {
  private ws?: WebSocket;
  constructor(private host: string, private token: string, private name: string) {}

  async connect(handler: (cmd: RunnerCommand, reply: WsReply) => Promise<void> | void): Promise<void> {
    const url = this.host.replace(/^http/, "ws") +
      `/api/machines/ws?token=${encodeURIComponent(this.token)}&name=${encodeURIComponent(this.name)}`;
    this.ws = new WebSocket(url);
    await new Promise<void>((res, rej) => {
      const t = setTimeout(() => rej(new Error("ws connect timeout")), 5000);
      this.ws!.onopen = () => { clearTimeout(t); res(); };
      this.ws!.onerror = () => { clearTimeout(t); rej(new Error("ws connect error")); };
    });
    this.ws.onmessage = (ev) => void this.dispatch(ev.data, handler);
  }

  async register(meta: Record<string, string>): Promise<void> { this.send({ type: "register", meta }); }
  publish(event: RunnerEvent): void { this.send({ type: "event", event }); }
  close(): void { this.ws?.close(); }

  private send(m: unknown): void {
    if (this.ws?.readyState === WebSocket.OPEN) this.ws.send(JSON.stringify(m));
  }

  private async dispatch(raw: unknown, handler: (cmd: RunnerCommand, reply: WsReply) => Promise<void> | void): Promise<void> {
    const msg = JSON.parse(String(raw)) as any;
    if (msg.type === "cmd") {
      const reply: WsReply = (ok, data) => this.send({ type: "reply", reqId: msg.reqId, ok, data });
      try { await handler(msg.cmd, reply); } catch (e) { reply(false, { error: (e as Error).message }); }
    } else if (msg.type === "ping") {
      this.send({ type: "pong" });
    }
  }
}
```
- [ ] **Step 4: 运行确认通过**
  Run: `cd apps/runner && bun test src/machine/` → PASS;`bun run typecheck` 干净。
- [ ] **Step 5: serve.ts 换传输**(解析 `--server/--token/--name`;保留既有命令处理逻辑,换 `WsDaemonTransport`;删除 NATS transport/dependency 与 `DAEMON_NATS_URL` 路径)
  Run: `cd apps/runner && bun run typecheck && bun test` 全绿。
- [ ] **Step 6: Commit**
  Run: `git add apps/runner/src/machine apps/runner/src/serve.ts apps/runner/package.json && git commit -m "feat(runner): WS runner transport + --server/--token CLI"`

---

### Task 2: runner — CLI 自动检测 claude/codex

**Files:**
- Create: `apps/runner/src/backends/detect.ts` + `detect.test.ts`
- Modify: `apps/runner/src/backends/claude.ts`, `apps/runner/src/backends/codex.ts`

**Interfaces:**
- `detectBinary(names: string[], envVar?: string): Promise<string | undefined>`:PATH 探测(`process.env.PATH` 拆目录,找 `names` 匹配的可执行;Windows 试 `.exe/.cmd`);`envVar` 非空直接返回。
- claude.ts:`CLAUDE_BINARY` → PATH 找 `claude` → 未找到报清晰错误。
- codex.ts:`CODEX_BINARY` → PATH 找 `codex`。

- [ ] **Step 1: 写失败测试**(detect.test.ts):临时目录造 `claude`/`codex` 占位文件,prepend PATH,断言探测到;env override 优先。
- [ ] **Step 2: 失败** → Step 3: 实现 detect.ts + 接入 claude.ts/codex.ts → Step 4: 通过 → Step 5: typecheck + bun test 全绿 → Step 6: commit `feat(runner): auto-detect claude/codex CLI binaries`

---

### Task 3: 控制面 — TokenStore + MachineStore(§5.3.1 状态机)

**Files:**
- Create: `apps/server-ai/internal/machine/token.go` + `token_test.go`
- Create: `apps/server-ai/internal/machine/store.go` + `store_test.go`
- Modify: `apps/server-ai/internal/store/sql/sqlite/00001_events.sql`(追加 machine_runners 迁移,或新增 `00002_machine_runners.sql`)

**Interfaces:**
```go
// token.go
type AccessToken struct { Token string; MachineID string; LongTerm bool; ExpiresAt time.Time }
type TokenStore struct{} // 内存 map[tokenHash]*AccessToken + 过期清理
func NewTokenStore() *TokenStore
func (t *TokenStore) Issue(machineID string) AccessToken             // 一次性 300s
func (t *TokenStore) MakeLongTerm(machineID, token string) error     // 一次性→长期
func (t *TokenStore) Validate(token string) (*AccessToken, error)    // 未过期一次性 / 长期
func (t *TokenStore) Invalidate(machineID string)                    // cancel/timeout
func hashToken(s string) string                                       // sha256 hex

// store.go (bun)
type Machine struct { ID, InstanceID, Address, Status string; LastHeartbeatAt time.Time; TokenHash string }
type MachineStore struct{ db *bun.DB }
func (s *MachineStore) Confirm(m Machine) error
func (s *MachineStore) List(ctx) ([]Machine, error)
func (s *MachineStore) Heartbeat(ctx, id string) error
func (s *MachineStore) Remove(ctx, id string) error
```
- Baseline 状态机:一次性 token 已连未确认 = `pending`;confirm → `confirmed` + 长期 token;cancel/timeout → Invalidate + 断开;pending 不出现在 List。原 Task 3 的进程内 pending 实现只满足单实例基线，已被 Phase R 的共享 onboarding repository 目标取代。

- [ ] **Step 1: 写失败测试** — token:Issue 300s、Validate 接受未过期/长期/拒绝过期与伪造、MakeLongTerm 后可重连、Invalidate 后失效。store:Confirm→List、Heartbeat、Remove、重复 Confirm 幂等。
- [ ] **Step 2: 失败** → Step 3: 实现 token.go/store.go(+迁移) → Step 4: 通过 + `-race` → Step 5: 全绿 → Step 6: commit `feat(machine): token issuance + machine store (§5.3.1)`

---

### Task 4: 控制面 — WS runner hub

**Files:**
- Create: `apps/server-ai/internal/machine/hub.go` + `hub_test.go`

**Interfaces:**
```go
type Hub struct {
  mu sync.Mutex
  conns map[string]*daemonConn  // runnerID -> ws
  tokens *TokenStore
  machines *MachineStore
  pending map[string]string     // machineID -> runnerID
  events chan RunnerEvent
}
func NewHub(tokens *TokenStore, machines *MachineStore) *Hub
func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request)
func (h *Hub) RegisteredRunners() []string
func (h *Hub) Events() <-chan RunnerEvent
```
- 握手:`?token=&name=` → `tokens.Validate` → 成功则注册 `conns[runnerID]=conn`,发 `{type:"ready"}`;pending 记入。
- 读写循环:控制→runner cmd(reqId) ↔ runner reply/event;event(含 heartbeat)→ `h.events` + `machines.Heartbeat`。
- 断线:移除 conn;pending 未确认 → token 失效(超时/异常路径)。

- [ ] **Step 1: 写失败测试** — httptest + gorilla dialer:有效一次性 token → `ready`;发 spawn cmd → reply;runner event → `h.events`;无效/过期 token → 拒绝;断线 → RegisteredRunners 不含。
- [ ] **Step 2: 失败** → Step 3: 实现 hub.go → Step 4: 通过 + `-race` → Step 5: 全绿 → Step 6: commit `feat(machine): WS runner hub`

---

### Task 5: 控制面 — RunnerLink 抽象 + WS-backed RunnerExecutor

**Files:**
- Modify: `apps/server-ai/internal/workflow/runner_executor.go`
- Create: `apps/server-ai/internal/machine/runnerlink.go` + `runnerlink_test.go`

**Interfaces:**
```go
// workflow 包
type RunnerLink interface {
  SpawnAndWaitOpts(ctx, runnerID string, sp gateway.Spawn, opts ...gateway.SpawnWaitOption) (gateway.SpawnResult, error)
  Kill(ctx, runnerID, spawnID string) error
  ReadArtifact(ctx, runnerID, spawnID, path string) ([]byte, error)
  RunCmd(ctx, runnerID, spawnID, cmdTemplate string, timeoutMs int) (gateway.CmdResult, error)
  RegisteredRunners() []string
}
// machine 包:WS 实现。SpawnAndWaitOpts 经 conn 发 cmd + 等 spawn-done(复用 gateway idle/max 逻辑)。
```
- `RunnerExecutor.link RunnerLink`(替换具体 gateway);`NewRunnerExecutor` 接 link。`GatewayRunnerLink` 适配当前控制面 gateway，provider 名称不进入业务 port。

- [ ] **Step 1: 写失败测试** — 启动真实 hub、gateway、runner 与 script backend，验证 SpawnAndWaitOpts、spawn-done、idle timeout、ReadArtifact/RunCmd 往返。
- [ ] **Step 2: 失败** → Step 3: 抽象接口 + 改 runner_executor.go + gateway adapter → Step 4: 通过 + `-race` + 既有 workflow 测试绿 → Step 5: commit `feat(machine): RunnerLink WS-backed executor`

---

### Task 6: 控制面 — machines HTTP/WS 端点 + 嵌入 UI

**Files:**
- Modify: `apps/server-ai/internal/server/server.go`
- Modify: `apps/server-ai/internal/server/ui.html` + `ui.go`
- Modify: `apps/server-ai/cmd/server-ai/main.go`
- Test: `apps/server-ai/internal/server/machines_test.go`

**API**(spec §6):
```
POST /api/machines/tokens             → 201 {token, machineId, expiresIn:300}
GET  /api/machines/ws                 → WS upgrade(hub)
POST /api/machines/{id}/confirm       → 200(幂等)
DELETE /api/machines/{id}             → 200(cancel:失效+断开)
POST /api/machines/{id}/refresh-token → 200 {token, expiresIn}
GET  /api/machines                    → [{id,name,address,status,lastHeartbeatAt}]
```
- UI:machines 区。添加向导:POST tokens → 显示命令 `bun run src/serve.ts --server <host> --token <t>` + 倒计时 300s(确认/取消/刷新;未连接不可确认;超时禁用)。列表:在线/离线 + 最近心跳。确认 → 进列表。
- main.go:装配 machine hub/store,传入 server.NewHandler 与 RunManager(RunnerLink)。

- [ ] **Step 1: 写失败测试**(machines_test.go):POST tokens → token;GET machines 空;真实 runner 进程用该 token 连 WS → confirm → GET machines 有 1 条在线;DELETE → 消失;confirm 幂等;过期 token → Validate 拒绝。
- [ ] **Step 2: 失败** → Step 3: 实现路由 + hub 装配 + UI → Step 4: 通过 + `-race` → Step 5: `go build ./...` 全绿 → Step 6: commit `feat(server): machine onboarding endpoints + UI`

---

### Task 7: 端到端冒烟 + 收尾

- [ ] **Step 1: 全量回归** `go test ./...`(控制面)+ `bun run typecheck && bun test`(runner)
- [ ] **Step 2: 手动冒烟(真实 claude)**:UI 添加 machine → 倒计时内 runner `--token` 连上 → 确认 → 列表在线 → 发起 workflow run → 真实 claude 经 WS 执行 → 审批通过 → 关 runner 显示离线 → 重连恢复
- [ ] **Step 3: 更新 MEMORY.md**
- [ ] **Step 4: commit**(若有收尾改动)

---

## Self-Review

- **Spec 覆盖**:§2 传输 → Task1/4/5;§3 接入流程 → Task3/4/6;§4.1 CLI 自动检测 → Task2;§4.2 存储 → Task3 迁移;§4.3/§6 UI+API → Task6;§7 验收 → Task7。
- **接口一致性**:`DaemonTransport`(TS)与 `RunnerLink`(Go)均从既有实现提取;契约复用 `RunnerCommand/RunnerEvent/Spawn`。`hashToken`=sha256;`AccessToken.ExpiresAt`=time.Time。
- **占位**:Task1/3/4 标注「按协议/按 gateway 逻辑回填」处为复用既有代码,无 TBD;执行以实际文件为准。
- **偏差**:machine 详情「托管 agent」列表 defer；会话 Bearer token 不在本轮。
- **2026-08-14 集群修订**:runner NATS 链路已删除；WebSocket 保持唯一公开 runner wire protocol，控制面内部 adapter 保持 transport-neutral；共享 onboarding token、runtime/scope directory、connection lease/fencing 和双实例真实 E2E 转入 Phase R，在完成前不得声明集群可用。
