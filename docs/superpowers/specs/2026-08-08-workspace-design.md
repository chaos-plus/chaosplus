# 工作区(协作区)设计 —— 需求/任务/测试/缺陷/OKR + 文件 + 走工作流

日期:2026-08-08
状态:Draft(待 architect 评审)
参照:PRD C5(协作区 = 工作流 artifact 视图)、附录 A、§9(群聊订阅)、§13、§23.A;spec `2026-08-08-mvp-closure-design.md`

## 1. 定位(对齐 PRD C5)

**工作区 = 工作流 artifact 的视图,不新造执行引擎。** 需求/任务/缺陷/测试是工作项(work item),它们的**执行统一走既有 DAG 工作流引擎**(RunManager/Engine/真实 runner)。群聊是沟通层(快速提需求/评审/确认),工作项变化订阅回群聊。

## 2. 数据模型(全部 DB 持久化,控制面 = 权威)

### work_items(扩展现有)
```
id, type(requirement|task|test|bug), title, description(支持文件引用),
status(open|in_progress|review|done), parent_id(子任务,可多级),
estimate_hours(校准工时), spent_hours(已耗,由 run 时长累计), progress(0-100,自动),
workflow_run_id(最近一次执行 run), channel_id(订阅频道), assignee_agent,
created_at, updated_at
```

### attachments(文件/图片/视频,聊天与描述共用)
```
id, work_item_id 或 message_id, filename, mime, size_bytes,
store_path(磁盘相对路径,控制面管理的 artifact 存储), created_at
```
- 上传:multipart → 存控制面 artifact 目录(env `ARTIFACT_ROOT`);下载/预览:GET /api/attachments/:id(按 mime 返回)。
- 聊天引用:消息 payload 可带 `attachments:[{id,filename}]`;工作项描述引用同。

### okrs
```
id, title, objective, period, key_results(JSON:[{title, target, progress, unit}]),
overall_progress(自动=各 KR 进度均值), created_at, updated_at
```

### 执行进度/工时
- `progress` 由工作项关联的 workflow run 状态自动推导:run 节点 completed 比例 → progress;run done → status done。
- `spent_hours` = 关联 run 的累计执行时长(控制面记录 run 起止)。
- `estimate_hours` 人工设置(校准),可随 run 实际耗时调整(记录校准历史可选)。

## 3. API(控制面,Go)

```
GET/POST /api/work-items?type=&status=&parent=  列表/创建(含子任务 parent_id)
GET/PUT/DELETE /api/work-items/:id
POST /api/work-items/:id/execute               → 用工作项上下文发起 workflow run(返回 runId)
POST /api/work-items/:id/attachments           multipart 上传 → attachment
GET /api/attachments/:id                       → 文件流(按 mime)
GET/POST/PUT/DELETE /api/okrs
GET/PUT /api/work-items/:id/progress           → 校准 estimate/查看自动 progress
```
- `execute` 路由:任务 → 构造 WorkflowDef(内置 task-execution 模板:trigger→agent(按任务描述执行)→[test 时跑 validator]→完成)→ RunManager.Launch → 关联 workflow_run_id。
- run 事件(OnEvent)→ 更新 work_item 的 progress/status/spent_hours → 群聊通知(若 channel_id)。

## 4. 前端(platform,ui-ux-pro-max 打磨)

- 左侧二级菜单(工作区下):**需求 / 任务 / 测试 / 缺陷 / OKR**。
- 各管理页:列表(类型/状态筛选、子任务树)、详情(Dialog/抽屉:描述+附件上传/预览+工时+进度+「执行」按钮→工作流)+ 新建/编辑(弹窗)。
- OKR 页:目标 + KR 进度条。
- 群聊:消息支持附件(上传/预览);工作项变化通知已实现(§9 订阅)。
- 操作体验:加载态/空态/焦点/响应式,遵循 ui-ux-pro-max 设计系统。

## 5. 后端规范

- Go:gofmt/goimports、`-race`、表驱动测试、**覆盖率 ≥80%**(work-item/attachment/okr/execute 路径)。
- daemon(TS):测试覆盖率同后端要求(bun test,补 backends/transport 覆盖率)。
- 无 mock 交付:真实 claude 执行 + 真实验收。

## 6. 验收(真实,严禁 mock)

1. 工作区建需求 → 关联频道 → 群聊收到订阅通知。
2. 任务建子任务、设 estimate → 点「执行」→ 真实 claude 走 DAG → 进度自动更新 → done → 群聊通知。
3. 上传图片/文件到任务 → 详情/群聊可预览;描述引用附件。
4. OKR 建目标 + KR → 进度自动汇总。
5. 后端 `go test -cover` ≥80%;daemon `bun test` 覆盖率达标。

## 7. 待评审点

- work_items 是否复用既有(已建)表还是新表(扩展列 vs 迁移 00005)。
- 附件存储:控制面磁盘目录 vs 复用 workspace artifact 存储(PRD ArtifactStore)。
- 任务→工作流模板:内置 task-execution 模板 vs 复用 software-dev-agile;测试节点 validator 接入。
- 工时/进度推导的准确性(以 run 事件为准)。
- 前端菜单结构(工作区二级:需求/任务/测试/缺陷/OKR)是否按 PRD §30 布局。
