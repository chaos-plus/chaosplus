# Machine Runner 接入 + runner WS 链路 + CLI 自动检测 设计

日期:2026-08-08
状态:集群实现完成；外部多实例验收待执行（2026-08-14）
参照:PRD v2.0 (定稿) §5.3 / §5.3.1 / §17.2 / D.2 / §15.3;上一轮 spec `2026-08-08-mvp-closure-design.md`

## 1. 目标

把 runner(执行面)↔ 控制面的传输从 **NATS 直连** 改为 **WS 长连 + token 认证**,NATS 彻底内部化;实现 PRD §5.3.1 的 **Machine 接入流程**(5 分钟一次性 token → 确认转长期 → 心跳/重连);runner 的 claude/codex 运行时 **自动检测**。外部只暴露控制面一个 HTTP/WS 端口。WebSocket 是版本化 runner wire protocol；“协议中立”指 workflow/machine 业务只依赖 transport-neutral port，内部可使用 NATS、MQTT 或其他满足合同的 adapter，而不是让 runner 感知或配置具体 broker。

## 2. 传输(对齐 J3/C3)

```
runner:  bun run src/serve.ts --server <url> --token <token> [--name <name>]
控制面:  WS runner hub — 握手带 token 认证 → runnerID↔conn 映射
         → 下发 spawn/kill/read-file/run-cmd → 回收事件 + 15s 心跳
内部总线: 仅控制面实例间路由；当前 NATS adapter 不进入 runner 协议或配置
CLI 自动检测:claude/codex 走 PATH 探测 + 本地配置(cc-switch / ~/.codex);env 仅 override
```

- runner 不再知道 NATS;`--server` 缺省 `http://127.0.0.1:8081`(本地 dev)。
- 命令协议沿用现有 runner 命令集(spawn/kill/switch-provider/read-file/run-cmd),载体从 NATS 改 WS。
- 事件流沿用现有 RunnerEvent(spawn-started/event/done/error/heartbeat)与 RunEvent,经 WS 回传。

## 3. 接入流程(§5.3.1 + D.2 状态机,原样)

```
UI 添加 machine → 签发一次性接入 token(5min,格式 xxxx.xxxx.xxxxxxxxx)
runner --token 连 WS → machine=pending,UI 倒计时
├─ 确认 → token 转长期、写 machine_runners、进列表、断线可重连
├─ 取消 → 关连接 + token 立即失效
└─ 超时(300s)/异常 → 自动断开 + token 失效 + 「刷新命令」
未确认临时 machine 不出现在列表
```

- 心跳 15s(§15.3)→ 列表在线/离线。
- 执行器 Agent 环境**不注入 token**(§17.2)。

## 4. 组件

### 4.1 runner(TS,`apps/runner`)
- CLI:`--server <url>`(默认 http://127.0.0.1:8081)、`--token <token>`(必填)、`--name <machine名>`(默认 hostname)。移除 `DAEMON_NATS_URL` 路径。
- `src/machine/client.ts`:WS 客户端 —— 握手带 token 注册 machine(name/OS/检测到的运行时),此后收命令、发事件、15s 心跳；`src/machine/protocol.ts` 独立拥有中立 command/event contract。
- CLI 自动检测(`src/backends/claude.ts`/`codex.ts`):PATH 探测 `claude`/`codex` 可执行文件;claude 用本地 cc-switch 配置(`~/.cc-switch`),codex 用 `~/.codex`;env(`CLAUDE_BINARY` 等)仅 override。

### 4.2 控制面(Go)
- **WS runner hub**(`internal/modules/machine`):接受 runner WS 连接,握手校验 token;`runnerID↔conn` 映射;路由 spawn/kill/read-file/run-cmd;回收事件 + 心跳;断线标记离线。
- **Token 服务**(`internal/modules/machine`):签发一次性接入 token(5min);只存 hash;校验 pending 一次性/长期 token;confirm→长期化;cancel/timeout→失效。production composition 使用共享 Bun repository 保存 pending token/scope/expiry。
- **machine store**:`machine_runners` 保存 confirmed machine 和长期 token hash；同一 owner 的 `machine_onboarding_tokens` 与 `machine_connection_leases` 保存 pending onboarding、活动 route、runtime/scope、connection lease 与 fencing token。进程内状态只缓存本实例 socket handle。
- **RunnerExecutor 依赖中立 port**:`RunnerExecutor` 只依赖 `RunnerLink`(SpawnAndWait/ReadArtifact/RunCmd/Kill/RegisteredRunners)；`GatewayRunnerLink` 适配控制面内部 gateway，runner 始终只使用 WS。
- **集群目录与围栏**:任意实例从 machine 共享目录解析活动 route/runtime/scope；同一 machine 单活动 connection lease，接管递增 fencing token，旧连接、订阅、reply 和 event 全部拒绝。进程内 `conns` 只缓存 socket，不能作为在线真相。
- 引擎/RunManager/HTTP/UI(上一轮)不动。

### 4.3 UI(嵌入页)
- machines 区:添加向导(命令区+5 分钟倒计时+确认/取消/刷新)、列表(id/名称/在线状态/最近心跳/托管 agent 数)、详情(关键信息、运行时检测、agent 列表 [agent 列表 v1 可先展示占位])。
- 确认/取消走 REST:`POST /api/machines/{id}/confirm`、`DELETE /api/machines/{id}`(cancel)。刷新命令:`POST /api/machines/{id}/refresh-token`。

## 5. 存储/表
- 当前 baseline:`machine_runners(id, tenant_id, entity_id, owner_id, address, status, last_heartbeat_at, token_hash, ...)` 保存 confirmed machine 与长期 token 摘要。
- Phase R 实现态:machine module 的共享三方言 repository 保存 pending onboarding token hash/scope/expiry、活动实例 route、runtime inventory、connection lease expiry 和单调递增 fencing token；不保存 token 明文。
- 实例本地内存只保存无法共享的 WebSocket handle；重启、跨实例 confirm、调度和接管均从共享 owner 恢复，不能依赖 sticky session。

## 6. API(控制面新增)
```
POST /api/machines/tokens           → 签发一次性 token {token, expiresIn:300, machineId}
GET  /api/machines/ws?token=...     → WS upgrade(runner 接入,握手即注册)
POST /api/machines/{id}/confirm     → 确认 → 长期化
DELETE /api/machines/{id}           → 取消 → token 失效 + 断开
POST /api/machines/{id}/refresh-token → 重新签发一次性 token(倒计时重置)
GET  /api/machines                  → 列表(已确认 machine)
```
- runner 事件走同一 WS 连接回传(不另开通道)。
- runner 连实例 A、workflow/API 命中实例 B 时必须仍可调度；禁止要求 sticky session 或暴露内部 broker。

## 7. 错误处理 / 测试 / 验收
- token 无效/过期 → WS 握手 401 拒绝;确认未连接不可点;重复确认幂等。
- 测试:启动真实 runner、WS hub 和 listener，验证握手、spawn 往返、心跳、token 签发/校验/过期/长期化、machine store CRUD/状态机与 RunnerExecutor 完整 WS 路径；禁止内部协议模拟器。
- 验收:控制面 UI 添加 machine → 倒计时内 `bun run src/serve.ts --server http://127.0.0.1:8081 --token <t>` 连上 → 确认 → 列表在线 → 发起 workflow run(上一轮闭环)走 WS 链路真实 claude 执行 → 审批通过。

## 8. 范围边界
- 本特性只做接入链路 + token + machine 列表/向导;machine 详情里「托管 agent/数字人」列表留到下一轮(依赖数字人模型)。
- runner 侧 NATS 链路、SDK 和配置全部删除；控制面内部 adapter 可替换但不属于 runner 产品合同。
- 多实例 token、在线目录和 connection lease/fencing 已落地并通过 SQLite/focused race；固定官方 NATS 双实例、live MySQL/PostgreSQL、崩溃接管和真实 runner@A → workflow@B 尚未在当前环境验收，因此仍不得声明 cluster-ready。
- 会话 Bearer token(§17.2 面向状态变更命令的 auth)不在本特性 —— 本轮只有 runner 接入 token。
