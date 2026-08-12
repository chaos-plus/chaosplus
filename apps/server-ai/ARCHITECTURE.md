# server-ai 架构

本文记录当前实现与目标边界，按 `apps/server` 的 DDD/module 规范描述。
规范唯一来源是仓库根 `AGENTS.md` 与 `.rules/`。

## 组合根与依赖方向

`cmd/server-ai/main.go` 是唯一组合根：负责配置、数据库、NATS、模块构造、
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
| Workspace（需求/任务/目标/附件） | `internal/modules/workspace/{requirement,task,objective,attachment}` | 每个子聚合为独立 leaf module，自有 domain/service/repository/rest/migrate/三方言 SQL/i18n |
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
- 生产与开发 NATS 使用固定官方镜像；仓库内不存在内嵌 NATS daemon（`cmd/nats` 已移除）。
- Runner 与工作流之间的命令/事件传输使用 NATS；REST/OpenAPI 使用 Huma 按 module 注册。
- 目录/预览隧道：`TunnelProvider` port + FRP adapter 属于独立的 Tunnel bounded context（PRD §5.4）；生产只使用官方 `frps`/`frpc`，禁止内嵌或自研隧道协议，公网流量必须经过 IAM-aware Gateway。

## 交付状态

- 已实现：Workspace（requirement/task/objective/attachment）、Workflow/run、Machine runner、Artifact、Conversation（channel/message/agent）、共享应用组合、官方 NATS 镜像部署说明。
- 计划中：Runner Preview Gateway（FRP 穿透，PRD §5.4）、随机唯一域名、过期/撤销/审计/限流与分享凭据。
- 禁止：共享业务 `internal/store`、中央 feature handler、sqlc、RSQL、重复 IAM/security/GUID 实现、内嵌基础设施 daemon。
