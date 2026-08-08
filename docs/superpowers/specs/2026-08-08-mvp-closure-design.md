# MVP 闭环设计 —— Web 发起 run + 实时进度 + 人工审批

日期:2026-08-08
状态:Draft
参照:PRD v2.0 (定稿),§1.4 / §2 / §3 / §4(C2/C3/C9) / §7.3 / §13 / §15.1 / §21.1 / §23.A / F.2 / F.4 / F.5

## 1. 目标

从控制面 Web UI 发起一个静态 DAG 工作流 run,对真实执行器(engine→NATS→daemon→claude)运行,浏览器实时看到 DAG 节点进度,并在 `human_approval` 节点完成一次真人工审批(通过/拒绝)。达成 PRD J6→J10 的核心闭环。

## 2. 范围

### 做
- 引擎加 `OnEvent` 钩子,实时吐节点生命周期事件
- `ApprovalBroker` + `ApprovalExecutor`:审批真阻塞,HTTP 解析
- 控制面 HTTP 服务:REST(起 run / 审批 / 列表)+ WebSocket(实时事件)
- 事件经 **NATS fan-out**(subject `chaos.run.{runID}.evt`),任意实例的 WS 客户端可收
- 事件必写 StateStore events 表(映射 F.2 类型 + 幂等键)
- 单页 HTML UI(嵌入控制面):run 列表 + 发起表单 + 节点进度 + 审批卡
- `validate.go` 补规则:human_approval 节点出边必须为 `approved`/`rejected`
- 拒绝语义:onReject=「pause」→ run 暂停(status=paused),发 REVIEW_REJECTED + reason

### 不做(YAGNI,记录于 §9)
- 结构化反馈 schema(category/location/expected/detail)注入下次执行 —— 本轮只 reason 字符串
- onTimeout 状态机、审批可重访/重决定 UI
- workflow_runs 表 + 重启回放(run 列表内存态)
- 多用户/auth、数字人、记忆系统(聊天/agent memory 落库)、Next.js UI

## 3. 传输层(关键决策)

- 客户端实时通道 = **WebSocket**(PRD C2:控制面↔客户端实时流走 WebSocket;SSE 无法在 Go 集群扇出,弃用)。
- 控制面内部 fan-out = **NATS**(PRD C3:cloud/self-hosted profile 的传输/扇出层)。引擎事件 → `chaos.run.{runID}.evt` → 控制面 server 订阅 → 按 runID 路由 → 推给 WS 客户端。集群:任意实例跑的引擎,任意实例挂的客户端都能收。
- 新依赖:`gorilla/websocket`(唯一新增;Go stdlib 无 WS)。

> **记录偏差**:PRD C3 称 desktop profile 进程内直连、不引入 NATS 硬依赖。当前实际部署(控制面↔daemon)本就以 NATS 为骨架,且为保留集群能力,本轮 run 事件 fan-out 走 NATS。若日后落纯 desktop 无-NATS profile,WS hub 改为进程内直投即可(改动很小)。

## 4. 组件

### 4.1 引擎(`internal/workflow/engine.go`)—— 唯一引擎改动
- `Engine` 加字段 `OnEvent func(Event)`;`mark()` 内调用。调度/readiness 逻辑零改动。
- `Status` 枚举加 `StatusWaitingApproval = "waiting_approval"`(非终态)。
- `execApproval`:先 `mark(StatusWaitingApproval)`(走单一事件路径,UI 卡映射 F.2 `REVIEW_REQUESTED`),再调 `exec.Approve(ctx, node)` 阻塞;resolve 后 `ok` → `mark(StatusCompleted)`;`!ok` → 按 onReject 语义(见 §7)。

### 4.2 审批代理(`internal/workflow/approval.go`)
- `ApprovalBroker`(每 run 一个):`map[nodeID]chan decision` + mutex。
  - `Wait(ctx, nodeID) (ok bool, reason string, err error)`:阻塞直到 `Resolve` 或 ctx 取消。
  - `Resolve(nodeID, ok, reason) error`:投递决定;重复 resolve → 错误(HTTP 409)。幂等。
- `ApprovalExecutor`:包装真实 `RunnerExecutor`(其 `Approve` 现硬编码 true,见 `runner_executor.go`),`Approve` 改为阻塞在 broker 上。引擎 execApproval 本来就在调 `exec.Approve`,此处不改引擎。

### 4.3 控制面 HTTP 服务(`internal/server`)
stdlib net/http,端口 `CONTROL_HTTP_PORT`(默认 8081)。

```
GET  /                                  → 嵌入单页 UI
POST /api/runs                          → {workflowFile, workspace, context?} → 201 {runID}
GET  /api/runs                          → run 列表(侧栏)
GET  /api/runs/:id/events               → 升级 WebSocket:回放缓冲 + 实时推送
POST /api/runs/:id/approvals/:nodeID    → {approve, reason?} → 200 / 404 / 409
```

- `RunManager`:run 注册表(每 run = def + engine + 事件缓冲 + WS 订阅者 + broker + status)。每 run 一个 goroutine 跑 `engine.Run`(天然满足 §15.1 per-run 单写者)。
- 启动装配复用 `cmd/run-workflow` 的路径:LoadValidate → `RunnerExecutor`(gateway+runnerID+workspace)→ `ApprovalExecutor` 包装 → `NewEngine` → goroutine `Run`。
- 事件流:引擎 `mark()` → `OnEvent` → RunManager 缓冲 + **发布 NATS `chaos.run.{runID}.evt`** + 落 StateStore。server 订阅 `chaos.run.*.evt` → 按 runID 推给 WS 订阅者。
- 审批解析:`POST approvals/:node` → broker.Resolve → 引擎继续。

### 4.4 单页 UI(嵌入 HTML)
深色风格(与 daemon UI 一致)。左侧 run 列表;右侧发起表单(工作流文件路径 + workspace + context JSON)+ run 详情(节点卡按状态着色:pending/running/completed/failed/skipped/**waiting_approval**);审批卡 = 节点身份 + 上游产物路径/摘要 + 通过/拒绝 + reason 输入。WS 自动跟进,不轮询。

### 4.5 StateStore 落库(必做)
- 引擎生命周期事件 + REVIEW_*/RUN_PAUSED 事件映射到既有 `events` 表(§16 结构),`run_id` 按 run 隔离,幂等键防重。复用 `store.Append`。
- run 列表仍内存态(workflow_runs 表 defer,见 §9)。

## 5. 事件类型(F.2 映射)

| 引擎侧 | Store/协议侧 |
|---|---|
| mark(status=completed/failed/skipped) | NODE_COMPLETED / NODE_FAILED / NODE_SKIPPED |
| mark(status=waiting_approval) | REVIEW_REQUESTED |
| resolve ok | REVIEW_APPROVED |
| resolve !ok | REVIEW_REJECTED(+ reason) |
| reject + onReject=pause | RUN_PAUSED |

## 6. 错误处理 / 测试

- 404(run 不存在)、409(审批已处理)、400(工作流无效/workspace 缺失)。
- 节点失败 / deadlock → 事件流呈现 + run 终态;引擎错误只影响该 run,不影响 server。
- 测试(Go,table-driven + `-race`):
  - broker:Wait 阻塞→Resolve→返回;重复 Resolve 报错;ctx 取消。
  - 引擎+broker(Mock 基座):审批暂停→通过继续;拒绝+onReject=pause → run 暂停、下游不跑。
  - validate:审批节点出边非 approved/rejected → 拒绝加载。
  - HTTP:POST 起 run、WS 回放+实时、审批解析、404/409。
- 验收闸门:浏览器开控制面 → 选工作流+workspace → 运行 → 真实 claude 执行、节点实时变绿 → 审批节点高亮「等待审批」→ 点通过 → DAG 继续至完成;审批链走完。

## 7. 拒绝语义(修正)

`human_approval` 节点 resolve 为 reject 且 `HumanApprovalSpec.OnReject == "pause"` → **run 暂停**(status=paused,非静默完结),发 REVIEW_REJECTED + reason,下游不执行。reject→`rejected` 边路由仅在模板显式声明 rejected 出边时生效(validate 规则强制)。审批「可重访/重决定」UI defer(§9)。

## 8. 架构一致性

- 控制面 = 唯一状态权威(P1):HTTP 处理器全走控制面,客户端不直连 StateStore。
- 执行恒为静态 DAG:聊天/UI 是作者面,收敛到同一份 WorkflowDef JSON。
- daemon 的 1v1 聊天 UI(SSE)为既存独立 dev 面,本轮不动,不计入 §9 IM/回灌需求。

## 9. 记录偏差 / defer 清单

| 项 | 状态 | 说明 |
|---|---|---|
| V1-M2 字面闸门(@数字人/聊天回灌) | 重释 | 本轮 = 真实闭环(launch→progress→一次真审批);聊天触发、@数字人、审批回灌后续 |
| C4 Next.js UI | 偏差 | MVP 用控制面嵌入单页 HTML;Next.js 瘦客户端后续 |
| C3 desktop 无-NATS | 偏差 | run 事件 fan-out 走 NATS;纯 desktop profile 落时 WS hub 进程内直投 |
| 结构化反馈 schema | defer | 本轮 reason 字符串;category/location/expected/detail 后续 |
| onTimeout / 审批重访 | defer | 本轮无超时状态机、无重决定 UI |
| workflow_runs 表 + 重启回放 | defer | run 列表内存态;事件已落 events 表,可重建 |
| 记忆系统(聊天/agent memory 落库) | defer | 独立子项目,后续轮次 |

## 10. 关键文件

- `apps/control-plane/internal/workflow/engine.go`(OnEvent 钩子 + waiting_approval 状态)
- `apps/control-plane/internal/workflow/approval.go`(新:broker + executor)
- `apps/control-plane/internal/workflow/validate.go`(审批出边规则)
- `apps/control-plane/internal/server/`(新:HTTP + WS + NATS 扇出 + RunManager + 嵌入 HTML)
- `apps/control-plane/cmd/control-plane/main.go`(装配 HTTP)
- 依赖:`gorilla/websocket`
