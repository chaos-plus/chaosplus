# chaos.plus
## 通用自治工作流运行时 —— PRD 定稿
### Product Requirements Document — v2.0 (Final)

版本：2.0｜状态：**定稿**｜生成日期：2026-08-01

> **文档沿革**：本文档是唯一权威规格，**自包含**——所有被采纳的历史内容均已内联，不引用任何仓库外或已佚失的文档。由 `PRD6.md`（架构规格）与 `chaosplus.md`（产品功能树）合并、裁决冲突后融合而成，两份源文档为仓库内仅存的历史输入，保留不动；更早的 PRD1–PRD5 与 RFC 文档已佚失（见 §24）。所有待决项（C1–C9、R1、R4、C8）已于 2026-08-01 裁决，决议记录见附录 C。
>
> **命名（C8 决议）**：产品、引擎、品牌统一为 **chaos.plus**；代码标识符/目录/CLI 用 `chaosplus`（如 `.chaosplus/` 目录、`chaosplus` CLI、`machine-runner@chaos.plus` npm 包、`api.chaos.plus` 域名）。历史文档中的 "Myrmidon" 一律指本产品。

---

## 0. 本文档的定位

历史版本（PRD4/PRD5/RFC_260523，原文已佚失，被采纳内容均已内联于本文档）沉淀了三层诉求：一个完整可实施的**单机自治工作流运行时**（静态 DAG、Artifact 即真相、Reconciliation、有界自治、事件溯源）；从工具走向平台需预留的抽象；以及**第一版做跨平台桌面单机版，第二版做云平台化（云端调度 + 本地多执行器），且第二版不能整体重构**。

核心架构决策回应了这个诉求，并把它推到更干净的形态：

> **云端架构是唯一架构。桌面版只是把同一套云服务（控制面 API + web + runner）打包为本地嵌入式部署。一套 monorepo、一套代码，靠「部署 profile」区分。desktop = 「一个租户的云」。**

因此不存在「v1 架构 vs v2 架构」，只有一个架构与三种部署 profile（`desktop` / `self-hosted` / `cloud`）。v1/v2 是**范围与打包差异，零重写**。

早期执行核设计（六大原则、静态 DAG、Artifact 系统、Reconciliation、有界自治、事件溯源）几乎原样保留；被推翻的主要是**语言选型与运行时拓扑**（历史决策对照见 §24）。

**范围裁决（R1，2026-08-01 已决）**：v1 收敛为**软件开发场景的「可治理、可复现自治执行核」楔子版**（见 §1.4 与 §21）。这是范围裁决，不是架构裁决——内核保持领域无关（§1.2、§7.4 不变），v1 只交付软件开发模板与最小协作面；其余功能全部保留在本文档中并标注 `[v2+]`。

---

## 1. 产品定位

### 1.1 是什么

chaos.plus 是一个**通用自治工作流运行时**。用户声明「期望的世界状态」（工作流定义），chaos.plus 持续协调 AI Agent 与人类协作者，将现实推进到该状态，并持续维护一致性。

> CI/CD 问：执行到第几步了？　chaos.plus 问：世界现在是我期望的样子吗？

### 1.2 通用性（架构层）

软件开发只是内置模板之一，内核对「工作流是什么」无任何领域假设。可覆盖软件开发、小说写作、视频制作、自媒体运营、内容审核、产品运营等。

### 1.3 目标用户与形态

- **个人 / 小团队**：用 `desktop` profile，本地自托管，全功能离线可用。
- **团队自建**：`self-hosted` profile，团队共用一台服务器。
- **SaaS 用户**：`cloud` profile，云端控制面 + 本地执行器，多租户。

三者同一套代码。

### 1.4 v1 楔子范围（R1 决议）

**v1 = desktop profile 的「软件开发可治理可复现自治执行核」：**

| 进 v1 | 降 v2+（快速跟进，见 §21.2）|
|---|---|
| 执行核全量（DAG/Artifact/验证/Reconciliation/有界自治/事件溯源）| 可视化编辑器、AI 生成作者面 |
| `software-dev-agile` 模板 + 真实执行器（claude-code 起步）| 其余领域模板的打磨交付 |
| Machine Runner 接入（单机 + 局域网）与管理 UI（§5.3）| 多 runner 并发压测/故障转移、cloud profile |
| 数字人 Agent（workflow-only）+ 生命周期管理（§6.2）| 数字人 `direct/both` 策略的完整场景 |
| 自带 IM：项目 channel 聊天、@mention、审批回灌（§9）| 外部 IM 连接器、消息桥接（mqtt/ws/http）|
| JSON/YAML 直写作者面 + Schema 校验（§8）| 协作区、看板、在线文件、预览隧道、定时任务 UI |
| 最小 web UI：仪表盘（最小）、管理区、个人中心（最小）、简中 + 跟随系统主题 | 完整 i18n（繁中/英）、5 套主题、市场（§30）|

对外定位（承 §25–26）：**「跨越生产鸿沟——可治理、可复现的 agent 自治执行」**，而非「通用工作流运行时」。

### 1.5 ICP 与端到端用户旅程（R4 决议，2026-08-01）

#### 1.5.1 首个 ICP：「多项目独立开发者 / 2–5 人小团队的技术负责人」

- 已日常使用 ≥1 个编码 agent CLI（Claude Code / codex / kimi），自带 API key。
- 同时维护 2 个以上软件项目；核心痛点：**agent 一次性任务做完即忘、不敢长时间无人值守、不敢让 agent 自主合并**（缺验证与审计）。
- 重视代码不出本机，愿意本地自托管（local-first）。
- **非用不可的一句话**：唯一能让你「睡觉时 agent 干活，醒来只处理一个经过验证的审批队列」的本地工具——完成由 validator 裁决，不靠 agent 自报。

此 ICP 与 v1 楔子范围（§1.4）严格对齐：不需要多租户、外部 IM、协作区；需要的恰是执行核 + machine/数字人管理 + 聊天审批。

#### 1.5.2 端到端用户旅程（v1 验收基线）

| # | 用户动作 | 系统行为 | 规格 |
|---|---|---|---|
| J1 | 下载桌面应用（Win/macOS/Linux），首次启动 | 自动创建实例 + 内置本地 runner；UI 无感知层级（项目=实例简化呈现）| §5.3, C1 |
| J2 | 新建项目，选择本地 git 仓库路径 | 该路径成为 Workspace / 本地 ArtifactStore；自动建默认频道 `# ALL` | §5.1, §9.1 |
| J3 | （可选）接入第二台机器 | 管理区 → machines → 添加：npx 命令 + 5 分钟 token 流程 | §5.3.1 |
| J4 | 创建数字人：选所属 machine、runtime（自动检测到的 claude-code）、填系统提示词 | 数字人以 workflow-only 策略入驻 `# ALL` | §6.2, §18 |
| J5 | 配置执行器 API key | `{ $env: 'ANTHROPIC_API_KEY' }` 引用，写入 `.chaosplus.env` | §17.2 |
| J6 | 在 `# ALL` @数字人 描述需求 | 数字人以 `software-dev-agile` 模板发起 workflow run（静态 DAG）| §9.3, §23.A |
| J7 | PRD/架构审批卡片回灌聊天，点「通过」 | review.approve 走网络协议 + token，写事件日志 | §9.4 |
| J8 | 走开；期间执行器在 worktree 并行编码 | 仪表盘可见 run 状态；validator 自动验证；失败有界重试 | §11, §13 |
| J9 | 收到通知回来（重试耗尽 pause_for_human，或验收就绪）| 通知含结构化上下文；拒绝时必填结构化反馈注入下次执行 | §13 |
| J10 | 验收审批「通过」 | artifact → valid，**完成由验证裁决**；全程可从事件日志重放 | §10, §15.1 |

**Aha 时刻**：J8→J10——用户离开期间系统自主推进并收敛，回来只面对一个可信的审批队列。v1 的一切 UI/功能取舍以走通此旅程为判据。

---

## 2. 核心设计原则

### 执行核原则（硬约束）

- **P1 — 控制面是世界状态的唯一权威。** Agent 只能提议（produce artifacts）；无权宣布完成、无权直接改系统状态、无权绕过验证。
- **P2 — Artifact 是唯一真相。** 世界状态 = 磁盘/对象存储上的 artifact 集合 + 验证结果。Memory、Summary、Agent 的话都不是真相。
- **P3 — 验证决定完成，Human 是一等公民 Validator。** 三层验证：自动化（编译/测试/lint，高可信）、AI 辅助（参考）、人工（最终权威）。Human 决定覆盖一切自动结论。
- **P4 — 持续 Reconciliation。** 上游 artifact 变化 → 下游标 `stale`、相关节点暂停。**`stale` 只传播标记，绝不自动触发重执行。**
- **P5 — 执行器 Worker 是无状态瞬时认知单元。** 每次注入最小上下文，结束即销毁。（**仅约束执行器 Agent；数字人 Agent 是另一类，见 §6.2，允许持久记忆。**）
- **P6 — 有界自治，失败必须收敛。** 有界重试 → 耗尽后 `pause_for_human`（非 abort）；结构化反馈注入下次上下文；相似度检测防原地打转。

### 平台原则

- **P8 — 一套代码，多部署 profile。** 任何功能不得硬依赖某一 profile 专有的服务；profile 差异只体现在「换实现」（StateStore/ArtifactStore/Auth 等接口的不同实现）。
- **P9 — 控制面与执行面分离，执行器永远在本地。** 控制面（API/引擎/调度）可部署在桌面进程或云端；执行器（runner）永远跑在用户本地机器。v1→v2 只是控制面搬家。
- **P10 — 多租户感知从第一天起。** 数据模型恒带租户/项目维度；隔离强度按 profile（desktop 单租户，cloud 强 RBAC）。

### P7 — 非目标与反模式（明确不做，硬约束）

| 反模式（禁止）| 为什么 | 正确做法 |
|--------------|--------|---------|
| **LLM 决定流程走向** | 不可控、不可复现、无法审计 | 工作流结构启动前静态定义（聊天/AI 只在**作者期**生成 JSON，运行期纯静态）|
| **Agent 自报"任务完成"** | Agent 的话不是真相 | 完成由 Validator 裁决，Artifact 是唯一真相 |
| **依赖长 session / 长 memory（执行器）** | 认知漂移、上下文腐化 | 执行器 Worker 无状态、最小上下文 |
| **stale 触发自动重执行** | 级联重跑、烧光预算 | stale 只传播标记，由调度/人工决策 |
| **无限重试直到成功** | 原地打转 | 有界重试 + 相似度检测 + 升级人工 |
| **可视化即真相**（n8n 模式）| 双向同步地狱 | **JSON 为唯一真相，画布是投影**（C7 决议同此）|
| **内核出现领域词汇**（coder/端口/DOM）| 破坏通用定位 | 领域内容只进模板 |
| **业务代码直接碰 DB/FS/本地 spawn** | 锁死单机，无法平台化 | 一律走 StateStore / ArtifactStore / ExecutionBackend / Scheduler 抽象 |
| **功能硬依赖某 profile 专有服务** | 破坏一套代码 | 走接口抽象，按 profile 换实现 |

---

## 3. 架构总览

```
┌──────────────── CONTROL PLANE（唯一状态权威，网络化）────────────────┐
│  部署：desktop=桌面进程内嵌 / self-hosted=团队服务器 / cloud=云集群    │
│   • RuntimeKernel        StateStore（事件日志 + 投影）                  │
│   • WorkflowEngine       静态 DAG 调度 / condition / join / 状态机      │
│   • Scheduler            per-run 租约 + fencing token（第一天就生效）   │
│   • ReconciliationLoop / ValidatorBus                                   │
│   • ConversationHub      事件溯源会话（channels）                       │
│   • Digital-human Agents 长期成员（数字人）                             │
│   • API Server           gRPC / WebSocket + REST 网关 + AuthProvider    │
└──────▲────────────────────────────────────▲───────────────────────────┘
       │ 网络协议（客户端/IM）                │ 网络协议（控制面 ↔ runner）
       │                                      │ localhost | 局域网 | 云
┌──────┴───────────┐            ┌─────────────┴──────────────────────────┐
│ Human 成员        │            │ Machine Runner（永远 LOCAL，1..N 跨机） │
│ 自带 IM(多channel)│            │  • ExecutionBackend  本地 spawn 执行器  │
│ 外部 IM(绑1channel)│           │  • 托管 执行器 Agent INSTANCE（瞬时）    │
│      [v2+]        │            │  • ArtifactStore     本地 FS / S3        │
└──────────────────┘            │  • worktree / 进程·端口清理·隧道[v2+]    │
                                └──────────────────────────────────────────┘

DSL/SDK：TS npm 包（可选，产出 WorkflowDef JSON）   引擎只认 JSON
前端 UI：web(Next.js + ShadCN + tailwindcss) → Tauri/Electron 壳，
        走网络协议连控制面（瘦客户端）（C4 决议）
```

**不变量：**
- 执行器永远在本地 runner（所有 profile）。控制面相对位置可变。
- 控制面 = 唯一状态权威（P1），但**网络化**，不是单进程；一切变更走网络协议 + token。
- 执行恒为静态 DAG（执行核不变量）；聊天/可视化/AI 都是**作者期**封装，收敛到同一份 WorkflowDef JSON。

---

## 4. 语言与技术栈 ADR（含 C2/C3/C4/C9 决议）

**决策：引擎（控制面 + runner）+ CLI 用 Go；编排放弃 TS 代码 DSL，改为 JSON/YAML + 可视化[v2+] + AI 生成[v2+]；前端 UI 为 Next.js + ShadCN + tailwindcss。**

| 因素 | 判断 |
|------|------|
| 难点已变为分布式协调正确性 + 跨机部署 | Go 主场；Temporal/Argo/Cadence 同类选择 |
| 跨机 runner 部署 | Go 静态二进制，桌面壳可直接内嵌 |
| 并发（多 run、租约、fencing）| goroutine 模型稳健 |
| WorkflowDef 本就是 JSON | 引擎读 JSON，不必内嵌 TS 求值器；多语言/多运行时天然兼容 |
| 富可视化画布 + 聊天 UI | 浏览器即 JS，前端必为 web；作为网络协议上的瘦客户端 |

**编排作者面三条路（都直出 WorkflowDef JSON，JSON 即唯一真相）：**
1. JSON/YAML 直写 + JSON Schema 校验（编辑器 `$schema` 联想）——**v1**
2. 可视化编辑器（拖拽 → 结构化 patch 回写 JSON）——[v2+]
3. AI 生成（聊天描述 → 产出 JSON → 人审 → 落库）——[v2+]

**契约源单一化：** WorkflowDef schema、网络协议类型，统一用 protobuf / JSON Schema 定义，**codegen 出 Go 结构体 + TS 类型**，两端类型安全。

**API 协议分层（C2 决议）：** 内部（控制面↔runner、控制面↔客户端实时流）走 gRPC / WebSocket；对外开放 API 经 **grpc-gateway 暴露 RESTful**，列表查询支持 **RSQL** 语法糖。契约仍单源于 `/schema`，不维护两套定义。

**消息传输（C3 决议）：** ConversationHub 的事件日志是唯一真相（P1/P2）。**NATS 仅作为 cloud / self-hosted profile 的传输/扇出层实现**（WebSocket/TCP）；desktop profile 进程内直连，不引入 NATS 硬依赖（P8）。

**DB 工具链（C9 决议）：** SQLite-first（§16），迁移用内置 `PRAGMA user_version` 顺序迁移函数、零外部框架；**sqlc** 生成类型安全查询代码（SQLite/Postgres 双方言）；**goose 仅在 cloud/Postgres profile 引入**。

**扩展逃生口：** `transform` / 自定义逻辑先用声明式（JSON Logic）；真需要可编程时，挂 **WASM（wazero）或 Lua（gopher-lua）沙箱插件**——比内嵌 JS 更干净、可沙箱、多语言。

> 放弃 TS 代码 DSL 的代价仅两条：作者期类型检查从编译期降到 schema 校验（够用）；`transform` 内联函数降级为声明式（反更合 P7 禁 eval）。且可逆——日后可加「只产 JSON 的 SDK」，引擎零依赖。

---

## 5. 实例 / 项目 / 工作空间 / 成员 / Runner 模型

### 5.1 层级（含 C1 决议）

```
Account → Instance(Org) → Project → Workspace
```

- **Instance（组织/实例）**：账号下的隔离单元。desktop 下通常单实例；cloud 下多租户。
- **Project**：实例下的项目，指定一个 **Workspace**。
- **Workspace = 本地路径 或 S3 路径**。决定该项目 ArtifactStore 的后端实现：本地路径→本地 FS；`s3://`→S3。内核只调 `put/get/stat/exists`。

**C1 决议**：数据层保持一实例多项目；**desktop 的 UI 默认呈现「一个项目一个实例」的简化形态**（chaosplus 产品设计），即新建项目时自动创建/绑定实例，普通用户无感知层级；数据模型不锁死，cloud 可解开。

### 5.2 成员（人 + AI，统一成员表）

实例成员 = **Human 成员** ∪ **数字人 Agent 成员**。`@mention` 目标只能是成员，**永远不是执行器 Agent**。

### 5.3 Machine Runner（对应产品概念「电脑机器 machines」）

- 实例可注册 **1..N 个 machine runner**（本机 / 局域网 / 远程）。每个 runner 向控制面注册（网络地址、状态、心跳）。
- desktop profile 默认内置 1 个本地 runner，可再加局域网 runner。
- runner 托管「执行器 Agent 实例」，并持有本地 ArtifactStore。
- 一台 machine 可托管多个数字人 Agent，每个数字人绑定一个运行时（claude code / codex / kimi 等，见 §18）与大模型配置。

#### 5.3.1 Machine 接入流程（产品规格，v1）

**添加流程：**

1. **命令区**展示一次性接入命令：
   ```
   npx machine-runner@chaos.plus --server api.chaos.plus --token xxxx.xxxx.xxxxxxxxx #machinename
   ```
   （desktop profile 下 server 为 localhost 地址；命令模板随 profile 生成。）
2. **连接状态实时监控**：
   - 命令/token **5 分钟有效**，UI 倒计时读秒；超时后提示命令失效，出现「刷新命令」按钮；刷新后重新计时；失效期间「确认」按钮不可点。
   - runner 已连接但倒计时结束仍未点击「确认」→ **自动断开连接**，需重新刷新命令。
3. **名称**：未连接前不可输入；连接后自动提取 hostname，可手动修改。
4. **异常**（断网、页面刷新等未走取消/确认的）：一律走超时取消路径。
5. **取消**：关闭连接 + token 立即失效。
6. **确认**：未连接时按钮不可点；点击后完成创建，**该 machine 的命令与 token 转为长期有效**（用于 runner 重连）。

**列表与详情：**

- 未经确认的临时 machine 不出现在列表。
- 列表项 + 过滤筛选。
- 详情 Tab：**关键信息** / **运行时**（该机可用执行器 runtime 检测结果，§18 `meta` 表）/ **agent 列表**（该机托管的数字人：基本信息、实时状态、操作按钮；搜索过滤）。

---

## 6. Agent 模型（两类，严格区分）

### 6.1 执行器 Agent（Executor）——「临时工」

- `spec → instance`：从 spec 创建，跑完一个工作流节点即销毁。**无状态**（P5）。跑在 runner 上，由 ExecutionBackend spawn 外部执行器 CLI。
- **不可被 @mention**。

#### 6.1.1 ExecutorAgentSpec —— 硬约束契约（核心）

每个执行器 Agent 由一份 **硬约束 spec** 定义；spec 可由人手写、模板提供、或 **AI 生成**，但**一经设定即由运行时强制执行**（代码层 + Constitution 层双重）。这是「每个 Agent 节点有固定输入/执行/产出/验证」原则的落地形态。

```typescript
interface ExecutorAgentSpec {
  id: string;
  role: string;                       // 角色名（领域含义由模板赋予，内核不预设）

  // —— 执行器绑定 ——
  executor: string;                   // 引用 executors（runtime × model，见 §7.6/§18 与附录 F.6）
  tokenProfile: 'budget'|'balanced'|'quality';
  maxContextTokens: number;

  // —— 能力硬边界（代码层强制）——
  systemPrompt: string;               // 注入为 CLAUDE.md / 角色宪法
  allowedTools: string[];
  forbiddenActions: string[];         // 明确禁止（如 git push --force、DROP TABLE）
  allowedMCPTools: string[];          // MCP 工具白名单
  requiredSkills: string[];           // 必须注入的 Skill

  // —— 输入规范（in spec）——
  inputSpec: {
    consumes: { id: string; type: string; required: boolean }[];
    inputValidator?: ValidatorRef;    // 节点开始前校验 consumes 状态
  };

  // —— 输出规范（out spec）——
  outputSpec: {
    produces: { id: string; path: string; type: ArtifactType; required: boolean }[];
    outputValidator?: ValidatorRef;   // 节点完成后校验 produces
  };

  // —— 产物规范（artifact spec）——
  // 每种产物的领域格式约束 + 验证器路由（如 DOM Contract、API schema）
  artifactSpecs?: Record<string, ArtifactSpec>;

  // —— 验证规范（validator spec）——
  // 该节点产物适用的验证器集合及其层级（automated / ai_assisted / human）
  validatorSpecs?: ValidatorSpec[];

  // —— 门控与重试 ——
  humanApproval?: HumanApprovalSpec;
  retry?: RetrySpec;
  hooks?: { pre?: HookRef; post?: HookRef; onError?: HookRef };
}
```

> **AI 生成硬约束**：允许「描述角色 → AI 产出一份 ExecutorAgentSpec（含 in/out/artifact/validator spec）」，但产出物是**待人审的硬约束 JSON**，审批后落库即成为运行时强制的契约——AI 参与的是作者期，不是运行期决策（不破 P7）。

### 6.2 数字人 Agent（Digital-human）——长期成员（对应产品概念「智能体 agents」）

- **长期活跃**，作为实例成员与 Human 并列；有持久身份 + **持久 memory**（P5 不约束此类，memory 子系统见 §6.3）。
- **可被 @mention**。`@mention 目标 = {human} ∪ {数字人}`。
- **动作策略（per-agent 配置）：**
  - `workflow-only`（默认推荐，v1 唯一开放）：只能通过编排/触发工作流产生真实变更 → 全程过 validator → 保 P2/P3。
  - `direct` [v2+]：可不开工作流直接动作（答疑、读、轻量操作、定时任务）。**显式 opt-in，且这些动作不享 artifact/验证保证**——刻意的取舍，用于 assistant/coordinator/relay 角色。
  - `both` [v2+]：按交互自行决定。
- **协调链**：`human @assistant → @orchestrator → workflow`。数字人可 @ 另一数字人或触发工作流。
- **托管（X-2 裁决）**：数字人进程由其绑定的 machine runner 托管（`runnerId` 必填）；控制面只做路由与调度，不托管数字人进程。desktop 默认绑定内置本地 runner（效果等同本机进程）。真实变更仍由 runner spawn 的执行器完成。

```typescript
interface DigitalHumanAgentSpec {
  id: string;
  name: string;
  description: string;                // 产品字段：描述
  systemPrompt: string;               // 产品字段：系统提示词
  runnerId: string;                   // 所属 machine（UI 可点击跳转）
  executor: string;                   // runtime × model（引用 executors 表，附录 F.6）——数字人自身对话/编排所用
  memory: MemorySpec;                 // 持久记忆子系统（§6.3）
  allowedMCPTools: string[];
  skills: string[];
  actionPolicy: 'workflow-only'|'direct'|'both';   // 默认 workflow-only
  authorizedWorkflows?: string[];     // 可触发/编排的工作流白名单
  channels: string[];                 // 默认参与的 channel
}
```

#### 6.2.1 生命周期管理（产品规格，v1）

**操作**：启动 / 停止 / 重启。

**正常注销**（machine 健康时，6 步）：
1. 检查工作状态，**生成交接文档并上传**（作为 artifact 入库，可审计）。
2. 确认是否需要交接；如需要则选择接手人（human 或另一数字人），或稍后再选。
3. machine 先清理该 agent 的相关文件（非项目文件）。
4. 服务端使连接 token 失效。
5. 服务端标记 agent 已注销。
6. machine 端销毁 agent 进程，关闭连接。

**强制注销**（machine 异常时）：
- UI 红色强提示：会丢失工作状态、操作无法撤销等。
- 使连接 token 失效。
- 服务端断开连接，标记 agent 已注销。

> 交接文档机制是本产品独有设计：数字人「离职」时组织记忆不丢失。交接文档为标准 artifact，走 §10 生命周期。

#### 6.2.2 数字人管理 UI（产品规格，v1）

- **添加**、**列表**（列表项：关键信息 / 实时状态 / 操作按钮；过滤筛选）。
- **详情 Tab**：
  - **基本信息**：名称、描述、系统提示词、所属 machine（点击跳转）、操作（启动/停止/重启）、注销（§6.2.1）。
  - **DMS**（智能体间私信）：数字人之间的对话列表 + 详细聊天记录（本质是特殊 channel 的投影，走 §9 同一事件日志）。
  - **技能列表**：添加 / 列表项（挂载 SKILL.md，含「从 run 提炼」入口，见 §6.4）。
  - **工作目录**：文件 / 代码（该数字人 workspace 的只读浏览）。

### 6.3 数字人记忆子系统（吸收 2026 记忆研究结论，v1 最小版）

`memory` 展开为完整 spec，覆盖记忆五操作（存储/检索/更新/压缩/遗忘）：

```typescript
interface MemorySpec {
  store: string;                      // 持久 workspace memory 引用（文件/DB）
  retrievalPolicy: 'recent'|'semantic'|'hybrid';
  updatePolicy: { requiresApproval: boolean };   // 高影响记忆写入需人审
  compressionPolicy?: { intervalDays: number };  // 定期蒸馏 [v2+]
  forgettingPolicy?: { ttlDays?: number; maxItems?: number };  // [v2+]
}
```

- **记忆写入过治理**：数字人 memory 的每次写入记入事件日志（append-only 审计轨迹）；「以后都这样做」类高影响规则记忆可配置为需人审——复用 §11 validator 体系。**防记忆投毒**（2026 研究：90%+ agent 中招、对话内纠正 100% 复发——写入必须走治理，不靠对话自愈）。
- **记忆永远不是真相**（P2 不变）：memory 只影响数字人行为倾向，真实变更仍走 workflow + validator。差异化卖点：**可审计记忆**。
- v1 交付 store/retrieve + 审计日志；压缩/遗忘/人审策略 [v2+]。

### 6.4 技能沉淀闭环（借鉴 Multica，[v2+]，接口 v1 预留）

**`成功 run → AI 从事件日志/artifact 提炼 SKILL.md 草稿 → 人审 → 入技能库 → 挂载到任意 agent spec`**

- 与 §6.1.1「AI 生成 spec → 人审 → 落库」同构：AI 参与作者期，运行期静态挂载（不破 P7）。
- 数据模型：`skills` 表（§16），`source_run_id` 提供出处审计。
- UI：技能列表含「从 run 提炼」入口。
- 团队技能随成功执行复利累积，同时是市场（§30）的供给端流水线。

---

## 7. 工作流系统

### 7.1 WorkflowDef（JSON，唯一真相）

工作流 = `{ id, version, name, nodes[], edges[] }`，以 JSON 持久化。作者面三条路均收敛到此。运行期使用 `workflow_def_snapshot`（§16），定义变更不影响在飞 run。

### 7.2 节点类型

| type | 说明 | 关键字段 |
|------|------|---------|
| `agent` | 派发执行器 Agent，每次新 session | ExecutorAgentSpec（§6.1.1）|
| `human_approval` | 人工验证门控 | `timeoutMs`, `onTimeout`, `onReject` |
| `condition` | 按表达式选择出边 | `expr`（JSON Logic）|
| `parallel_fork` | 并行启动多分支（支持数据驱动 fan-out）| 出边隐式定义 |
| `join` | 等待所有并行分支（AND 语义）| 入边隐式定义 |
| `transform` | 声明式产物转换（JSON Logic；复杂逻辑升级为 agent/WASM 节点）| `transform.expr` |
| `trigger` | 入口（手动/定时/Webhook/Connector）| `trigger.source` |
| `loop` | 循环子图直到 condition | `loop.maxIterations` |
| `subworkflow` | 一个节点引用另一 WorkflowDef（预留）| `ref`, `inputMapping`, `outputMapping` |

> 调度器从一开始就支持节点是子图（不假设扁平）。
>
> 产品概念「定时任务」（任务名称 / 执行者=人或 agent / 定时提醒）映射：执行者为 agent → `trigger` 定时触发工作流；执行者为人 → 定时提醒通知（轻量通知功能，[v2+] UI）。

### 7.3 调度器 Formal Rules

**Node Readiness**：
```
ready(N) = 所有入边条件成立 AND 所有 consumes artifacts = valid
           AND N.status == 'pending' AND N 未被有界自治暂停
```

**Edge Condition（预定义枚举，禁 eval）**：`success` / `failed` / `approved` / `rejected` / `always`。`condition` 节点额外用沙箱化 **JSON Logic** `expr`，变量仅来自 `workflow_run.context_json`。

**Join 语义**：等所有上游并行分支到 terminal（completed | skipped | failed 且重试已耗尽，见附录 F.3），AND 语义，无 OR。分支最终失败 → join 进 `failed`，触发其重试策略。

### 7.4 内核 vs 模板分离（硬约束）

| 归内核（通用）| 归模板（领域）|
|---|---|
| DAG 调度、状态机、condition/join | 具体角色（pm/writer/moderator）|
| Artifact 生命周期、stale 传播、Reconciliation | artifact 领域规范（DOM Contract / 分镜格式）|
| 七层上下文管理、Worker 生命周期 | 角色代码/写作/审核规约 |
| 有界自治、重试、相似度 | 领域专属验证器（Playwright/字幕对齐）|
| 三层配置机制（role→executor→binding）| 角色定义具体内容 |

新增领域 = 写一套新模板（角色 + 产物规范 + 规约），**不改内核一行**。

### 7.5 内置模板

v1 交付：`software-dev-agile`。已定义待打磨 [v2+]：`software-dev-waterfall`、`content-creation`、`novel-writing`、`video-production`、`content-moderation`。详见 §23。

### 7.6 三层配置

`agentRoles`（能力边界，模板填充）→ `executors`（runtime×model）→ `agents`（工作流绑定）。换模型不动角色、改角色不动工作流、调工作流不动模型。

---

## 8. 编排作者面

- **JSON/YAML 直写** + JSON Schema 校验 —— **v1**。
- **可视化编辑器** [v2+]：拖拽节点/连线/配参，产出结构化 patch 回写 WorkflowDef JSON。无法可视化表达的高级逻辑（数据驱动 fan-out、WASM/Lua 节点）显示为「代码节点」占位，不破坏回写。
- **AI 生成** [v2+]：聊天描述 → 产出 WorkflowDef JSON / ExecutorAgentSpec → 人审 → 落库。

**铁律（C7 决议同此）**：JSON 为唯一真相，画布为投影；作者期可动态生成，运行期纯静态（保可复现/可审计）。**不采用 n8n 双向同步模式。**

---

## 9. 对话与 IM 协作

### 9.1 会话 = 事件溯源日志，Channel = 同步单元

- **Channel** = 会话单元 + 同步单元，每个 channel 有自己的事件溯源日志（按 `seq` 定序、`idempotency_key` 去重）。**（X-1 裁决）channel 消息以 `CHANNEL_MESSAGE_POSTED` 事件写入统一 `events` 表；`channel_messages` 是投影表，World Reconstruction 时删除重建。全系统唯一真相只有一个：`events` 表（§15.1）。**
- **自带 IM（应用 web/桌面 UI）= 多 channel 客户端**（类 Slack 侧栏）—— **v1**。
- **外部 IM（微信/Telegram/Slack/...）= 绑定到指定的一个 channel**（单 channel 窗口，刻意简单）—— [v2+]。
- **多个外部 IM 绑同一 channel → 内容完全同步一致**（皆为同一日志的投影）。
- 同步边界 = channel：扇出 = 在一个 channel 内投递给「所有绑到该 channel 的外部 IM + 正在看该 channel 的自带 IM 客户端」。无跨异构渠道 N-to-M 同步。
- 逐渠道已读回执尽力而为，完整 read-receipt 推后。

**Channel 作用域（默认）**：每个 Project 一个 channel（项目 room，产品呈现为默认频道 **`# ALL`**），可再自建频道。

### 9.2 频道规格（产品设计）

自建频道包含：
- **频道名称**
- **成员管理**（频道级成员：human + 数字人）
- **消息钩子**：频道消息可触发钩子 → 映射为工作流 `trigger.source=Webhook/Connector`
- **消息桥接** [v2+]（C3 决议下的 transport 扩展）：
  - `mqtt`
  - `websocket`
  - `http`：push → 外部（向外部系统推送）；hook ← 外部（接收外部回调）
  - 对应 `channel_bindings.transport` 枚举：`'builtin'|'slack'|'wecom'|'telegram'|'mqtt'|'websocket'|'http'|...`
- **聊天区**

### 9.3 @mention 路由（ConversationHub）

- `@human` → 跨该成员所属 channel 通知。
- `@数字人` → 派发给该 agent，按动作策略处理（回话 / 触发工作流 / 再 @ 其他成员）。
- @mention 是触发工作流的入口；「自动建任务」为 [v2+]（随 Task/看板引入）。

### 9.4 工作流 ↔ 会话统一（关键）

- 数字人编排/触发 WorkflowDef → 真活落静态 DAG。
- 工作流的人工介入点（`human_approval`：架构决策、验收、合并）**作为消息回灌对应 channel** 并扇出通知。
- **聊天里点「通过/拒绝」= 调 review.approve/reject（网络协议 + token）。** Review Queue 与聊天合一——人不管开哪个渠道都能直接 work。

### 9.5 线程粒度

v1 线程粒度为 `Instance → Project → Channel → (0..N) workflow run`；**Task 实体为 [v2+]**（随看板引入，届时层级为 `Channel → Task → run`）。频道内人类消息 + 数字人消息 + 工作流生命周期事件交织在同一 channel 日志，过程可视化天然成立、可重放。

---

## 10. Artifact 系统

### 10.1 类型（6 core + 自定义）

`document` / `source_code` / `test_output` / `screenshot` / `build_artifact` / `external_state`。类型决定默认匹配的 Validator，其余行为一致。自定义类型匹配不到内置 Validator 时仅做文件存在性验证。

### 10.2 生命周期

```
pending → generating → needs_validation
   → valid / invalid / needs_review（含 force_valid 人工覆盖）
   → stale（上游变化）/ orphaned（关联 execution 消失）
```

### 10.3 粒度与 Stale 传播

细粒度（一个组件/接口/报告一个 artifact）。上游 checksum 变 → 批量标下游 `stale`（单事务，最大递归深度 10）。**不触发自动重执行。**

### 10.4 可从 Artifact 重建

删除所有 session/memory/对话后，系统仍能从「artifact 内容 + checksum」「事件日志」「WorkflowDef JSON」完整重建。

---

## 11. 验证系统

- **工程验证（全自动）**：tsc、单测/集成、ESLint/Biome、构建、SQL 迁移、API schema。外部 CI（如 GitHub Actions）检查结果亦可作为自动化 Validator 回灌（§29.2，[v2+]）。
- **UI 多层**：Structural / Design Token / Responsive / Interaction（自动）+ **Semantic（仅人工）**。
- **治理验证**：ADR 合规、工具使用合规、worktree 访问边界。
- **Validator 优先级**：**Human 决定覆盖一切自动结论。** Human override 记 `validation_results`（`force_valid`、`overrides_validator_ids`）。Human 不可 force_valid 缺失/orphaned 的 artifact。`ARTIFACT_FORCE_VALIDATED` 写事件日志，不可删审计。
- **诚实性**：约 70% 自主完成，约 30% 需人工（UI 语义、架构决策、需求澄清）。目标是让这 30% 高效有据。

---

## 12. Reconciliation 引擎

- **两层对账**：事件驱动（写入即 checksum 比较）+ 周期扫描（默认 5 分钟全量预检）。
- **Storm 防护**：Debounce（500ms 合并）+ Batch 传播（单事务）+ 深度限制（10 层）。
- **检测**：artifact 缺失（→invalid）、内容变化（→stale）、Phantom Running（无心跳>阈值→failed）、孤立 worktree（清理归档）、审核超时（按 onTimeout）。
- **Checksum 分层**（下沉为 ArtifactStore 实现细节）：事件驱动 → mtime+size 预检 → SHA-256（>10MB 仅 mtime+size）；S3 后端用 ETag/版本号。

---

## 13. 有界自治模型

- **重试**：`maxAttempts` + `backoffSeconds[]`，耗尽 → `pause_for_human`。
- **三档升级**：`auto_retry`（<notifyThreshold）/ `notify_and_wait`（≥notifyThreshold，发 IM 不阻塞）/ `pause_for_human`（≥maxAttempts 或相似度触发）。
- **结构化反馈注入**：人类拒绝必填 `{category, location?, expected?, detail}`，与 artifact 绑定，注入下次执行上下文。
- **相似度检测（防原地打转）**：文件级 SHA-256 集合比对，变化文件比例 < 阈值（默认 0.08）→ `pause_for_human`。
- **调度层 Stuck 检测**：振荡（A→B→A→B，窗口 4）、依赖产物持续缺失（连续 2 次）→ `pause_for_human`。

---

## 14. Human Governance

人类是**校准者**：负责产品方向、ADR、UI 语义验收、高风险仲裁；不负责重复执行。`human_approval` 节点覆盖 PRD 确认、ADR、UI 验收、Sprint 交付、高风险仲裁。ADR 一经批准为不可变治理产物，后续节点不得绕过。

---

## 15. 可靠性模型与平台抽象

### 15.1 事件溯源

- 所有行为记为 append-only JSONL 事件（`events` 表，`seq` 单调递增排序）。事件日志是唯一真相，投影表可重建。
- **Event Idempotency**：run 域事件 `idempotency_key = {run_id}:{type}:{entity}:{attempt}`；平台域事件（`run_id` 为空，如 machine 接入、数字人注销、memory 写入、channel 消息）`= {instance_id}:{type}:{entity_id}:{client_request_id}`（`client_request_id` 由客户端生成，见附录 F.1 RPC 幂等）。均 `INSERT OR IGNORE`。
- **快照 + 增量重放**：每节点完成写 snapshot；启动加载最新 snapshot + 重放后续事件。
- **崩溃恢复**：重启重建状态；`running` 节点查心跳（无心跳超阈值→failed→重试）。**冲突裁决：事件日志永远是 Source of Truth，投影表损坏则清空重建。**
- **World Reconstruction Test**：删 session+snapshot+投影表，仅留事件日志 + artifact，重启后状态须一致。**发布闸门，必跑。**

**时序模型（非单进程假设）：**

> **per-run 单写者 + fencing token，从第一天起生效。** 同一 run 的事件只由当前持有该 run 租约的控制面实例写；不同 run 可并行。集群并发度 = 并发 run 数。故障转移：持租约实例崩 → 租约超时 → 别的实例接管，从事件日志 replay。fencing token（单调递增）防脑裂——旧实例过期租约的写入被 StateStore 拒绝。

### 15.2 平台抽象（v1 全部动真格）

因 v1 即「网络化控制面 + 跨机 runner」，四抽象不是 no-op，而是真实现：

```typescript
interface StateStore {            // v1 SQLite / cloud Postgres
  appendEvent(e: Event, lease?: Lease): Promise<void>;  // run 域事件必须携带有效租约，StateStore 做 fencing 校验（附录 F.6）
  readEvents(runId, since?): AsyncIterable<Event>;
  projection<T>(table, query): Promise<T[]>;
  transaction<T>(fn): Promise<T>;
}
interface ArtifactStore {         // v1 本地FS / S3
  put(id, content): Promise<Checksum>;
  get(id): Promise<Readable>;
  stat(id): Promise<{mtime; size; sha256?}>;
  exists(id): Promise<boolean>;
}
interface ExecutionBackend {      // 网络 spawn 到 runner
  spawn(opts): Promise<WorkerHandle>;
  heartbeat(handle): Promise<HeartbeatStatus>;
  kill(handle, signal): Promise<void>;
}
interface Scheduler {             // per-run 租约 + fencing
  claim(runId): Promise<Lease|null>;
  renew(lease): Promise<void>;
  release(lease): Promise<void>;  // Lease 含单调递增 fencingToken
}
```

| 抽象 | desktop | self-hosted | cloud |
|---|---|---|---|
| StateStore | SQLite | SQLite/Postgres | Postgres |
| ArtifactStore | 本地FS / S3 | 本地/NFS/S3 | S3 |
| ExecutionBackend | 本机 + 局域网 runner | 本地池 | 用户本地 runner |
| Scheduler | 单实例租约+fencing | 单实例 | 分布式租约（Postgres advisory lock / etcd）|
| 消息扇出（C3）| 进程内直连 | 进程内 / NATS | NATS（WebSocket/TCP）|

> DB 选型决策：**先 SQLite 一把梭**（desktop + 小规模 cloud），真到规模再加 Postgres 实现（接口隔离，换实现不改业务）。

### 15.3 长期稳定性保证

不应发生：世界状态漂移、幻觉完成、上下文腐化、worktree 泄漏、无限循环。

---

## 16. 数据模型

> 所有持久化结构带 `schema_version`；启动按 `PRAGMA user_version` 顺序执行迁移函数，每个迁移原子事务，零外部框架（C9 决议：sqlc 生成查询代码；goose 仅 cloud/Postgres profile）。**多租户列从第一天起存在**（desktop 单值，cloud 隔离）。

```sql
-- 平台层
CREATE TABLE instances ( id TEXT PRIMARY KEY, account_id TEXT, name TEXT, created_at TEXT );
CREATE TABLE projects  ( id TEXT PRIMARY KEY, instance_id TEXT, name TEXT,
                         workspace_uri TEXT, created_at TEXT );      -- 本地路径 或 s3://
CREATE TABLE members   ( id TEXT PRIMARY KEY, instance_id TEXT,
                         kind TEXT,            -- 'human' | 'digital_human'
                         display_name TEXT, spec_id TEXT );  -- 数字人指向 agent_specs（spec 唯一权威存放处）
CREATE TABLE machine_runners ( id TEXT PRIMARY KEY, instance_id TEXT,
                         address TEXT, status TEXT, last_heartbeat_at TEXT );
CREATE TABLE channels  ( id TEXT PRIMARY KEY, project_id TEXT, name TEXT, created_at TEXT );
CREATE TABLE channel_bindings ( id TEXT PRIMARY KEY, channel_id TEXT, member_id TEXT,
                         transport TEXT,       -- 'builtin'|'slack'|'wecom'|'telegram'|'mqtt'|'websocket'|'http'|...
                         external_addr TEXT );
-- 投影表：源于 events 的 CHANNEL_MESSAGE_POSTED，可删除重建（X-1 裁决）
CREATE TABLE channel_messages ( seq INTEGER PRIMARY KEY AUTOINCREMENT,
                         id TEXT UNIQUE, channel_id TEXT, ts INTEGER,
                         author_member_id TEXT, author_kind TEXT,    -- human|digital_human|workflow
                         idempotency_key TEXT UNIQUE, payload_json TEXT );
CREATE TABLE agent_specs ( id TEXT PRIMARY KEY, instance_id TEXT,
                         kind TEXT,            -- 'executor' | 'digital_human'
                         spec_json TEXT );     -- ExecutorAgentSpec / DigitalHumanAgentSpec
CREATE TABLE skills    ( id TEXT PRIMARY KEY, instance_id TEXT, name TEXT,
                         frontmatter_json TEXT, body TEXT,
                         source_run_id TEXT,   -- 技能提炼出处（§6.4），可空
                         approved_by TEXT, version TEXT, created_at TEXT );

-- 执行核（恒带 instance_id/project_id 维度）
CREATE TABLE workflows ( id TEXT PRIMARY KEY, instance_id TEXT, project_id TEXT,
                         version TEXT, name TEXT, schema_version INTEGER DEFAULT 1,
                         def_json TEXT NOT NULL, created_at TEXT, updated_at TEXT );
CREATE TABLE workflow_runs ( id TEXT PRIMARY KEY, project_id TEXT, workflow_id TEXT,
                         workflow_version TEXT, workflow_def_snapshot TEXT NOT NULL,
                         status TEXT, started_at TEXT, completed_at TEXT,
                         context_json TEXT, cost_usd REAL DEFAULT 0,
                         budget_cap_usd REAL, snapshot_path TEXT );  -- budget_cap 见 §22
CREATE TABLE node_executions ( id TEXT PRIMARY KEY, run_id TEXT, node_id TEXT,
                         runner_id TEXT, status TEXT, attempt INTEGER DEFAULT 1, agent_id TEXT,
                         started_at TEXT, completed_at TEXT, last_heartbeat_at TEXT,
                         worktree_path TEXT, error TEXT, output_json TEXT,
                         cost_usd REAL DEFAULT 0, output_similarity REAL );
CREATE TABLE artifacts ( id TEXT PRIMARY KEY, project_id TEXT, run_id TEXT, node_id TEXT,
                         execution_id TEXT, file_path TEXT, artifact_type TEXT, status TEXT,
                         checksum TEXT, created_at TEXT, validated_at TEXT );
CREATE TABLE artifact_deps ( artifact_id TEXT, depends_on TEXT, PRIMARY KEY (artifact_id, depends_on) );
CREATE TABLE events ( seq INTEGER PRIMARY KEY AUTOINCREMENT, id TEXT UNIQUE,
                      instance_id TEXT, run_id TEXT,          -- 平台域事件 run_id 为空（P10 多租户）
                      ts INTEGER, type TEXT, idempotency_key TEXT UNIQUE,
                      schema_version INTEGER DEFAULT 1, payload_json TEXT );
CREATE INDEX idx_events_run_seq ON events(run_id, seq);
CREATE INDEX idx_events_instance_seq ON events(instance_id, seq);
CREATE TABLE executor_procs ( id INTEGER PRIMARY KEY AUTOINCREMENT, spawn_id TEXT,
                      pid INTEGER, proc_type TEXT, port INTEGER, started_at TEXT, killed_at TEXT );
CREATE TABLE validation_results ( id TEXT PRIMARY KEY, artifact_id TEXT, execution_id TEXT,
                      validator_id TEXT, validator_type TEXT, passed INTEGER,
                      evidence_json TEXT, reviewed_by TEXT, ts INTEGER );
CREATE TABLE feedback_log ( id TEXT PRIMARY KEY, artifact_id TEXT, execution_id TEXT,
                      reviewer TEXT, category TEXT, location TEXT, expected TEXT, detail TEXT, ts INTEGER );
```

> 数字人 memory 写入的审计事件记入 `events`（§6.3）；memory 内容本体存于 workspace（不入表）。

---

## 17. 网络协议、安全与部署 profile

### 17.1 协议（含 C2 决议）

- **控制面 ↔ runner、控制面 ↔ 客户端/IM**：gRPC / WebSocket（protobuf 契约，codegen Go+TS）。desktop 跑 localhost，cloud 跨网。
- **对外开放 API**：grpc-gateway 暴露 RESTful；列表查询支持 RSQL。契约单源 `/schema`。
- **控制面 = 唯一写者**；CLI/客户端/runner/外部工具不得直接写 StateStore（只读查询可）。一切变更经协议。
- **AG-UI 对齐**（§29.4）：客户端协议设计时对齐 AG-UI 的流式状态同步与 HITL 语义（或提供兼容适配层）。

### 17.2 安全

- **会话 Token**：状态变更命令需 `Authorization: Bearer <token>`；只读命令免 token。**执行器 Agent 环境不注入 token**（防自批 review）。
- **Runner 接入 Token**：一次性 token 5 分钟有效（§5.3.1），确认后转长期；取消/超时即失效。
- **AuthProvider 抽象**：desktop = 本地 token / 近 no-op；cloud = SSO / RBAC。数据模型多租户感知，隔离强度按 profile。
- **密钥**：配置敏感字段用 `{ $env: 'VAR' }` 引用，不写值；init 自动建 `.chaosplus.env` 并入 `.gitignore`。
- **执行器隔离**：worktree 沙箱，最小权限环境变量；Constitution 层强制 forbiddenActions（禁访问**自身 run 目录（`.chaosplus/runs/{run_id}/{node_id}/{attempt}/`）之外的** `.chaosplus/` 路径、其他 worktree、token 文件、IPC 调用）。v1 不引入 OS 级沙箱。

### 17.3 部署 profile

| | `desktop` | `self-hosted` | `cloud` |
|---|---|---|---|
| 控制面+web+runner | 桌面壳内嵌，localhost | 团队服务器 | 云集群 |
| 租户 | 单租户 | 单/少租户 | 多租户 |
| Auth | 本地 token | 本地/简单 | SSO/RBAC |
| Runner | 内置 1 + 局域网 | 本地池 | 用户本地机 |
| 消息扇出 | 进程内 | 进程内 / NATS | NATS |
| 离线 | 全内嵌，自包含可离线 | 可离线 | 在线 |

**桌面壳职责**：监管内嵌 Go 进程（control-plane + runner 二进制）、端口、崩溃、升级；不硬依赖任何 cloud 专有服务（保离线——§29.5 Vibe Kanban/Bloop 教训：local-first 是被市场验证的护城河）。

### 17.4 monorepo 结构

```
/schema         protobuf/JSON Schema → codegen Go + TS（唯一契约源）
/control-plane  (Go) API/引擎/调度/reconciler/会话Hub/数字人托管
/runner         (Go) ExecutionBackend/ArtifactStore/执行器适配器
/cli            (Go) 静态二进制（chaosplus）
/web            (Next.js + ShadCN + tailwindcss) UI/聊天/可视化编辑器[v2+]
/desktop        (Tauri/Electron) 内嵌 control-plane+runner 二进制 + 起 web
```

---

## 18. 多运行时执行器

```
ExecutorType = 'claude-code' | 'codex' | 'opencode' | 'kimi' | 'gemini-cli' | 'mock' | string
```

开放枚举（Multica 已验证 14+ CLI 生态：Copilot CLI / Cursor Agent / Trae 等均可后续以适配器加入）。每个适配器：启动进程、注入上下文、监听心跳、读取输出，映射原生退出码到标准语义。**标准目录**（`.chaosplus/runs/{run_id}/{node_id}/{attempt}/{context,output,logs}`、`continue.md`、`exit_code`）、**心跳协议**（15s）、**退出语义**（0 完成 / 1 失败重试 / 2 上下文不足 pause / 3 主动放弃 pause）均为本规范定义；**七层上下文管理**见 §18.1。Runtime 自动检测可用 runtime 并写 `meta` 表（machine 详情「运行时」Tab 的数据源）。

### 18.1 七层上下文管理（自包含定义）

执行器每次 spawn 时注入的上下文按七层组装，层序即优先级（高层不可被低层覆盖）：

| 层 | 内容 | 来源 |
|---|---|---|
| 1 角色宪法 | systemPrompt + forbiddenActions（Constitution 层强制）| ExecutorAgentSpec |
| 2 任务契约 | 本节点 in/out/artifact/validator spec 摘要 | ExecutorAgentSpec |
| 3 输入产物 | consumes artifacts 的内容或路径清单 | ArtifactStore |
| 4 结构化反馈 | 上次失败/拒绝的 `{category, location, expected, detail}`（仅重试时注入）| feedback_log（§13）|
| 5 项目规约 | 已批准 ADR + 模板规约（只读）| 治理产物（§14）|
| 6 技能 | requiredSkills 的 SKILL.md 注入 | skills 表（§6.4）|
| 7 运行环境 | 标准目录说明、退出语义、心跳要求、`continue.md` 续跑说明 | runner 适配器 |

组装受 `maxContextTokens` 约束：超限时按 3→5→6 顺序把内容降级为路径引用；第 1/2/4/7 层永不裁剪。

**span 回调（预留）**：适配器现在就吐 OpenTelemetry 风格 span（LLMCall/ToolCall），哪怕只写本地日志，避免日后改所有适配器。

---

## 19. 其他技术机制

- **副作用声明**：节点输出 `sideEffects.json` 不空则执行前暂停确认（Terraform plan 风格）。
- **Checksum 策略**：见 §12 已述。
- **Run 版本隔离**：`workflow_def_snapshot` 保证在飞 run 不受定义变更影响。
- **非文件 artifact**：`external_state` 类型自动进 `needs_review`，人工确认。
- **测试基础设施**：Mock Executor（fixture）、Mock Reviewer（模拟人工审批）、Fake Clock、World Reconstruction Test。
- **Runtime GC**：按保留策略归档/删除旧事件、orphan snapshot、dead validation；`chaosplus gc [--dry-run]`。

---

## 20. 商业模式

| 层级 | 价格 | 内容 |
|------|------|------|
| 个人版（开源）| 免费 | 完整 Runtime，本地自托管（desktop / self-hosted）|
| 团队版（SaaS）| $20–50/人/月 | cloud profile：云端控制面、团队共享、Web Review、备份 |
| 企业版 | 按需 | SSO、审计、RBAC、私有部署 |

核心 AI 执行费用由用户自己的 API key 承担。后续可加模板/连接器市场分成（市场方向草案见 §30，已冻结待 v2 再议）。

---

## 21. 发布路线图（R1 决议后重排）

### 21.1 v1 楔子版（desktop profile，软件开发场景）

| 里程碑 | 内容 | 可运行闸门 |
|---|---|---|
| **V1-M0 骨架** | monorepo、`/schema` codegen、Go 控制面+runner 骨架、网络协议、SQLite StateStore、本地 ArtifactStore、事件日志、CLI、runner 接入流程（§5.3.1 含 5 分钟 token）| runner 经完整接入流程连上控制面，事件落库 |
| **V1-M1 静态执行核** | `workflow-def.schema.json` + 加载/校验（X-3 裁决：schema 与校验属引擎能力，M1 交付；`software-dev-agile` 示例定义文件同期落库）、DAG 引擎、节点类型、执行器经 runner spawn（先 mock）、artifact 生命周期+验证器、Reconciliation、有界自治、World Reconstruction Test 进 CI | `software-dev-agile` 用 mock 端到端跑通 |
| **V1-M2 最小协作面** | 最小 web UI（Next.js：machine 管理 §5.3、数字人管理 §6.2、仪表盘最小版、个人中心最小版）、项目 channel（# ALL）聊天、@mention 路由、数字人（workflow-only + 最小记忆 §6.3）、审批回灌聊天、响应式导航（桌面顶部/手机底部）| 聊天里 @数字人触发工作流并完成一次人工审批/合并 |
| **V1-M3 真执行器** | claude-code 适配器 + 第二执行器（`pi --rpc` 或 codex）+ mock 常备（R7）；作者面工具链（编辑器 `$schema` 联想集成）；局域网 runner；NFR 压测（附录 E.1 口径）| 真实执行器端到端跑通 software-dev 工作流，产出经 validator 裁决；NFR 目标值校准完成 |

### 21.2 v2+ 快速跟进 backlog（按需求牵引排期，功能定义见对应章节）

| 项 | 来源章节 |
|---|---|
| 可视化编辑器、AI 生成作者面 | §8 |
| 多 runner 并发压测、租约+fencing 故障转移验证 | §15.1 |
| cloud profile（Postgres、S3、多租户 Auth/RBAC、控制面上云）| §17.3 |
| 外部 IM 连接器（Slack/微信/Telegram…）、消息桥接（mqtt/ws/http）| §9.2 |
| 数字人 `direct/both` 策略、记忆压缩/遗忘/人审策略 | §6.2/§6.3 |
| 技能沉淀闭环（从 run 提炼 skill）| §6.4 |
| Git 平台深度集成（PR/Issue/Actions 作 validator）、移动监督面板加强 | §29.2 |
| A2A 端点（数字人对外互操作）| §29.4 |
| 协作区（需求/任务/缺陷/测试 = 工作流 artifact 视图；OKR/绩效/洞察/脑暴/设计 另议）、看板 | 附录 A（C5 决议）|
| 预览区（runner 端口转发/隧道；desktop=localhost 直连，cloud=frp 类隧道）| 附录 A（C6 决议）|
| 在线文件（v2 先 artifact 预览 md/pdf/图片；doc/xls/ppt 在线编辑另议）| 附录 A（C5 决议）|
| 定时任务 UI（提醒人类）| §7.2 |
| 完整 i18n（繁中/英）、5 套主题全量 | 附录 A |
| 其余领域模板打磨（novel-writing 等）| §7.5 |
| 市场（验证包市场）| §30（冻结再议）|

---

## 22. 成功指标

自主完成率、Reconciliation 漂移检测准确率、崩溃恢复成功率、循环收敛速度、原地打转触发率、人工介入频率/耗时、孤立 worktree 数、幻觉完成率、平均每 run 费用、超 budget_cap 频率。

`budget_cap` 定义：`workflow_runs.budget_cap_usd`（StartRun 可选参数，缺省不限）；run 累计 `cost_usd` 超限 → run 转 `paused` 并通知（不自动终止，人工决定续跑或取消）。

非功能需求目标值与 v1 功能级验收标准见**附录 E**。

---

## 23. 附录：内置模板规格

### A. 软件开发（敏捷）`software-dev-agile`（v1 交付）
```
trigger → requirements(pm) → prd(pm) → [prd-approval:human]
→ design(arch) → [arch-approval:human]
→ sprint-plan(pm) → [parallel_fork] → coding-1..N(coder)
[join] → qa(qa) → [condition] ↗ passed → [sprint-delivery:human]
                              ↘ failed → bug-fix(coder) → qa [loop, max 5]
```

以下均为模板内容，**不进内核**：

- **角色库**：pm / arch / coder(FE/BE/Mobile) / qa / security / ui / devops。每个角色 = 一份模板提供的 ExecutorAgentSpec 预设（systemPrompt、allowedTools、forbiddenActions、in/out spec）。
- **DOM Contract**（ui / coder(FE) / qa 三方共同合约）：ui 角色产出的界面结构合约 artifact——组件树、语义化选择器、状态与交互清单；coder(FE) 的实现必须匹配合约，qa 的 Structural/Interaction 自动验证器按合约断言。合约本身走 artifact 生命周期（上游改动 → 下游 stale）。
- **领域配置**：端口分配 `basePort + taskId % 1000`、monorepo 布局约定、跨仓依赖声明。

### B. `novel-writing`（[v2+]，草案级定义）
```
trigger → 大纲(outliner) → [outline-approval:human]
→ [parallel_fork] → 章节-1..N(writer) → [join]
→ 一致性审校(editor) → [final-approval:human]
```
artifact：大纲 / 角色设定 / 章节文稿。validator：风格与连贯性（AI 辅助层）+ 人工终审。

### C. `video-production`（[v2+]，草案级定义）
```
trigger → 脚本(writer) → [script-approval:human] → 分镜(storyboard)
→ 素材与剪辑(editor) → 字幕对齐验证(automated) → [final-approval:human]
```
artifact：脚本 / 分镜 / 成片 / 字幕。validator：字幕时间轴对齐（自动）+ 人工验收。

### D. `content-moderation`（[v2+]，草案级定义）
```
trigger(内容流) → 初审(moderator) → [condition] ↗ 通过 → 放行(transform)
                                     ↘ 存疑 → [human_approval] → 处置(agent)
```
artifact：审核记录 / 处置结果。validator：策略合规检查（自动）+ 人工复审。

> B–D 为草案级模板定义，排期进 v2 时细化角色 spec 与验证器。

---

## 24. 与既往文档关系（自包含声明）

**本文档自包含：所有被采纳的历史内容均已内联，不依赖任何仓库外或已佚失的文档。**

| 文档 | 位置 | 状态 |
|------|------|------|
| **PRD（本文档 v2.0）** | 根目录 `PRD.md` | **唯一权威规格（定稿，自包含）** |
| PRD6 | 根目录 `PRD6.md` | 仓库内历史输入：架构规格来源，已被本文档融合 |
| chaosplus | 根目录 `chaosplus.md` | 仓库内历史输入：产品功能树来源，已被本文档融合 |
| PRD1–PRD5、RFC_260523、partyA | **已佚失** | 被采纳内容已全部内联于本文档，不再引用 |

### 历史决策对照（存档：早期设计 → 现行决策）

| 早期设计 | 现行决策 |
|---|---|
| 引擎语言 = TS | **Go 引擎**；无 TS 代码 DSL；JSON/YAML+可视化+AI；WASM/Lua 逃生口 |
| 单进程 Kernel + Unix socket IPC | **网络化控制面（gRPC/ws）+ 跨机 runner**；云优先 + desktop 嵌入式 profile；token 认证泛化 |
| 单进程天然有序，不需分布式时钟 | **per-run 单写者 + fencing，第一天生效** |
| v1 不做多租户 | **数据模型第一天多租户感知**；RBAC/SSO 按 profile 推到 cloud |
| MVP 排除 UI/通知 | desktop profile 核心含 web UI + IM |
| 可视化/连接器列远期 | [v2+] 快速跟进（R1 决议）|

**P7 反模式复核**：聊天是上层作者面、执行恒静态 → 「LLM 决定流程」未破；数字人默认 `workflow-only` → 「agent 自报完成」由 validator 裁决；其余原样保留。

---

## 25. 市场与竞争分析（2026-05 调研归档）

> *本章为 2026-05 web 调研结论的归档。数据随市场快速变化，引用日期以来源为准。*

### 25.1 市场时点（痛点已被验证 —— 最大利好）

- AI agent 市场 2026 ≈ **$9–11B**，CAGR ~45–50%（2030 ~$50B）；Gartner 称 2026 是企业 AI 支出「拐点年」（总 AI 支出 ~$2.59T）。
- **决定性数据**：**79% 企业已采用 agent，仅 11% 真正上生产**；Gartner 预测**到 2027 年 >40% 的 agentic 项目会被砍**，原因是「价值不清、成本失控、治理薄弱」。
- 治理缺口被量化：63% 无法对 agent 强制目的限制、60% 无法快速终止失控 agent、33% 缺审计级日志；EU AI Act 罚则 €35M / 7% 营收。

> **「11% 生产 / 治理鸿沟」就是 chaos.plus 的命脉。** 「artifact 即真相 + 验证决定完成 + 有界自治 + 不可删审计 + 静态可复现 DAG」正对着全市场最大的未满足需求：**可治理、可复现、能上生产**。对外定位是「跨越生产鸿沟 / 可治理可复现的 agent 执行」（已落入 §1.4）。

### 25.2 竞争地图（门槛极高 —— 清醒认知）

| chaos.plus 的能力 | 已被谁规模化占据 | 含义 |
|---|---|---|
| 持久执行/崩溃恢复/重放 | **Temporal**（$300M D 轮/$5B/3000+ 客户，已出 Durable AI Agent Bundle + OpenAI/Vercel/Google ADK 集成）；**LangGraph 1.0**（durable execution + time-travel，Uber/Klarna/JPM 在用）| **事件溯源+重放不再新颖，是 table stakes。** 不应从零重造 Temporal/LangGraph 级持久执行（R2 已决：自建但沿事件源最小路线）|
| 人在回路（HITL）| **LangGraph 1.0** 将 pause/approve/modify 做成头牌特性 | HITL 已商品化；差异在**深度**（产物验证/结构化反馈/reconciliation）非「有没有」|
| 可视化工作流 + AI + 可审计 | **n8n**（$5.2B，SAP 注资并嵌入 Joule Studio，1.7M MAU，多 agent + MCP 节点）；中国 **Coze 扣子 Workflow 2.0**（可视化+AI 自动规划+多 agent+模型路由，免费/字节）| n8n/Coze 已是「可视化+AI+可审计」那个产品，可视化编辑器是正面竞争（R1 决议后已降 v2+）|
| 自治软件工程 | **Devin/Cognition**（$10.2B→传 $25B，收购 Windsurf）；**OpenHands**（开源/自托管/自带 key/本地沙箱/可查每步推理）；Claude Code；Cursor（$29B/$500M ARR）| 软件开发楔子会撞这些；**OpenHands 尤其占了「开源+自托管+自带 key+透明」生态位** |
| 多 agent 编排 | LangGraph/CrewAI/MS Agent Framework/OpenAI Agents SDK/Google ADK；MCP+A2A 已进 Linux 基金会 | 框架层彻底商品化，「边界消融，未来是多框架组合」|
| 细粒度 tracing | **LangSmith**（每节点 token/成本/时延）| 可观测层亦有现成赢家 |

### 25.3 三条不舒服但重要的真相

1. **持久执行已被商品化。** 用 Go 从零重造它，可能把稀缺精力花在 Temporal/LangGraph 已做透处（R2 已决，见 §27）。
2. **几乎每个单项都有融资 $1B+ 的在位者。** 能赢的是**组合 + 纪律**（世界状态对账 + 产物即真相 + 不允许幻觉完成）作为一个**治理姿态**，不是某个单点功能。
3. **离 chaos.plus「形状」最近的是 OpenHands / Dify / Coze**（开源/自托管/自带 key/可视化）。差异点真实但微妙：**它们是「一次性任务执行器」，chaos.plus 是「持续维护世界状态的 reconciler」**（Runtime not Pipeline）——此差异必须一句话讲清。

### 25.4 来源（2026-05 调研）

市场：tech-insider「Agentic AI Enterprise 2026」、saasultra「AI Agent Statistics 2026」、Gartner 2026 支出预测。竞品：Temporal Series D（xgrid / thenewstack）、LangChain「LangGraph 1.0」、PRNewswire「n8n $5.2B / SAP」、SiliconANGLE「Cognition $25B」、amplifilabs「Devin vs OpenHands」、gurusup「Best Multi-Agent Frameworks 2026」。治理：Kiteworks「AI Governance 2026」、Raconteur「Autonomous AI agents 2026」。中国：aigc.cn「扣子 Workflow」、coze.cn、IT之家「2026 企业级 AI 智能体选型」。

---

## 26. 差异化与战略定位

### 26.1 一句话护城河

> **"别人的 agent 跑完一个任务就结束；chaos.plus 持续维护一个可验证、可复现、可审计的世界状态——失败会收敛，完成由验证裁决，不靠 agent 自报。"**

此定位对应市场第一痛点（生产/治理鸿沟），且是 Temporal（只给持久性）、n8n/Coze（只给可视化自动化）、Devin/OpenHands（一次性任务）都未正面占据的位置。

### 26.2 对每个在位者的定位一句话

| 对手 | chaos.plus 的差异 |
|---|---|
| Temporal | 不只是持久性，而是**产物即真相 + 验证决定完成** |
| LangGraph | 我们是**带治理的运行时**，不是开发框架 |
| n8n / Coze | 我们是**可复现的自治执行**，不是可视化自动化 |
| Devin / OpenHands | 我们**持续维护世界状态一致性**，不是一次性任务 |

### 26.3 楔子（R1 已决，落入 §1.4）

**软件开发场景的「可治理、可复现自治执行」**。理由：自动验证最强（编译/测试/lint）、生产鸿沟痛点最尖、可走 OpenHands 式「开源+自托管+自带 key」绕开 SaaS 巨头、「世界状态对账 vs 一次性任务」差异最明显。

### 26.4 主市场（待 R3 决策）

- **海外（建议主战场）**：开源 + 自带 key + 治理合规（SOC2 / EU AI Act），「可监管可复现」是真卖点，付费意愿高。
- **国内**：Coze 免费 + 字节生态碾压，开发者 SaaS 付费意愿低；建议做开源社区，不做商业主战场。

---

## 27. 风险与开放问题

| # | 风险 / 开放问题 | 状态 |
|---|---|---|
| **R1** ✅ **已决 (2026-08-01)** | 范围 vs 资源 | **收敛为楔子版 v1**（§1.4/§21.1）：软件开发可治理可复现执行核；IM 连接器/可视化/多 runner 压测/云为快速跟进（§21.2）|
| **R2** ✅ **已决 (2026-05-23)** | 持久执行：全 Go 自建 vs 借力(Temporal/DBOS/go-workflows) | **全 Go 自建，沿 M0 事件源路线**。调研结论：Temporal 生产需独立 server+DB(破坏桌面嵌入)；DBOS-Go 仅 Postgres(破坏 SQLite-first)；go-workflows 可嵌入但 workflow-as-code 模型与数据驱动静态 DAG 不匹配；三者都不省去 artifact/验证/对账/有界自治这些自有的活，而崩溃恢复已由事件源提供。保留 Scheduler/timer 接口，未来可按需把 go-workflows 塞在定时器/队列后面。|
| **R3** | **主市场未定**（国内/海外）——决定 IM/模型/合规/支付/竞品全套 | 未决。建议主攻海外开源开发者/团队 |
| **R4** ✅ **已决 (2026-08-01)** | ICP 定义 | **多项目独立开发者 / 2–5 人小团队技术负责人**（§1.5）；端到端旅程 J1–J10 为 v1 验收基线 |
| **R5** | **法律最小集**：开源许可证未定；产物 IP/自治动作责任 ToS 缺；cloud profile 的数据驻留与 **GDPR 删除权 vs append-only**（crypto-shredding，cloud 上线即 v2 问题）；SOC2；v1 无 OS 级沙箱 | 未决。落地前定许可证 + ToS；删除权方案前移；cloud profile 前补 SOC2/沙箱 |
| **R6** | **商业计划层缺失**：无 GTM、无单位经济、无商业里程碑 | 未决。补「商业里程碑」（首个真实工作流→首个外部自托管用户→首个付费）与 §21 并列 |
| **R7** | **外部依赖风险**：命脉绑在外部执行器 CLI（claude-code 等）的行为/定价/可用性 | 缓解中：多执行器抽象（§18）；V1-M3 要求 ≥2 个可用执行器 + mock 常备 |

---

## 28. 参考实现（外部开源项目借鉴，2026-05 调研归档）

> *2026-05 调研了两个外部开源项目（Rust 编码 agent「DeepSeek-TUI」，Codex/OpenCode 血统；TS agent harness「pi」）。项目本体不在仓库、不作为依赖；下表仅存沉淀下来的设计模式，作为本规格的设计输入，全部自行实现。*

| 模式 | 调研来源 | 用于 chaos.plus |
|---|---|---|
| npm 分发原生二进制（postinstall 下载 Release + checksum + spawn 壳）| DeepSeek-TUI | `npx` 分发 Go 二进制（`machine-runner@chaos.plus`）|
| 分层命令门控（Builtin<Agent<User，最长前缀 allow/deny）| DeepSeek-TUI | ExecutorAgentSpec 的 `allowedTools/forbiddenActions` |
| ToolSpec（`approval`/`supports_parallel`/`read_only`）+ 目录记忆化保 KV-cache 稳定 | DeepSeek-TUI | 工具硬约束字段 |
| MCP per-server `enabled/disabled_tools` + secret 脱敏 | DeepSeek-TUI | MCP 白名单 |
| 沙箱后端抽象（seatbelt/landlock/JobObject）| DeepSeek-TUI | 执行隔离（v2+）；**Windows 仅进程树** |
| JSON-RPC 2.0 app-server（HTTP+stdio，Envelope 帧）| DeepSeek-TUI | 网络协议参考 |
| **runner↔执行器 JSONL/stdio RPC**（prompt/steer/abort/get_state + 人工审批旁路）| pi | **ExecutionBackend 协议设计** |
| agent loop 事件分类 + hook 契约（`beforeToolCall` block / `afterToolCall` override / hook 永不抛）| pi | 事件溯源 + validator/工具门控 |
| `SKILL.md`（Claude 兼容 frontmatter + 目录名校验 + `<skill>` 注入）| pi + DeepSeek-TUI | agent spec 的 skills（§6.4 `skills` 表格式）|
| 双层 config（项目级 vs 全局级，可白标）| pi | SDK/CLI 配置 |
| 工程质量栈（Biome + tsgo + husky check 门 + 钉死依赖/shrinkwrap）| pi | TS SDK/UI 工程化 |

**反面教材（避开）：**
- **上帝模块**：调研对象曾把 21.3 万行堆进单一模块、其余包只是门面 → **强制 Go 包真实边界**。
- **无界 history 落盘崩溃**（调研对象自承长会话会退化崩溃）→ **反证事件溯源 + 有界/增量持久化 + 七层上下文管理（§18.1）是对的**。
- **build 顺序硬编码、无 MCP、全包锁步版本** → 用 TS project references；MCP 自建；SDK/UI/引擎独立版本。

> **落地启示**：V1-M3 的「首批真实执行器适配器」候选之一为 pi CLI 的 `--rpc` 模式（具备 prompt/steer/abort/状态查询 + 人工审批旁路，作为外部执行器 CLI 接入，与 claude-code 同级）；npm 分发壳与 SKILL.md 规范按上表模式自行实现。

---

## 29. 外部借鉴增补（2026-08 调研）

> *§29.1–29.3 的吸收方案已升格并入正文（§6.4 技能沉淀、§6.2.2 管理 UI、§6.3 记忆子系统、§11 CI validator、§17.1 AG-UI）；本章保留调研出处与未入正文的部分。*

### 29.1 技能沉淀复利机制（借鉴 Multica）

[Multica](https://github.com/multica-ai/multica)（multica-ai，~7.4k★，Go 后端 + Next.js + Electron + CLI/daemon，与本项目技术栈高度同构）核心创新：agent 解决问题后方案固化为可复用 skill，全团队共享、随时间复利。已落入 §6.4。另吸收：**任务生命周期状态机对外暴露**（enqueue/claim/start/complete/fail，作看板任务视角模型 [v2+]）；**运行时枚举放宽**（已落入 §18）。

### 29.2 Git 平台深度集成（借鉴 Orca）

[Orca](https://github.com/nqcuong2030-ctrl/orca-agent-ide)（stablyai，~34k★，并行 agent 舰队 IDE）已规模化验证：worktree-per-agent + GitHub 深度集成（PR/Issue/Actions 挂 worktree）+ 桌面/移动/SSH 远程机。吸收（[v2+]，§21.2）：Git 平台连接器（run↔PR 关联；**Actions/CI 检查作为自动化 Validator 回灌** `validation_results`，符合 P3）；Issue 指派 → `trigger.source=Connector`；**移动监督面板**（审批/@mention/run 状态天然适合手机，v1 已做响应式导航打底）。

同名项目反向印证：[ThakeeNathees/orca](https://github.com/ThakeeNathees/orca)（"Terraform for AI agents" 声明式 DSL → 印证 JSON 唯一真相路线）；[VirtusLab/orca](https://github.com/VirtusLab/orca)（代码强制「AI 写码必须另一 agent 审」→ 印证 validator 裁决完成）。

### 29.3 数字人记忆研究结论（Letta / Mem0）

2026 共识（[Mem0 State of Agent Memory](https://mem0.ai/blog/state-of-ai-agent-memory-2026)、[Letta](https://www.letta.com/blog/towards-agents-that-learn/)）：记忆须覆盖五操作（存储/检索/更新/压缩/遗忘），多数团队只做前两个；**记忆投毒**测试中 90%+ agent 中招、对话内纠正 100% 复发。已落入 §6.3（可审计记忆 + 写入过治理）。

### 29.4 协议对齐（A2A / AG-UI）

2024–2026 分层开放协议栈已入驻 Linux 基金会（AAIF，190 成员含 Anthropic/Google/OpenAI/Microsoft/AWS）。MCP（agent↔工具）已在 spec；**A2A v1.0**（agent↔agent，2026 初定稿）→ 数字人暴露 A2A 端点 [v2+/cloud]，@mention 语义映射 A2A 消息，desktop 不硬依赖（P8）；**AG-UI**（agent↔前端）→ 已落入 §17.1。

### 29.5 市场教训：local-first 是护城河

Vibe Kanban 背后公司 Bloop 于 2026 初倒闭，项目转社区维护、云功能被迫全部本地化。**教训：依赖托管服务的 agent 编排器死于商业失败时会连累用户；local-first / desktop profile（P8/P9）是被市场验证的真护城河，对外叙事强调「引擎永远本地可用，云是增值」。** 同赛道综述（[Augment Code](https://www.augmentcode.com/tools/open-source-agent-orchestrators)、[awesome-agent-orchestrators](https://github.com/andyrewlee/awesome-agent-orchestrators)）指出全行业共同短板：任务对齐、冲突解决、合并决策仍全丢给开发者——正是 validator + reconciliation 要吃的空白。

---

## 30. 市场（Marketplace）方向草案

> *状态：**已冻结，v2 再议**（2026-08-01 决策：「市场再说吧」）。保留设计结论备用。*

### 30.1 四种候选形态的调研结论（2026-08）

| 候选 | 现状 | 判断 |
|---|---|---|
| **Prompt 市场** | PromptBase 等，早已商品化，单价与壁垒双低 | ❌ 不做 |
| **Skill 市场** | Claude Skills 生态、MCP 目录（Smithery/mcp.so）已泛滥；**ClawHavoc 事件（2026-02）：无审核社区市场确认 341 个恶意 skill** | ❌ 不做独立市场；skill 是包的组成部分，安全审核是机会 |
| **Agent 市场** | GPT Store、Coze 商店、AWS AgentCore Marketplace，巨头主场 | ❌ 不正面做 |
| **Session/轨迹市场** | 已存在：Sell Traces 等按条卖 agent 轨迹给实验室做 RL 训练（eval ~$8.4/条、RL ~$22/条、代码基准 ~$75/条）；Letta trajectory / Harbor ATIF 已有标准格式 | ⚠️ 原始直卖有隐私/IP 问题且已有玩家；session 的正确用法见 30.2 |
| **模板/工作流市场** | n8n 模板库 9,400+（近月新增 78% 为 agent 模板）；Profound、AI Hive（verified installs + 人工评审为卖点）| ✅ 赛道成立，竞争焦点已转向**验证与信任**——本产品独有能力 |

### 30.2 设计结论：「验证包市场」（Verified Workflow Pack Marketplace）

**一个市场、一种商品：**

> **商品 = 工作流包（Pack）** = WorkflowDef JSON + ExecutorAgentSpec 集 + Skill 集 +（可选）DigitalHumanAgentSpec + 领域验证器配置。
> **信任层 = 验证凭证（Receipt）** = 真实成功 run 的回放证据：事件日志摘要 + artifact checksum + validation_results。

- **session 当凭证，不当商品**：「此包有 N 次经 validator 裁决的成功执行，凭证可回放核验」——n8n/Coze/GPT Store 给不出，因为只有本产品有事件溯源 + validator 裁决 + World Reconstruction。
- **供给端流水线 = §6.4 技能沉淀**：成功 run → 提炼 skill → 组包 → 挂凭证 → 上架，市场自我造血。
- **安全审核一等公民**（ClawHavoc 教训）：上架须过 spec 静态审核 + 沙箱试跑；**凭证由控制面签名，不由卖家自报**（P1 纪律延伸到市场）。
- **凭证脱敏**：只含事件类型序列、checksum、validator 结论与统计，不含 artifact 内容/代码/密钥。

### 30.3 次级变现与分期（冻结备用）

- 轨迹数据变现（opt-in 脱敏导出，ATIF/Letta trajectory 格式）为副产品。
- 分期：市场 v0（包格式 + 签名凭证 + 免费目录）→ v1（付费分成 + 卖家信誉 + 审核流水线，随 cloud）→ v2（轨迹变现、企业私有市场）。
- 依赖：R3 定结算合规；R5 定 IP 条款；凭证签名依赖事件日志 + §17.2 token 体系。

---

## 附录 A：产品 UI 全量功能规格（源自 chaosplus 功能树，标注 v1/v2+）

> Web 技术栈：Next.js + ShadCN + tailwindcss（C4 决议）。已并入正文的功能标注章节引用，此处不重复展开。

### A.1 基础功能 base

| 功能 | 明细 | 版本 |
|---|---|---|
| 多国语 i18n | 简体中文 | v1 |
| | 繁体中文、英文 | v2+ |
| 多主题 theme | 默认跟随系统（浅色/暗黑）| v1 |
| | 现代简约-浅色、新粗野主义（Neo-Brutalism）、粉色少女+渐变高级感、暗夜模式 | v2+ |
| 跨平台 cross | Windows / Darwin (macOS) / Linux | v1 |

### A.2 实例内一级菜单（响应式：web/pad 顶部，手机底部 —— v1）

| 区块 | 内容 | 版本 / 规格位置 |
|---|---|---|
| **仪表盘 dashboard** | 实例总览（v1 最小版：run 状态、runner 健康、待审批数）| v1 最小 / v2+ 完整 |
| **会话区** | 频道列表（默认 `# ALL` + 自建频道）、聊天区 | v1，§9 |
| | 频道：名称/成员管理/消息钩子 | v1，§9.2 |
| | 消息桥接（mqtt/websocket/http push+hook）| v2+，§9.2 |
| **看板** | 任务看板（任务生命周期视角：enqueue/claim/start/complete/fail）| v2+，§21.2 |
| **预览区** | 以进程为单位的实时预览（如 frp-api、frp-web、进程 n…）；runner 端口转发/隧道：desktop=localhost 直连，cloud=frp 类隧道（C6 决议）| v2+，§21.2 |
| **协作区** | 需求/任务/缺陷/测试 → 工作流模板的 artifact 视图（C5 决议：不新造模块）| v2+ |
| | OKR/脑暴/设计/洞察/绩效 | v2+（另议）|
| **扩展区** | 在线文件：v2 先 artifact 预览（md/pdf/图片）；doc/xls/ppt/任意文件在线编辑另议（C5 决议）| v2+ |
| | 定时任务：任务名称/执行者（人或 agent）/定时提醒；列表；搜索过滤（映射见 §7.2）| v2+ |
| | 工作流：可视化编辑器（JSON 唯一真相，画布是投影，C7 决议）| v2+，§8 |
| **管理区** | 人类成员 humans（v1 = 成员列表只读展示，当前账号 + 数字人；邀请/新增人类成员 [v2+]，随 self-hosted/cloud）| v1 最小 |
| | 电脑机器 machines（接入流程/列表/详情）| v1，§5.3.1 |
| | 智能体 agents（数字人管理/详情/注销/DMS/技能/工作目录）| v1，§6.2 |
| **个人中心** | 基本信息：邮箱/昵称/头像（类似 NFT 高精度像素人——v1 可先占位头像）| v1 最小 |
| | 安全设置：修改密码 | v1 |
| | 个性配置：指定主题/指定语言 | v1（随已交付主题/语言范围）|

### A.3 服务端与消息（产品视角）

- **接口 API**：Golang；对外 RESTful + RSQL（经 grpc-gateway，C2 决议）；查询代码 sqlc 生成（C9 决议）。
- **消息**：NATS（WebSocket/TCP）仅 cloud/self-hosted 传输层（C3 决议）；desktop 进程内直连。

### A.4 machine 侧（产品视角）

一台 machine（runner）可托管多个数字人 Agent；每个数字人绑定运行时（claude code / codex / kimi …，§18）与大模型配置。

---

## 附录 B：术语表（定稿统一用词）

| 定稿术语 | 同义/历史用词 | 定义 |
|---|---|---|
| **chaos.plus** | Myrmidon | 本产品/引擎/品牌统一名（C8 决议）|
| **Machine Runner**（简称 runner）| 电脑机器 machines | 跑在用户机器上的执行面进程，托管执行器实例与本地 ArtifactStore（§5.3）|
| **执行器 Agent**（Executor）| 临时工、Worker | 无状态瞬时认知单元，跑完一个节点即销毁，不可 @mention（§6.1）|
| **数字人 Agent**（Digital-human）| 智能体 agents（产品树用词）| 长期成员，持久身份+记忆，可 @mention（§6.2）|
| **执行器运行时**（ExecutorType）| 运行时 runtime | claude-code/codex/kimi 等外部 CLI（§18）|
| **Instance** | 实例、组织 | 账号下隔离单元；desktop UI 默认与项目 1:1 呈现（C1 决议）|
| **Channel** | 频道 | 会话+同步单元，事件溯源日志；项目默认频道呈现为 `# ALL`（§9）|
| **DMS** | 智能体间私信 | 数字人之间的对话，特殊 channel 投影（§6.2.2）|
| **Artifact** | 产物 | 世界状态的唯一真相载体（§10）|
| **WorkflowDef** | 工作流定义 | JSON 唯一真相，画布/聊天/AI 皆作者期投影（§7）|
| **Pack / Receipt** | 工作流包 / 验证凭证 | 市场商品与其签名执行证据（§30，冻结）|

---

## 附录 C：裁决记录（2026-08-01）

| # | 议题 | 决议 |
|---|---|---|
| **C8** | 命名 | **统一用 chaos.plus**（产品/引擎/品牌）；代码标识符用 `chaosplus` |
| **R1** | v1 范围 | **软件开发楔子**：执行核 + software-dev 模板 + 最小 web UI（machine/agent 管理、聊天审批）；其余降 v2+（§1.4/§21）|
| **C1** | 实例/项目关系 | 数据层一实例多项目；desktop UI 默认 1:1 简化呈现 |
| **C2** | API 协议 | 内部 gRPC/ws；对外 grpc-gateway REST + RSQL；契约单源 `/schema` |
| **C3** | 消息中间件 | NATS 仅 cloud/self-hosted 传输层；desktop 进程内直连；事件日志唯一真相 |
| **C4** | 前端框架 | Next.js + ShadCN + tailwindcss，静态导出由桌面壳承载 |
| **C5** | 协作区/在线文件 | 需求/任务/缺陷/测试 = 工作流 artifact 视图（v2+）；OKR/绩效/洞察与在线编辑另议；在线文件 v2 先 artifact 预览 |
| **C6** | 预览区 | runner 补端口转发/隧道能力（desktop=localhost，cloud=frp 类），v2+ |
| **C7** | 工作流可视化 | 遵循 P7：JSON 唯一真相，画布是投影；非 n8n 双向模式 |
| **C9** | DB 工具链 | SQLite-first + 内置迁移；sqlc 生成查询（双方言）；goose 仅 cloud/Postgres |
| **R4** | ICP | 多项目独立开发者 / 小团队技术负责人；旅程 J1–J10 为 v1 验收基线（§1.5）|
| **市场** | §30 | 冻结，v2 再议 |
| **X-1** | 双「唯一真相」矛盾 | `events` 表是全系统唯一真相；channel 日志为其投影，`channel_messages` 可删除重建（§9.1/§16）|
| **X-2** | 数字人托管位置矛盾 | 数字人进程由绑定的 runner 托管，控制面只做路由调度；desktop 用内置本地 runner（§6.2）|
| **X-3** | 作者面排期矛盾 | workflow schema + 加载校验属引擎能力，M1 交付；作者面工具链 M3（§21.1）|
| **F 包** | 评审缺口补齐 | 架构评审 CRITICAL/HIGH 缺口的工程规格定义收录于附录 F（2026-08-01）|

---

## 附录 D：v1 页面级交互规格

> 仅覆盖 v1 页面；[v2+] 页面待其排期时补规格。字段标 `*` 为必填。

### D.1 仪表盘（最小版）

卡片：**活跃 run**（数量 + 状态分布：running/paused/failed）、**Runner 健康**（在线/离线 + 最近心跳时间）、**待审批队列**（数量，点击跳转对应 channel 消息）、**今日成本**（`cost_usd` 汇总）。空态：引导「创建项目 → 接入 machine → 创建数字人」三步卡片。

### D.2 Machine 接入向导（状态机，实现 §5.3.1）

```
idle(展示命令+300s倒计时) ──runner连接──▶ connected(显示hostname,可改名)
   │超时                                        │点确认           │超时未确认
   ▼                                            ▼                 ▼
expired(刷新命令按钮→回idle重新计时)      confirmed(创建成功)   自动断开→expired
任意状态点取消 → cancelled(关连接+token失效)
```

按钮/输入可用性：「确认」仅 `connected` 可点；名称输入仅 `connected` 起可编辑（预填 hostname）；「刷新命令」仅 `expired` 出现。断网/页面刷新等未决状态一律按超时取消处理。

### D.3 Machine 列表与详情

列表列：名称 / 状态（在线|离线）/ 最近心跳 / 托管 agent 数 / 操作。临时（未确认）machine 不出现。详情 Tab：**关键信息**（id、地址、OS、注册时间、长期 token 管理）/ **运行时**（检测到的 ExecutorType + 版本，源自 `meta` 表）/ **agent 列表**（基本信息、实时状态、操作按钮；搜索过滤）。

### D.4 数字人管理

- 列表列：名称 / 状态（运行中|已停止|已注销）/ 所属 machine / 操作（启动|停止|重启|详情）。
- 添加表单：名称`*`、描述、系统提示词`*`、所属 machine`*`、runtime`*`（下拉，来自该机检测结果）、模型配置`*`、默认 channels（默认 `# ALL`）。
- 详情 Tab 按 §6.2.2；**注销向导**：正常注销 = 分步 UI（生成交接文档 → 选择接手人/稍后 → 执行清理，映射 §6.2.1 六步）；强制注销 = 红色确认框，需输入数字人名称二次确认。

### D.5 聊天与审批卡片

消息类型：人类文本 / 数字人文本 / 工作流事件（run 启动、节点完成、pause_for_human）/ **审批卡片**。审批卡片字段：标题（节点名）、摘要、artifact 链接列表、[通过] [拒绝] 按钮（调 review.approve/reject，§9.4）。**拒绝必填结构化反馈**（§13）：`category*`（枚举：功能缺陷|样式|需求偏差|其他）、`location`、`expected`、`detail*`；未填必填项不可提交。

### D.6 个人中心（最小版）

邮箱（未设置时可填写，设置后 v1 只读）、昵称、头像（v1 占位图，NFT 像素人 [v2+]）、修改密码（旧+新+确认）、主题（跟随系统|浅|暗）、语言（简中）。

---

## 附录 E：非功能需求与 v1 验收标准

### E.1 非功能需求（目标值，V1-M3 压测校准；压测口径：mock 执行器 10 并发 run × 100 节点，测延迟/恢复/资源三组指标）

| 类别 | 目标 |
|---|---|
| 并发 | desktop 单机 ≥10 并发 run、≥50 排队；≥5 runner；每 runner ≥8 执行器进程 |
| 延迟 | 事件写入 P95 < 50ms（本地 SQLite）；聊天消息端到端 < 500ms（localhost）；UI 首屏 < 2s |
| 恢复 | 崩溃重启至状态可用 < 30s（含事件重放）；World Reconstruction Test 100% 通过（发布闸门）|
| 资源 | 控制面+runner 空闲内存 < 300MB；事件日志经 GC 后磁盘占用有界 |
| 可靠 | 心跳 15s；Phantom Running 检测 < 3 个心跳周期；接入 token 时效 300s（±5s）|
| 安全 | 执行器环境 0 泄漏控制面 token；forbiddenActions 违规 100% 拦截并写审计事件 |

### E.2 v1 功能级验收标准（关键项，Given/When/Then）

| 功能 | 验收标准 |
|---|---|
| Machine 接入 | token 过期后「确认」不可点且原 token 拒绝连接；已连接但超时未确认 → 自动断开；确认后 token 长期有效且 runner 断线可重连 |
| 数字人注销 | 正常注销产出交接文档 artifact（入库可查）且连接 token 失效；强制注销必经红色二次确认；两种注销后 agent 不再出现在活跃列表 |
| 聊天审批 | 审批卡片「通过/拒绝」写入 `validation_results` + 事件日志；拒绝未填 `category/detail` 无法提交；结构化反馈注入该节点下次执行上下文 |
| 有界自治 | 重试耗尽进入 `pause_for_human` 且对应 channel 收到通知；文件变化比例 < 0.08 触发暂停；join 分支失败按其重试策略处理 |
| 事件溯源 | 删除全部投影表与 snapshot 后重启，仅凭事件日志 + artifact 恢复到一致状态（WRT，CI 必跑）|
| 执行器隔离 | 执行器进程环境变量中不存在控制面 token；访问自身 run 目录之外的 `.chaosplus/` 路径或其他 worktree 被拦截并记审计事件 |
| 旅程基线 | §1.5.2 J1–J10 全程可在一台干净机器上走通（发布前人工演练一次）|

---

## 附录 F：工程规格包（2026-08-01 评审缺口补齐）

> 覆盖架构评审的 CRITICAL/HIGH 缺口。本附录是 `/schema` 的规格来源；proto/JSON Schema 文件为 V1-M0/M1 交付物，以此为准生成。

### F.1 网络协议（/schema 契约骨架）

**RunnerGateway（控制面 ↔ runner；runner 为客户端）**

```proto
service RunnerGateway {
  rpc Register(RegisterRequest) returns (RegisterResponse);          // 携带 bootstrap/长期 token
  rpc Channel(stream RunnerToServer) returns (stream ServerToRunner); // 注册后唯一双向长连接
}
// RunnerToServer = oneof { Heartbeat, SpawnResult, ExecutorEvent, ArtifactReport, RuntimeInventory, AgentState }
// ServerToRunner = oneof { SpawnRequest, KillRequest, ProbeRuntimes, AgentStartRequest, AgentStopRequest }
// —— 数字人常驻进程生命周期（X-2：runner 托管；ClientAPI.AgentAction 的下游落点）——
// AgentStartRequest { agent_id, spec_json }   AgentStopRequest { agent_id, mode: 'graceful'|'force' }
// AgentState        { agent_id, status: 'running'|'stopped'|'crashed', pid }

message SpawnRequest  { string spawn_id;          // 幂等键：{run_id}:{node_id}:{attempt}
                        string run_id; string node_id; int32 attempt;
                        string executor_type; string model_config_json;
                        string context_bundle_uri;  // 七层上下文物化包（F.8 目录）
                        string workdir; repeated string env_allowlist; int64 timeout_ms; }
message SpawnResult   { string spawn_id; bool ok; int64 pid; string error; }
message Heartbeat     { string runner_id; int64 ts;
                        repeated ExecutorState states; }   // { spawn_id, pid, last_beat_ts }
message ExecutorEvent { string spawn_id; oneof { HeartbeatTick, LogChunk, Exited{int32 exit_code} } }
message KillRequest   { string spawn_id; string signal; }  // 'TERM'(优雅,10s宽限)→'KILL'
```

**ClientAPI（控制面 ↔ web/CLI；经 grpc-gateway 暴露 REST + RSQL）**

变更型：`Login / CreateProject / RegisterMachineBegin|Confirm|Cancel / CreateAgent / AgentAction(start|stop|restart|retire{normal|forced}) / SubmitWorkflow / StartRun / ApproveReview / RejectReview / PostMessage`。
只读型（免 token，见 F.7）：`Get* / List* / Subscribe(stream Event)`（按 channel/run 订阅事件流）。

**RPC 幂等（C-10）**：所有变更型 RPC 必带 `client_request_id`（客户端生成 UUID）。服务端按 `(方法, client_request_id)` 去重：重复请求返回首次结果，不重复执行。该 id 同时进入平台域事件的 `idempotency_key`（§15.1）。

### F.2 事件类型目录（events.type 全量枚举）

| 域 | 类型 | payload 关键字段 | 更新的投影 |
|---|---|---|---|
| run | RUN_STARTED / RUN_COMPLETED / RUN_FAILED / RUN_CANCELLED / RUN_PAUSED / RUN_RESUMED | run_id, workflow_id, snapshot | workflow_runs |
| run | NODE_READY / NODE_STARTED / NODE_COMPLETED / NODE_FAILED / NODE_SKIPPED / NODE_RETRY_SCHEDULED / NODE_PAUSED_FOR_HUMAN / NODE_RESUMED | node_id, attempt, error?, output_json? | node_executions |
| run | EXECUTOR_SPAWNED / EXECUTOR_EXITED / HEARTBEAT_LOST | spawn_id, pid, exit_code? | node_executions, executor_procs |
| run | ARTIFACT_PRODUCED / ARTIFACT_VALIDATED / ARTIFACT_INVALIDATED / ARTIFACT_STALE_MARKED / ARTIFACT_FORCE_VALIDATED / ARTIFACT_ORPHANED | artifact_id, checksum, validator_id?, reviewed_by? | artifacts, validation_results |
| run | REVIEW_REQUESTED / REVIEW_APPROVED / REVIEW_REJECTED | node_id, reviewer, feedback{category,location,expected,detail}? | node_executions, validation_results, feedback_log, channel_messages |
| 平台 | MACHINE_REGISTER_BEGUN / CONFIRMED / CANCELLED / MACHINE_ONLINE / OFFLINE | runner_id, address, hostname | machine_runners |
| 平台 | AGENT_CREATED / STARTED / STOPPED / RETIRED | agent_id, mode('normal'\|'forced')?, handover_artifact_id? | members, agent_specs |
| 平台 | MEMORY_WRITTEN | agent_id, entry_digest, approved_by? | （审计，无投影）|
| 平台 | SKILL_SUBMITTED / SKILL_APPROVED | skill_id, source_run_id? | skills |
| 平台 | CHANNEL_MESSAGE_POSTED | channel_id, author_member_id, payload | channel_messages（投影，X-1）|
| 平台 | MEMBER_ADDED / REMOVED、TOKEN_ISSUED / REVOKED | member_id / token_id, kind | members / tokens |

每类事件的完整 payload JSON Schema 在 `/schema/events/` 逐类落文件（M0 交付）；投影更新 = 单事务内「append 事件 + 更新对应投影行」。

### F.3 run / node 状态机（权威定义）

```
run.status : pending → running → { completed | failed | cancelled }
             running ⇄ paused（人工暂停/恢复，或全部活跃节点均 paused_for_human）
node.status: pending → ready → running → { completed | failed | skipped }
             running → paused_for_human → ready（人工恢复/审批放行）
             failed → pending（重试调度，attempt+1，受 RetrySpec 上限）
             completed → pending（仅 loop 体内节点重入：iteration+1，独立于 attempt，
                                  受 loop.maxIterations 上限；重入产出覆盖同身份 artifact，
                                  checksum 变化天然触发下游 stale 传播 §10.3）
terminal(node) = { completed, skipped, failed(且重试已耗尽) }   ← §7.3 join 判定即用此集合
```

**失败分类映射（H-8）**：

| 现象 | node.status | 消耗 attempt |
|---|---|---|
| 退出码 1（业务失败）/ 验证失败 | failed | 是 |
| 退出码 2（上下文不足）/ 3（主动放弃）| paused_for_human | 否 |
| 超时（timeout_ms）/ 引擎主动 kill | failed | 是 |
| 心跳丢失（Phantom）/ runner 掉线 / 进程被外部杀死 | failed（infra）| 否；连续 3 次 infra 失败 → paused_for_human |

### F.4 WorkflowDef Schema（字段级定义）

```typescript
interface WorkflowDef { id: string; version: string; name: string;
  contextSchema?: JSONSchema;          // 校验 StartRun 传入的 context_json
  nodes: Node[]; edges: Edge[]; }
interface Node { id: string; type: NodeType; name?: string;
  agent?: ExecutorAgentSpec | { ref: string };        // ref 指向 agent_specs
  humanApproval?: HumanApprovalSpec;                  // type=human_approval
  condition?: { expr: JSONLogic };                    // type=condition
  transform?: { expr: JSONLogic; output: string };    // output=产物逻辑 id
  trigger?: { source: 'manual'|'schedule'|'webhook'|'connector'; scheduleCron?: string };
  loop?: { bodyEntry: string; condition: JSONLogic; maxIterations: number };
  subworkflow?: { ref: string; inputMapping: Record<string,string>; outputMapping: Record<string,string> };
  fanOut?: { itemsExpr: JSONLogic; templateNodeId: string }; }   // type=parallel_fork（H-3）
interface Edge { from: string; to: string;
  condition: 'success'|'failed'|'approved'|'rejected'|'always'; branchKey?: string; }
```

- **`branchKey` 语义**：仅用于 `condition` 节点的出边——`expr` 求值结果（字符串化）与出边 `branchKey` 做相等匹配选路；无匹配时若存在 `condition='always'` 出边则走之，否则该 condition 节点判 `failed`。

- **artifact 绑定**：`consumes/produces` 的 `id` = 项目内唯一的**逻辑 id**（默认即 `path`）；`path` 一律相对 Workspace 根解析。
- **JSON Logic 变量作用域**：`run.context_json` ∪ 各已完成节点的 `output_json`（以 nodeId 为 key）。禁止访问其它数据。
- **fan-out 展开（H-3）**：调度器在 fork 就绪时求值 `itemsExpr` 得数组，为每个元素克隆 `templateNodeId` 为子节点 `{templateNodeId}#{index}`（元素注入其 context）；join 以 fork 记录的实际展开数为分支数。
- 完整 `workflow-def.schema.json` 与 `software-dev-agile` 示例文件为 V1-M1 交付物。

### F.5 ExecutorAgentSpec 子类型定义（补 §6.1.1）

```typescript
type ValidatorRef = string;   // 'builtin:<id>'（内置验证器）| 'cmd:<命令模板>'（runner 上执行）
interface ValidatorSpec { ref: ValidatorRef; layer: 'automated'|'ai_assisted'|'human';
                          required: boolean; timeoutMs?: number; }
interface ArtifactSpec  { type: ArtifactType; schemaRef?: string; validators?: ValidatorRef[]; }
interface HumanApprovalSpec { approvers: 'any_human' | string[];   // memberId 白名单；desktop 单账号下 any_human = 当前账号
                              channel?: string;                    // 缺省=项目默认频道
                              timeoutMs: number; onTimeout: 'pause'|'auto_reject';
                              onReject: 'retry'|'pause'; }
interface RetrySpec     { maxAttempts: number; backoffSeconds: number[]; notifyThreshold: number; }
type HookRef = string;        // 'cmd:<命令模板>'；在 runner 上执行，非 0 退出 = 节点失败
```

**Validator 执行模型（H-5）**：`automated` 在 runner 上执行（可访问 worktree），`cmd` 形式，超时按失败；`evidence_json = { exitCode, stdoutTail, files[] }`。`ai_assisted` 由控制面派发执行器求值（结论仅参考，P3）。`human` = 审批卡片（D.5）。

### F.6 数据模型补充（并入 §16）

```sql
CREATE TABLE accounts  ( id TEXT PRIMARY KEY, email TEXT UNIQUE, password_hash TEXT,  -- argon2id
                         created_at TEXT );          -- desktop 首启创建单账号（H-10）
CREATE TABLE tokens    ( id TEXT PRIMARY KEY, kind TEXT,  -- 'session'|'runner_bootstrap'|'runner'
                         subject_id TEXT, token_hash TEXT UNIQUE,   -- SHA-256(不透明随机 256-bit)
                         expires_at TEXT, revoked_at TEXT, created_at TEXT );
CREATE TABLE leases    ( run_id TEXT PRIMARY KEY, holder TEXT,
                         fencing_token INTEGER NOT NULL, expires_at TEXT );
CREATE TABLE meta      ( runner_id TEXT, key TEXT, value_json TEXT,   -- runtime 检测结果等
                         PRIMARY KEY (runner_id, key) );
CREATE TABLE executors ( id TEXT PRIMARY KEY, instance_id TEXT, runtime TEXT,  -- §7.6 第二层
                         model_config_json TEXT, name TEXT );
```

**fencing 落地（C-7）**：`Scheduler.claim` 写 `leases`（`fencing_token` 单调递增）；`appendEvent(e, lease)` 对 run 域事件校验 `lease.fencing_token >= leases.fencing_token AND expires_at 未过`，否则拒绝写入。平台域事件由 API handler 单写者产生，不需租约。

### F.7 Auth / Token 规格（C-9）

- Token 一律为**不透明随机 256-bit**，库中只存 SHA-256 hash；比对走 `tokens` 表。
- **runner_bootstrap**：TTL 300s（§5.3.1）；确认后签发长期 **runner** token（轮换 = 新发旧废、24h 宽限期；吊销 = 置 `revoked_at`，连接即断）。
- **session**：登录签发；desktop 首启创建本地账号（密码必设，email 可后补），本机回环地址默认放行只读。
- **只读免 token 清单** = ClientAPI 的 `Get* / List* / Subscribe`；其余一律要求 `Authorization: Bearer`。

### F.8 执行器适配器目录契约（H-2）

```
.chaosplus/runs/{run_id}/{node_id}/{attempt}/
  context/            # 七层上下文物化（§18.1）：
  ├ 01_constitution.md  02_contract.json  03_inputs/（或 inputs.list 路径清单）
  ├ 04_feedback.json    05_governance/    06_skills/    07_runtime.md
  output/             # 执行器自由写作区（非产出判定依据）
  logs/agent.log
  continue.md         # 退出码 2 时由适配器写入，供下一 attempt 注入
  exit_code           # 单行整数，进程结束时适配器写入
```

- **产出判定**：引擎按 `outputSpec.produces` 的 path 扫描**该 execution 的 `worktree_path`**（文件存在 + checksum 变化 → `ARTIFACT_PRODUCED`；artifact 身份仍按逻辑 path，F.9）。**不采信执行器自报**（P1）。
- **worktree → Workspace 归集**：合并类 `human_approval` 节点（如 sprint-delivery）通过后，runner 将该 run 的 produces 归集到 Workspace——git 仓库 = 合并 worktree 分支；普通目录 = 覆盖拷贝；归集后更新 artifact checksum。项目未启用 worktree 并行（单执行流）时，worktree 即 Workspace 根，无需归集。
- **心跳**：适配器进程每 15s 经 runner 的 `Channel` 流上报（RPC，非文件轮询）。

### F.9 Artifact 身份与依赖（C-8 / X-4）

- **身份**：`artifact.id = hex(sha256(project_id + ':' + 逻辑id))[:16]`——项目内以逻辑 id（默认=path）稳定，**跨 run 复用同一身份**，stale 传播（§10.3）因此成立。
- `artifacts` 表的 `run_id/node_id/execution_id` = **最近一次产出**该 artifact 的执行；历史产出经 `events` 回溯。
- **`artifact_deps` 写入方 = 引擎**：节点完成时，为该节点每个 produces → 每个 consumes 建边；模板可经 `artifactSpecs` 追加显式依赖。建边时做环检测，成环即拒绝（保持 DAG）。
- **stale 出口（H-6）**：仅两条——① 产出节点重新执行成功且新产物通过验证 → valid；② 人工 `force_valid`。Reconciliation 扫描范围 = 活跃项目的全部 artifact。

### F.10 相似度与消息细则

- **相似度检测（H-7）**：比较对象 = 本 attempt 与上一 attempt 的 **produces 文件集**（路径并集），逐文件 SHA-256 判同异；`变化文件数 / 并集文件数 < 0.08` → `pause_for_human`。attempt=1 无基线，不触发。
- **消息 payload（H-9）**：`channel_messages.payload_json = { kind: 'text'|'workflow_event'|'approval_card', text?, mentions: memberId[], ref?: { run_id, node_id, execution_id } }`。@mention 由客户端拾取器解析为 memberId 落库（不存显示名，无歧义）；审批卡片经 `ref` 关联 node_execution。工作流事件在 UI 按 run 聚合折叠：同一 run 的连续 `workflow_event` 消息合并为一条可展开的摘要消息（显示最新事件 + 计数）；审批卡片与 `pause_for_human` 事件永不折叠。
