# DeepSeek Harness / Cordis 插件系统吸收调研报告

> 调研日期：2026-08-18
> 上游仓库：[deepseek-ai/deepseek-harness](https://github.com/deepseek-ai/deepseek-harness)（`dsh`，官方 agent harness）
> 底层框架：[cordiverse/cordis](https://github.com/cordiverse/cordis)（vendored，`Everything is a Plugin`）
> 证据快照：commit `99f6f02fecdb7dff40c3fbc9470f5907c29f74ca`（master，2026-08-17）
> 证据范围：`docs/architecture.md`、`docs/cordis-primer.md`、`docs/development.md`、`AGENTS.md`；源码除 vendored Cordis 外未逐行审读（上游 5 天新仓，迭代极快，结论以文档所述契约为准）。
> 报告范围：Cordis 插件系统机制、dsh 能力边界与事件契约、ChaosPlus 吸收点与不吸收点、最小落地动作。

> 定位：本文是调研归档与决策笔记，不是实现规范。吸收结论如需具约束力，另行落入 `.rules/`。

## 1. 执行结论

**值得吸收的是四项机制/纪律，不是架构立场。**

dsh 与 ChaosPlus 是两种相反的极端：dsh 把包括 agent-loop、持久化、模型适配器在内的一切做成插件，连 agent 都能在运行时挂载/修改自己的插件（`self-modification`）；ChaosPlus 由 PRD P7 / §7.4 硬约束为「内核固定、模板为数据」。**不要吸收「无特权核心」的立场**——那会直接违反 P7。值得拿走的是它拆开的四块机制：

1. **Event dispatch 模式是公开契约**（observe/wrap/fan-out/run-in-order + 短路语义）。
2. **注册即效果**：register 返回 disposer，卸载逆序撤销，无特权核心需打补丁。
3. **「模型可见 ⟺ 已记录」运行时不变式**：一切进入模型请求的输入必须可从事件日志重建。
4. **Capability seam 三件套**：Service Definition / Provider / Consumer，一件不构成 seam。

不能吸收：**Service locator（`ctx.<key>` 服务仓库）**——依赖 TS 声明合并，Go 里是反模式；ChaosPlus 现有显式接口注入（§15.2 `StateStore`/`ArtifactStore`/`ExecutionBackend`/`Scheduler`）更干净。

> **「完备插件系统」的准确边界**：ChaosPlus 要的完备插件化限于**模板/节点层**——node 类型、验证器、执行器、artifact 规范、技能——这层正是 §7.4「新增领域不改内核一行」的字面要求，与 P7 不冲突。P7 禁止的只是 dsh 连内核（调度/FSM/DAG/七层上下文/有界自治）都可换的立场。**节点层全量插件化的具体吸收见 §6。**

## 2. 调研方法与可信边界

- dsh 2026-08-13 创建，5 天内 15.6 万 star / 1.6 万 fork，developer preview（官方明示会破坏性变更）。热度证明需求，不等于接口或格式已稳定。
- 认证为：`npx @deepseek-ai/dsh web` 起 Web UI（默认 :3080）；headless 一次性 runner；`--profile web --dump-config` 可导出实际装配的插件树。
- 生态：`dsh-plugin` topic、桌面客户端（Tauri/Electron 多实现）、awesome 列表。本文只依据官方仓库自身文档，生态项目不作依据。

## 3. Cordis 五条核心机制

### 3.1 上下文即服务仓库，依赖靠声明

插件是「实现了 Service 的对象」：函数带 `inject`/`apply(ctx)` 或 Service 子类。`ctx.<key>` 声明服务归属（`ctx.tools`/`ctx.llm`/`ctx.sessions`），插件按 key 找服务而非 import 具体实现。`inject` 声明服务依赖，**加载顺序由服务需求表达，不是手工 boot 排序**——这是它对「boot 顺序硬编码」的解法。

### 3.2 四种 dispatch 模式是公开契约

| 模式 | 异步 | 顺序 | 返回值 | 语义 |
|---|---|---|---|---|
| `emit` | 否 | 注册序 | 无 | 观察 |
| `waterfall` | 否 | 注册序 | 有 | 包装/替换 |
| `parallel` | 是 | 并行 | 无 | 扇出 |
| `serial` | 是 | 注册序 | 有 | 有序执行 |

模式属于事件公共契约，文档用 `@mode` 标注，由生成目录校验声明与派发点一致。

### 3.3 Waterfall 短路语义

`ctx.waterfall` 是 around-middleware：监听者收 `(...args, next)`，**必须 `next()` 委托**；不调 `next()` 即短路终结。协同监听者改共享对象后委托；单决策事件里短路即设计——策略监听者短路拍板，只注释/观察的监听者必须放行。`prepend` 仅在必须跑在普通注册之前时用。

### 3.4 注册即效果，卸载逆序撤销

一切贡献走 `ctx.effect()`/`ctx.on()`；`register()` 返回 disposer。**没有特权核心要打补丁**：扩展 = 在别的插件旁挂一个，registrations 是 effect，插件 unload 时自动反卷。teardown 顺序敏感时把相关注册放一个 effect 里，撤销按内联顺序。

### 3.5 会话日志 = 模型所见上下文的唯一来源 + 运行时不变式

- session log 是 append-only 事件流，`deriveMessages()` 从日志投影模型历史；raw `assistant/chunk` 事件保留回放与 UI 保真。
- **Model-visible ⟺ logged**：到达模型请求的一切必须可从日志重建，运行时不变式断言。新增模型可见输入 = 新增 session 事件 + 从日志渲染，没有例外。

## 4. dsh 能力边界（分层机制参考）

| 包 | 归属 | 插拔方式 |
|---|---|---|
| `core/session` | append-only SessionEvent 日志 | `ctx.sessions`，可从日志 fork/replay/重建 |
| `core/system-prompt` | prompt 段 + 工具 schema 装配 | `ctx.systemPrompt` |
| `core/tools` | 有作用域的注册表 + 守卫执行管线 | `ctx.tools` + `tools/*` 事件 |
| `core/agent` + `agent-loop` | Agent 接口 + 默认驱动（loop 本身是插件） | `ctx.agents` + `agent/*` 事件 |
| `core/scope` | 单 agent 作用域注册原语 | 库，无线程键 |
| `llm/llm` | 消息/流词汇 + 适配器缝 | `ctx.llm` |
| capability seams | fs / subprocess / shell / terminal / sandbox / lsp | Service Definition + Provider + Consumer，换 provider 后端整组搬走 |
| `skill/` | 技能提供者注册表 + 本地实现 + catalog/loader 工具 | 技能按名注入上下文 |
| `plan/` / `todo/` / `goals/` | plan 作为有状态日志 / todo 写工具 / 目标管理 | `ctx.goals`、`agent/*` 事件续跑 |
| `hooks/` | Claude Code / Codex hook 桥 + wire 协议库 | 事件接入外部 CLI |
| `bundle/` | profile 分层 patch（`cordis.patch.yml` 按 id 替换或插入行） | 配置层叠加 |

Turn 流程：step = 一次模型请求 + 其工具调用；turn = 若干 step。`agent/pre-step` 决定模型看到什么（可改写或拒绝），`agent/request`/`tools/*`/`llm/stream` 都是 waterfall 事件；输入经**单一 inbox** 进入，注入的上下文在 inbox 里等下一个消息唤醒。

## 5. ChaosPlus 吸收点

### A. Event dispatch 模式 = 契约（最值得，先落）

PRD §28 已从 pi 吸收 `beforeToolCall block / afterToolCall override` hook 契约，但**未定义短路语义**。Cordis 的 waterfall 规则（必须 `next()` 委托，短路即决策）直接可用；否则一个本应只观察的 hook 忘了 `next()`，会把整条链堵死。

- 对应现状：`policyx` condition、`authz` gate、`validation_results`、`feedback_log`（§13）都应该是门/链，但没有统一语义。
- 吸收动作：`.rules` 加一条「waterfall hook 必须 next() 委托，短路即决策」。零代码，成本一行，防止每个 hook 日后各自发明语义。

### B. 注册即效果（先决条件）

WASM claims 插件已做对（`claims.go` `Close()` 逆序回收 disposer）；进程内扩展（`authz` register、`ratex`、`secure`）无统一 disposer 约定。§7.4「新增领域不改内核一行」要做到，前提是**每个 register 返回 unregister/disposer**——没有它，模板分层与热挂载无从谈起。

- 吸收动作：给 `internal/core/extension/` 定约定：`Register(...)` 返回 `func() error` 或挂到 ctx 生命周期；卸载逆序撤销。

### C. 「模型可见 ⟺ 已记录」运行时不变式（直接修复 audit P1）

2026-08-10 audit 的 P1：节点输出 = agent 自报 `output.json`，从不 checksum/校验（`runner_executor.go:74-83`）。dsh 把这条从纪律升格为**运行时断言**。

- 吸收动作：把 §18.1 七层上下文输入（L3 产物/L4 反馈/L6 技能）纳入「可从 events+artifact 重建」，写进 World Reconstruction Test 门禁断言。P1 从一次性修复变成可回归的不变式。

### D. Capability seam 三件套（验证方向，补消费者纪律）

§15.2 `ExecutionBackend`/`StateStore`/`ArtifactStore` 已经是 seam 形态，§18 多执行器也是。dsh 增量：**消费者侧纪律**——节点/工具禁止直接 import 具体 adapter，只经接口；否则为 claude-code 写的消费者就绑死了适配器。dsh 的 fs/subprocess 共享执行域、换 provider 后端连 Bash/PTY/LSP 一起搬走，是 seam 收益的样板。

### E. 不吸收

- **「无特权核心，一切皆插件」**：违反 P7 / §7.4。内核（调度、状态机、DAG、七层上下文、有界自治）保持一等公民；只有执行器/存储/验证器/技能可插。吸收 seam 机制，不拆内核。
- **Service locator**：TS 声明合并的产物，Go 反模式。保持显式接口注入。
- **异步 parallel 事件**：ChaosPlus 是强时序的 event-sourced 系统（§15.1 事件带 seq/fencing），不需要 dsh 的无序 fan-out 层；事件顺序本身就是正确性的一部分。

## 6. 工作流 node 插件化专项吸收

ChaosPlus 的 node 生态已是插件化的既定方向（`.rules/3.ARCH.md`「Workflow 节点生态」：OCI 1.1 artifact + SemVer + JSON Schema + digest/Sigstore/SBOM + 能力受限 WASI）。dsh 两个最接近 node 的子系统——`core/tools`（注册式工具定义）与 `core/scope`（作用域分层注册）——把这个方向的缺失机制补齐了：

### 6.1 节点类型 = 注册式定义（ToolDefinition 类比）

dsh 的 `ToolDefinition` 是一个注册的「工具」：模型侧 schema + **强制 canonical 输出 JSON Schema** + `execute` + 仅宿主可见的调度元数据 + 陈列投影。注册表持有它们，loop 经它们派发调用。对 node 类型的投影：

- **声明式输入 schema**（JSON Schema，`defineTool` 同款校验/收窄）→ 现有 node 包 schema。
- **强制输出 schema**：`output.schema` 「Raw supported JSON Schema enforced against every successful canonical value」——**输出不是自报而是运行时强制**。这正是 2026-08-10 audit P1 的机制化：`outputSpec.produces` 从「声明」升为「运行时强制 JSON Schema + checksum」。dsh 连渲染/展示投影都从规范输出派生，Node 输出的 `sideEffects.json`/artifact 声明同理。
- **白名单投影**：注册表的 `schemas()` 显式 allowlist——`execute`/`timeoutMs`/`isConcurrencySafe`/presenters **永不泄漏进模型请求**。对 node：执行细节（executor 选择、重试策略、超时）永不写进节点数据契约/context JSON；节点对外只见 schema + 输出声明。
- **调度元数据走登记而非硬编码**：`timeoutMs`、是否并发安全、幂等都由定义登记，由引擎读取——替代手写每节点逻辑。

### 6.2 节点执行 = waterfall 管线（可否决，短路即决策）

dsh 工具管线是三段 waterfall 事件：`tools/pre-execute`（可短路的守卫）→ `tools/execute` → `tools/post-execute`（可改结果/驳回）。对 node 执行的生命周期：

```
node/pre-execute   — 输入校验、approval、resource 预算；可以短路（不 next() 即拒绝）
node/execute       — 派发执行器（多运行时 §18）
node/post-execute  — validation_results（§11）+ artifact checksum + feedback_log 打点（§13）；可驳回 → 重试
```

- **短路语义**：pre 是决策点（批准/预算硬停/validator veto），post 是收口点（校验失败 → 驳回重试）。这正是 §13 feedback 闭环和 audit P1 应落位的管线——**artifact 校验不是一个旁观函数，而是 post-execute 里可否决的 waterfall 节点**。
- 事件需带 `@mode`（observe/waterfall；node 调度是强时序事件源，不引入 parallel 异步 fan-out，见 §5E）。

### 6.3 作用域分层注册（ScopedLayers）——多版本 node 安装的机制

dsh 的 `ScopedLayers<L>`：一个全局默认层 + 懒创建的精确作用域 shadow 层。**读不建层**（无覆盖时零开销），注册用一个 context 同时承担可见性与 effect 归属，回收只在整层空（`ScopeLayer.isEmpty()`）时触发。

- **投影**：全局默认 node 版本 + 每 tenant/entity 的 shadow 覆盖层 = 节点包的多版本 pinning 落地机制。`.rules` 已要求「安装/升级/撤销/回滚按 tenant/entity 授权」——ScopedLayers 是这个授权的存储形态，升级/回滚 = 撤销 shadow 层，天然支持 version pin 与租户隔离。
- 同一个 `Scope` 携带 `rawDispose`（嵌套进组合 effect）与 `dispose()`（对外静默边界）双撤销路径 → 卸载逆序撤销的调用约定（对接 5B）。

### 6.4 配置 schema + load 期 fail-loud

dsh 硬规则：**misconfiguration fails loud at load（自包含时）或最早可解析点；绝不静默跳过缺失引用**。node 包 schema 在 load/install 时校验，缺失依赖引用在安装时失败，而非运行到该节点才炸。`.rules` 已要求 JSON Schema 校验;这里补的是 **load 期 fail-fast 的时机姿态**,以及「hook 永不抛 → 编号日志」的护栏。

### 6.5 节点 schema 注册进上下文装配

dsh 的系统提示是「各插件注册的 prompt 段 + 工具 schema 装配出来的」,不是适配器硬编码。对 node:**node in/out/artifact/validator spec 注册进 registry,§18.1 第 2 层(任务契约)/第 6 层(技能)由 registry 装配**——不是每个执行器各写一份摘要。

### 6.6 不吸收（node 层）

- **Service locator(`ctx.<key>`)**：Go 保持显式注册表依赖注入，不引入运行时服务仓库。
- **单次调用级并发安全(`isConcurrencySafe`)**：DAG 层已有 `join`/`parallel_fork`（§7.2）表达并行,不引入第二并发轴。
- **异步 parallel 事件**：§15.1 强时序事件源,事件顺序即正确性;dsh 的 fan-out 不适用。
- **UI 陈列投影(presentCall/presentResult)**：工作流画布已覆盖呈现,低优先。

## 7. 最小落地动作

1. `.rules` 加「waterfall hook 必须 next() 委托,短路即决策」(覆盖所有门链)。
2. `extension/` 约定 `Register` 返回 disposer、卸载逆序撤销。
3. World Reconstruction Test 加「模型可见输入可从 events+artifact 重建」断言。
4. 事件文档标注 dispatch 模式(observe/waterfall/serial;parallel 不引入)。
5. **node 输出 schema 运行时强制**(JSON Schema + checksum,不采信自报)——修 audit P1。
6. **node 执行三段 waterfall**:`node/pre-execute`(批准/预算/校验,可否决)→ `node/execute` → `node/post-execute`(validation + checksum + feedback 打点,可驳回重试)。
7. **节点注册表作用域分层**(全局默认 + tenant/entity shadow 层),升级/回滚 = 撤层——多版本 pinning 与租户隔离的存储形态。
8. **节点 schema 入 registry 装配**进 §18.1 L2/L6,非各执行器各自硬编码。

全部是规则/测试级,不碰内核。第 1、3、5、6 条可落成 `.rules` 规则条与测试断言。