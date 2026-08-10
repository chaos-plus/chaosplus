# Machine Runner 接入 + runner WS 链路 + CLI 自动检测 设计

日期:2026-08-08
状态:Draft
参照:PRD v2.0 (定稿) §5.3 / §5.3.1 / §17.2 / D.2 / §15.3;上一轮 spec `2026-08-08-mvp-closure-design.md`

## 1. 目标

把 runner(执行面)↔ 控制面的传输从 **NATS 直连** 改为 **WS 长连 + token 认证**,NATS 彻底内部化;实现 PRD §5.3.1 的 **Machine 接入流程**(5 分钟一次性 token → 确认转长期 → 心跳/重连);runner 的 claude/codex 运行时 **自动检测**。外部只暴露控制面一个 HTTP/WS 端口。

## 2. 传输(对齐 J3/C3)

```
runner:  bun run src/serve.ts --server <url> --token <token> [--name <name>]
控制面:  WS runner hub — 握手带 token 认证 → runnerID↔conn 映射
         → 下发 spawn/kill/read-file/run-cmd → 回收事件 + 15s 心跳
NATS:    仅控制面内部引擎事件 fan-out(chaos.run.*);runner 链路 NATS gateway 移除
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
- 新 `src/machine/client.ts`:WS 客户端 —— 握手带 token 注册 machine(name/OS/检测到的运行时),此后收命令、发事件、15s 心跳。取代 `src/nats/transport.ts` 作为 serve 的传输。
- CLI 自动检测(`src/backends/claude.ts`/`codex.ts`):PATH 探测 `claude`/`codex` 可执行文件;claude 用本地 cc-switch 配置(`~/.cc-switch`),codex 用 `~/.codex`;env(`CLAUDE_BINARY` 等)仅 override。

### 4.2 控制面(Go)
- **WS runner hub**(`internal/machine/`):接受 runner WS 连接,握手校验 token;`runnerID↔conn` 映射;路由 spawn/kill/read-file/run-cmd;回收事件 + 心跳;断线标记离线。
- **Token 服务**(`internal/machine/`):签发一次性接入 token(5min 内存态);校验(pending 一次性 / 长期);confirm→长期化;cancel/timeout→失效。
- **machine store**:`machine_runners (id, instance_id, address, status, last_heartbeat_at, token_hash)` 迁移;`pending` 态在内存/表里区分,confirm 后入库。
- **RunnerExecutor 换 WS-backed**:现有 `RunnerExecutor` 依赖 `*gateway.Gateway`(NATS)。抽象 `RunnerLink` 接口(SpawnAndWait/ReadArtifact/RunCmd/Kill/RegisteredRunners),RunnerExecutor 走接口;本特性实现 WS-backed(经 hub),NATS 实现保留但 runner 链路默认 WS。
- 引擎/RunManager/HTTP/UI(上一轮)不动。

### 4.3 UI(嵌入页)
- machines 区:添加向导(命令区+5 分钟倒计时+确认/取消/刷新)、列表(id/名称/在线状态/最近心跳/托管 agent 数)、详情(关键信息、运行时检测、agent 列表 [agent 列表 v1 可先展示占位])。
- 确认/取消走 REST:`POST /api/machines/{id}/confirm`、`DELETE /api/machines/{id}`(cancel)。刷新命令:`POST /api/machines/{id}/refresh-token`。

## 5. 存储/表
- 迁移:`machine_runners(id, instance_id, address, status, last_heartbeat_at, token_hash)`(PRD §16 表 + token_hash 列存长期 token 摘要,§17.2)。
- 一次性 token:内存态,`expiresAt = now+300s`。

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

## 7. 错误处理 / 测试 / 验收
- token 无效/过期 → WS 握手 401 拒绝;确认未连接不可点;重复确认幂等。
- 测试:runner WS client↔hub 握手、spawn 往返、心跳;token 签发/校验/过期/长期化;machine store CRUD + 状态机;RunnerExecutor WS 路径(mock)。
- 验收:控制面 UI 添加 machine → 倒计时内 `bun run src/serve.ts --server http://127.0.0.1:8081 --token <t>` 连上 → 确认 → 列表在线 → 发起 workflow run(上一轮闭环)走 WS 链路真实 claude 执行 → 审批通过。

## 8. 范围边界
- 本特性只做接入链路 + token + machine 列表/向导;machine 详情里「托管 agent/数字人」列表留到下一轮(依赖数字人模型)。
- NATS runner 链路代码保留(不删),默认切换到 WS。
- 会话 Bearer token(§17.2 面向状态变更命令的 auth)不在本特性 —— 本轮只有 runner 接入 token。
