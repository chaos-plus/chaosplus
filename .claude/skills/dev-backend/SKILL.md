---
name: dev-backend
description: 开发、评审、测试和重构 Dev Go 后端与 IAM API。涉及 cmd、internal、pkg、Go module、SQL migration、OpenAPI、认证授权、多租户、数据库、运行时插件、部署启动或后端测试时必须使用。
---

# Dev 后端开发

完整读取 `.rules/3.BACKEND.md`。它是架构、DDD、持久化、IAM 和交付的唯一规范；本 Skill 只定义后端工作流程。

## 从仓库事实开始

1. 在仓库根运行 `python3 .claude/skills/dev-quality-gate/scripts/skill-runtime.py refresh`。
2. 完整读取 `../dev-quality-gate/references/repository-facts.md`。
3. 编辑前读取最近的生产文件、同名测试、相关架构文档和 `references/lessons.md`。
4. 合同以生产代码、迁移、配置和真实调用方为准，严禁根据旧部署文件、复制项目或测试名猜测。

## 保持边界

- `internal/app` 只负责组合、共享生命周期和基础设施注入，不放业务策略、迁移或 YAML。
- 功能放入归属明确的 `internal/modules/<module>`。状态机或生命周期不同的聚合必须拆为独立 leaf bounded context。
- 每个持久化 leaf module 自有 domain、service、repository、REST、migrate、三方言 SQL、i18n、生命周期和测试。
- 生产文件按领域职责命名，禁止 `bun_`、`huma_`、`goose_`、`sqlc_`、`http_` 等实现前缀。
- framework adapter 放 `internal/core/extension`；真正通用的库放 `pkg`。
- API DTO 与持久化模型在生命周期或暴露面不同时必须分离。
- 只有存在真实替代实现或能消除显著重复时才增加抽象。
- 认证、授权、凭据存储和租户隔离必须保留在可信编译代码中。

## 数据与 IAM

- 数据源沿用现有 `type` 加 `dsn` 或 `dsn_file` 契约，只支持 `sqlite`、`mysql`、`postgres`。
- 三方言迁移必须等价；多写不变量使用事务；查询参数化。
- tenant 是强制授权边界，entity 从属于 tenant，严禁削弱 tenant 隔离。
- 全局 principal、tenant membership、entity relationship 必须分开建模。
- 使用共享 `guid.ID`/Snowflake、Bun、Goose、UTC Unix 毫秒和完整审计字段。
- 密码、token、DSN、恢复材料和密钥严禁记录日志。
- OAuth/OIDC 使用精确 redirect、PKCE、一次性 code、refresh rotation、撤销和最小 claims。
- 保持 Huma OpenAPI 和共享响应契约；不得另建 HTTP/IAM/security 实现。

## 垂直完成

1. 定义或更新领域不变量。
2. 完成 repository 与三方言迁移。
3. 完成 service 编排、IAM 和审计。
4. 用稳定 operation ID、summary、tags、状态码和 schema 暴露完整 Huma operation。
5. 只在 composition root 接线。
6. 通过生产 constructor、真实 SQLite 或可达数据库、真实监听器验证。

测试不得用 mock、fake、stub、miniredis、monkey patch 或测试专用生产分支代替真实依赖。每个 `name_test.go` 必须验证同目录 `name.go`。

## 验收与学习

运行：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .claude/skills/dev-quality-gate/scripts/check-gates.ps1 -Scope backend
```

发布前加 `-Full`，覆盖率、race、vet、静态分析、漏洞、OpenAPI 和结构门禁全部通过后才能交付。

只有已复现、已修复且有测试证据的通用失败才可写入 lessons：

```text
python3 .claude/skills/dev-quality-gate/scripts/skill-runtime.py record --domain backend --symptom "..." --cause "..." --prevention "..." --evidence "..."
```

严禁记录猜测、秘密、任务流水账或一次性业务决策，严禁为了通过而降低门禁。
