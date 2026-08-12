# server-ai / admin-ai 生产就绪评审

- 评审日期：2026-08-12
- 评审范围：`apps/server-ai`、`apps/admin-ai`，以及端到端执行必需的 `apps/runner`
- 规格基线：`PRD.md` v2.0 Final（2026-08-01）
- 代码基线：`myrmidon` 分支，本轮整改后的工作树
- 发布结论：**NO-GO，不满足生产级发布条件，PRD v1 未 100% 实现**

## 1. 执行摘要

本轮已经把可在当前架构内闭环的高风险问题直接整改，并完成后端竞态测试、静态分析、前端构建测试和一条真实的 NATS + server-ai + runner 工作流链路。Machine 接入、基础 DAG 执行、人工审批、Artifact 持久化与 stale 传播、Run 暂停/恢复/取消、事件投影重建、多租户过滤等核心能力已经具备可运行实现。

但当前状态不能判定为 production-ready。主要原因不是代码无法运行，而是 PRD 定义的若干执行核不变量尚未实现：ExecutorAgentSpec 的能力边界只做了 fail-closed，`subworkflow` 未执行，自治收敛策略缺少相似度/振荡检测，调度缺 per-run lease 与 fencing，事件与投影没有统一原子提交，runner 心跳失效恢复不完整。此外，生产身份链路、可观测性、限流、备份恢复、压测、桌面三平台打包和 J1-J10 人工演练仍缺发布证据。

**发布建议：** 当前版本可作为开发/受控演示版本；不得作为无人值守生产自治系统对外承诺。

## 2. PRD v1 覆盖矩阵

状态定义：

- **已实现**：存在运行时代码和自动化或真实链路证据。
- **部分实现**：主路径可用，但缺少规格中的硬约束、异常恢复或验收证据。
- **安全拒绝**：提交前明确拒绝不支持的配置，避免静默绕过；这不等于功能实现。
- **未实现/未证明**：没有实现，或缺少足以支持发布的验收证据。

| PRD 能力 | 状态 | 评审结论与证据 |
|---|---|---|
| Machine 5 分钟接入状态机（§5.3.1） | 已实现 | pending/connected/confirmed/expired/cancel 路径、token 刷新与 UI 倒计时已覆盖；真实 runner 连接和确认通过。token 使用 32-byte CSPRNG、Raw URL Base64（256-bit）。 |
| Machine 租户隔离（P10） | 已实现 | pending 与 confirmed machine 的读取、刷新、取消、删除均校验租户；新增跨租户 HTTP 回归测试。 |
| WorkflowDef JSON Schema（§7.1/F.4） | 部分实现 | 启动前做 schema 和上下文 schema 校验；部分 Schema 字段虽可表达，但运行时不执行。 |
| 静态 DAG 与基础节点（§7.2/7.3） | 部分实现 | trigger、agent、human approval、condition、fork/join、transform、loop 主体存在；`subworkflow` 在提交阶段拒绝。 |
| ExecutorAgentSpec 硬约束（§6.1.1/F.5） | 安全拒绝 | `allowedTools`、`maxTurns`、attempt/checksum 等已下发；`forbiddenActions`、`allowedMCPTools`、`requiredSkills`、hooks、inputValidator 尚不能强制，当前在 `validate.go` fail-closed。 |
| Artifact 生命周期与依赖（§10/F.9） | 部分实现 | Artifact 表、required output 检查、checksum、依赖、stale 传播、force-valid 与审计已实现；完整跨后端 ArtifactStore 与 orphaned 流程未证明。 |
| 三层 Validator（§11） | 部分实现 | `cmd:` automated validator 和 human approval 可运行；ai_assisted validator 及通用 validator adapter 未实现。 |
| Reconciliation（§12） | 部分实现 | 周期扫描、缺失检测和 stale 传播已具备；当前固定 30 秒，不符合默认 5 分钟，且缺事件驱动 debounce/batch/mtime 分层、Phantom Running 和孤立 worktree 完整治理。 |
| 有界自治（§13） | 部分实现 | maxAttempts/backoff、耗尽后 paused、审批 timeout/retry exhausted 已实现；`notifyThreshold` 未消费，相似度 `<0.08`、振荡和连续缺失产物 stuck 检测未实现。 |
| Run 生命周期（F.3） | 已实现（单实例范围） | pause/resume/cancel API、状态恢复和租户隔离已有 HTTP 集成测试；真实链路从 started 到 completed 通过。 |
| 事件溯源与重建（§15.1） | 部分实现 | `RUN_STARTED` 保存完整 snapshot，核心投影可从事件重建并有 WRT 测试；事件 append 与投影更新不是统一事务。 |
| per-run lease/fencing（§15.1） | 未实现 | 缺少租约持有者、fencing token 和过期写拒绝，不能满足多控制面/故障转移不变量。 |
| 崩溃与 runner 失联恢复（§12/15） | 部分实现 | 已有启动恢复和故障注入测试；runner heartbeat 虽持久化，但无心跳阈值到 node failed/retry 的完整闭环。 |
| 多租户数据边界（P10） | 部分实现 | workflow/run/artifact/machine 的主要接口已过滤 instance/project；没有完整 RBAC、全路由 IDOR 测试矩阵和云 profile 验收。 |
| 生产认证（§17.2/F.7） | 部分实现 | 非 loopback 强制 token，生产要求 `CONTROL_AUTH_HARDENED=1`，支持可信代理 identity；浏览器端 `VITE_CONTROL_API_TOKEN` 会进入 bundle，WebSocket 身份传递仍依赖未交付的反向代理契约。 |
| NATS/JetStream（§17） | 已实现（本地链路） | 支持内嵌 NATS/JetStream、认证/TLS 配置、health/readiness；真实连接通过。生产集群拓扑未验收。 |
| Admin Runs/Approvals/Machines UX | 部分实现 | 核心操作、错误/空/加载态、响应式和 i18n 已整改；缺真实浏览器跨视口与辅助技术验收。 |
| Desktop profile 与三平台交付（J1） | 未证明 | 未发现 Win/macOS/Linux 安装包、签名/公证、自动更新和首次启动验收证据。 |
| 可观测性与运维（E.1） | 未实现/未证明 | 缺业务指标、资源指标、分布式 tracing、告警规则、备份恢复演练和生产 runbook。 |
| 性能与稳定性门槛（E.1） | 未证明 | 未执行 PRD 的 mock 10 并发 run × 100 节点压测及 8 小时 soak test。 |

## 3. J1-J10 端到端旅程

| Journey | 状态 | 结论 |
|---|---|---|
| J1 桌面应用下载和首启 | 未证明 | 缺三平台安装、内置 runner 自动启动、首次实例创建的产物和人工记录。 |
| J2 新建项目并选择 git workspace | 部分实现 | workspace/control API 存在；目录不存在时仍会到 runner 阶段才失败，应在启动 Run 前返回清晰 4xx。默认 `# ALL` 全链路未验收。 |
| J3 接入第二台 Machine | 已实现 | 300 秒 token、连接、确认、刷新、取消和过期状态已实现并实测。 |
| J4 创建 workflow-only 数字人 | 部分实现 | 管理页面和基础数据能力存在；绑定 runtime、进程托管、生命周期/注销交接 artifact 的完整规格未验收。 |
| J5 API key 使用 `$env` 引用 | 未证明 | 未完成 `.chaosplus.env` 创建、权限、脱敏和 UI 配置的干净机器演练。 |
| J6 在聊天中 @数字人发起模板 | 部分实现 | 聊天和路由代码存在；从 `# ALL` 到数字人作者期生成/选择 `software-dev-agile` 的用户链路未端到端验收。 |
| J7 聊天审批 PRD/架构 | 部分实现 | HTTP 审批和审批 UI 可用；审批卡片回灌聊天与 token 身份的完整链路未证明。 |
| J8 离开后自治执行 | 部分实现 | 真实 runner + mock workflow + validator/approval 已完成；并行 worktree、长时无人值守与所有自治收敛规则未满足。 |
| J9 通知后介入 | 部分实现 | retry exhausted 可进入 paused，拒绝要求结构化反馈；notifyThreshold/IM 通知没有运行时消费。 |
| J10 验收、artifact valid、重放 | 部分实现 | 审批完成、事件序列和投影重建已有测试；完整 J1-J10 干净机人工演练未完成。 |

## 4. 本轮已完成整改

### server-ai / runner

- 增加内嵌 NATS/JetStream、认证/TLS、health/readiness 与配置校验。
- 实现 Machine onboarding 状态机，并修复 pending/confirmed machine 跨租户 IDOR。
- 将 Machine token 提升为严格 256-bit CSPRNG token。
- 增加 Artifact 存储、依赖、stale 传播、reconciliation 和 force-valid 审计。
- 增加 Run pause/resume/cancel、完整启动 snapshot、崩溃恢复和核心 projection rebuild。
- 增加 workflow/run/artifact 租户过滤和 `00018_workflow_tenancy.sql`。
- 对尚未执行的 ExecutorAgentSpec 硬约束改为提交前 fail-closed。
- 实现 `cmd:` automated validator，并让审批 timeout/retry exhausted 收敛到 paused。
- 下发 runner attempt/checksum/allowedTools/maxTurns；修复 mock backend 未写 `output.json` 导致真实 Run 失败的问题。
- 清理 Bun API 弃用与 Go/TS 中未处理的 Close/respond/Setenv 错误。

### admin-ai UI/UX/UE

- Machine wizard 对 expired/invalid 状态禁止复制和确认，提供刷新命令；刷新/重新添加前取消旧 pending 流程。
- Runs、Approvals、Run Detail 补齐 loading/error/retry/empty 状态，并用 toast/内联错误替代阻塞 `alert`。
- Run Detail 统一走 `controlApi`，增加 pause/resume/cancel、实时状态和审批防重复提交。
- 补齐卡片/链接键盘 focus、语义标签、错误 `role=alert` 和常用触控目标最小 44px。
- 改善移动端 header、entity selector、用户菜单和 React Flow 容器的响应式约束。
- 修复暗色主题 focus ring 对比度；Theme button 达到 44px 并具备可见焦点。
- Runs、Approvals、Run Detail 接入简中、英语、马来语 i18n。

## 5. UI/UX/UE 评审结论

### 已达到的基线

- 核心工作流页面已具备异步状态闭环，不再把网络失败表现为空数据。
- 危险操作和审批操作具备禁用/进行中反馈，减少重复提交。
- Machine onboarding 的状态、倒计时和可执行动作保持一致，过期命令不会继续暴露为可复制操作。
- 常用交互改善了键盘可达性、焦点可见性、触控尺寸和移动端布局。
- Run 状态变化可实时反馈，列表、详情与审批之间的信息架构更一致。

### 剩余体验风险

- workspace 在 runner 执行时才报告路径不存在，错误发生得太晚，用户难以定位；应在 Run 创建时校验并给出字段级错误。
- 生产认证的浏览器体验未闭环。构建时 token 不是用户会话，刷新、过期、退出和 WebSocket 续连没有统一身份模型。
- 数字人管理、聊天审批回灌、默认频道和首次启动仍未完成以 J1-J10 为脚本的真实用户任务测试。
- 缺少桌面窄屏、平板、宽屏、暗色、高对比度、键盘-only、屏幕阅读器的截图与交互证据。
- 本环境没有暴露 Browser 插件的可调用浏览器入口，因此本轮不能执行真实 DOM/视口/截图验收。静态代码与构建通过不能替代该项。

## 6. 安全、可靠性与运维发现

### P0 发布阻断

1. **执行边界未强制。** `forbiddenActions`、MCP 白名单、required skills、hooks、input validator 目前只能拒绝整个 workflow，无法按 PRD 执行。
2. **调度一致性不满足规格。** 没有 per-run lease/fencing；多实例或故障接管时无法证明单写者，存在重复执行/脑裂风险。
3. **事件原子性不足。** 事件 append 与 projection 更新未统一事务，崩溃窗口可能出现日志与查询状态暂时或永久不一致。
4. **失联恢复不完整。** runner heartbeat 失效到 Phantom Running 标记失败、重试/暂停的闭环未实现。
5. **生产身份链路不完整。** 必须交付并验证 TLS 终止、可信代理 header 清洗、HTTP/WS identity 注入和用户会话模型；不得把 `VITE_CONTROL_API_TOKEN` 当生产秘密。
6. **缺少生产验收。** 三平台桌面包、10×100 压测、8 小时 soak、备份恢复、全 Journey 演练均未完成。

### P1 高优先级

1. 实现 `notifyThreshold`、输出相似度、振荡和连续缺失产物检测。
2. 使 reconciliation 符合事件驱动 + 默认 5 分钟扫描 + debounce/batch/mtime 分层的规格。
3. 实现通用 validator adapter 和 ai_assisted 层，并保留 human override 审计。
4. 在控制面校验 workspace 可达性/归属/权限，以明确 4xx 拒绝无效路径。
5. 增加 server rate limit、Prometheus/OpenTelemetry 指标与 trace、结构化审计字段和告警。
6. 建立所有租户资源/动作的 IDOR 与 RBAC 测试矩阵。

### P2 产品完整性

1. 实现 `subworkflow`，或从 v1 Schema/文档中明确移除，不应长期处于“可声明但必拒绝”。
2. 完成数字人生命周期、注销交接 artifact、memory 审计和聊天审批回灌。
3. 完成三平台安装、签名/公证、自动更新、升级回滚和首启引导。
4. 建立 UI 自动化可访问性、截图回归和 J1-J10 任务成功率基线。

## 7. 验证证据

### server-ai

以下检查通过：

```text
go test -race -cover ./cmd/server-ai ./internal/...
go vet ./cmd/server-ai ./internal/...
staticcheck ./cmd/server-ai ./internal/...
golangci-lint run ./cmd/server-ai/... ./internal/...
govulncheck ./cmd/server-ai ./internal/...
```

覆盖率：

| Package | Coverage |
|---|---:|
| `cmd/server-ai` | 14.9% |
| `internal/gateway` | 85.7% |
| `internal/machine` | 80.6% |
| `internal/server` | 71.2% |
| `internal/store` | 51.8% |
| `internal/websec` | 100.0% |
| `internal/workflow` | 72.6% |

`govulncheck` 未发现当前代码或已导入包的可达漏洞。模块元数据报告 1 条依赖项信息，但当前代码没有调用受影响符号。低覆盖区域 `cmd/server-ai` 和 `internal/store` 仍是发布风险，尤其需要补充启动配置、迁移失败、事务/恢复和并发路径。

### admin-ai

以下检查通过：

```text
bun run lint             # 0 errors, 0 warnings
bun run typecheck
bun test src             # 121 pass, 0 fail; 390 assertions
bun run build
```

`bun audit` 未形成有效结果：当前 registry 的 audit endpoint 返回 HTTP 404。发布前必须在可用 registry/npm audit 或等价 SCA 平台重新执行并存档。

### runner

以下检查通过：

```text
bun run typecheck
bun test                 # 37 pass, 0 fail; 66 assertions
```

### 真实链路

本轮使用真实 NATS/JetStream、server-ai、WebSocket runner 与 admin dev server 验证：

1. 签发 Machine token，runner 连接，onboarding 进入 `connected` 并确认。
2. Machine 列表显示 online，并上报 claude/codex/mock/mastra/script/http runtimes。
3. 启动包含 trigger → mock agent → human approval 的真实 workflow。
4. Run 到达 `waiting_approval`，批准后完成。
5. SQLite 事件顺序为：

```text
RUN_STARTED
NODE_STARTED
NODE_COMPLETED
NODE_STARTED
NODE_COMPLETED
NODE_STARTED
REVIEW_REQUESTED
REVIEW_APPROVED
NODE_COMPLETED
RUN_COMPLETED
```

成功 Run ID：`run-3-c4460620`。

## 8. 发布闸门

满足以下全部条件后，才建议把结论从 NO-GO 调整为 GO：

- P0 执行边界、lease/fencing、事件原子性、heartbeat recovery 全部实现并通过故障注入。
- 生产 Auth/TLS/HTTP/WS 代理链路有部署配置、威胁模型和集成测试，不向浏览器 bundle 注入生产 secret。
- J1-J10 在 Win/macOS/Linux 干净环境逐项通过并留存记录。
- 完成 10 并发 run × 100 节点压测、8 小时 soak、进程崩溃、网络分区、磁盘满、备份恢复演练。
- server-ai 指标/trace/告警、rate limit、runbook、容量基线可用于值班与故障定位。
- admin-ai 完成真实浏览器桌面/移动/暗色/键盘/屏幕阅读器验收。
- 在可用依赖审计服务完成 admin-ai/runner SCA，所有高危项关闭或形成有期限的风险接受记录。
