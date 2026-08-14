# server-ai 架构

本文记录当前实现与目标边界，按 `apps/server` 的 DDD/module 规范描述。
规范唯一来源是仓库根 `AGENTS.md` 与 `.rules/`。

## 组合根与依赖方向

`cmd/server-ai/main.go` 是唯一组合根：负责配置、数据库、内部消息 adapter、模块构造、
HTTP/Huma 生命周期与优雅退出。跨领域基础设施 client 只允许在组合根创建并注入。

```text
cmd/server-ai (composition root)
  -> internal/app        (共享应用组合：Huma、Bun/Goose、IAM 扩展、生命周期)
  -> internal/modules/*  (bounded contexts)
       rest.go -> service -> domain -> repository/ports
  -> internal/infra/*    (跨领域基础设施适配器：runnergateway 等)
```

`internal/app` 只做组合与接线，不放业务策略、迁移或 YAML。
`internal/infra` 只放真正通用的基础设施适配器；业务代码全部在 `internal/modules`。

## Bounded contexts

| Context | 位置 | 状态 |
|---|---|---|
| Workspace（目标/需求/任务/测试/缺陷/附件） | `internal/modules/workspace/{objective,requirement,task,testcase,testrun,defect,attachment}` | 每个子聚合为独立 leaf module，自有 domain/service/repository/rest/migrate/三方言 SQL/i18n |
| Workflow / run | `internal/modules/workflow` | 领域类型、引擎、执行器、REST、迁移与测试同属该 leaf module |
| Machine runner | `internal/modules/machine` | Hub/领域行为、token、REST 与迁移同属该 module |
| Artifact | `internal/modules/artifact` | 领域、REST、迁移、测试同属该 module |
| Conversation（channel/message/agent） | `internal/modules/conversation/{channel,message,agent}` | 每个子聚合独立 leaf module |

## 持久化

- Goose 负责迁移，Bun 负责 ORM/查询/事务；`*bun.DB` 由 `internal/app` 创建并注入。
- 每个持久化 leaf module 内嵌 `sql/sqlite`、`sql/mysql`、`sql/postgres` 等价迁移，维护自己的 Goose version table。
- 所有内部实体/事件 ID 与 FK 使用 `apps/server/internal/infra/guid.ID`（Snowflake，BIGINT），JSON/HTTP/NATS 边界编码为十进制字符串。
- 时间统一 UTC Unix 毫秒 `BIGINT`；展示层才转换时区。
- 可变聚合带 `tenant_id`/`entity_id`/`owner_id`/`created_at`/`created_by`/`updated_at`/`updated_by`/`deleted_at`/`deleted_by`/`version`。

## IAM 与基础设施

- 认证/授权/会话/IAM 全部由 `apps/server` 拥有；server-ai 只消费可信身份结果，不重复实现 authn/authz/security/GUID。
- runner 唯一 wire protocol 是 authenticated WebSocket；`apps/runner` 不包含 NATS/EMQX SDK、broker 地址、subject/topic 或相关启动参数。
- Workflow 只消费 `RunnerLink` port，machine Hub 只消费 machine-owned `ClusterTransport`，runner 与公开 API 不感知内部 provider。NATS subject、JSON 编解码和 client 全部位于 `internal/infra/runnertransport`，由组合根注入；仓库内不存在生产内嵌 NATS daemon。
- 目录/预览隧道：`TunnelProvider` port + FRP adapter 属于独立的 Tunnel bounded context（PRD §5.4）；生产只使用官方 `frps`/`frpc`，禁止内嵌或自研隧道协议，公网流量必须经过 IAM-aware Gateway。

### 集群连接不变量

- runner 连接实例 A、workflow 在实例 B 调度时，tenant/entity scope 与 runtime inventory 必须在任意实例一致。
- 同一 machine 只能持有一个未过期 connection lease；每次接管递增 fencing token，旧连接、旧订阅和旧事件必须被确定性拒绝。
- onboarding token hash、在线目录、runtime inventory、lease/fencing 已由 machine module 的共享 Bun repository 管理，进程内 map 只保存本实例 WebSocket handle 与进行中的请求。
- Hub 在数据库中获取、续租和释放 route lease；接管递增 fencing token。Gateway 在 dispatch、event 和 reply 边界复核活动 route，拒绝 stale、乱序和 takeover 后的旧 reply。
- 本地 SQLite migration lifecycle、真实 listener 与 focused race 已通过；固定官方 NATS 双连接/双控制面实例、live MySQL/PostgreSQL 和故障恢复证据尚未在当前环境运行，因此不能声明 Phase R 或生产集群验收完成。

## 交付状态

- 已实现：Workspace（objective/requirement/task/testcase/testrun/defect/attachment）、Workflow/run、Machine runner authenticated WebSocket、共享 route/lease/fencing 与中立 cluster transport port、Artifact、Conversation（channel/message/agent）、共享应用组合、官方 NATS 内部 adapter 部署说明。
- 已验证但未完成发布验收：Workspace SQLite up/down/re-up 与真实 Huma/TCP 追溯链；machine/gateway SQLite 与 race。MySQL/PostgreSQL、官方 NATS 双实例和真实 IAM 浏览器四视口证据仍待外部环境执行。
- 计划中：Runner Preview Gateway（FRP 穿透，PRD §5.4）、随机唯一域名、过期/撤销/审计/限流与分享凭据。
- 禁止：共享业务 `internal/store`、中央 feature handler、sqlc、RSQL、重复 IAM/security/GUID 实现、内嵌基础设施 daemon。
