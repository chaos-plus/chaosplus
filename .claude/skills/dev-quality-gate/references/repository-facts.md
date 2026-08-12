# Dev 仓库事实

本文件由仓库 manifest 和 source tree 自动生成，严禁手工编辑。

## 后端

- Go module：`github.com/chaos-plus/chaosplus`
- Go 版本：`1.26.5`
- Go package 目录数：60
- 功能模块：audit, authn, federation, governance, iam, identity, oauth, organization, provisioning
- IAM SQL 方言：mysql, postgres, sqlite
- HTTP 框架：基于 chi 的 Huma v2
- 持久化：Bun + Goose
- 主配置：`apps/server/internal/app/config.go`
- 组合根：`apps/server/internal/app`

## 前端

- Workspace：`chaosplus-admin-ai`
- 包管理器：`bun@1.3.12`
- Workspaces：apps/*, packages/*
- 应用：`apps/admin-ai/apps/platform`（React、Vite、TypeScript）
- 共享 UI：`apps/admin/packages/ui`
- 应用测试命令：`bun test src`

## 文档

- Package：`dev-docs`
- 站点：`apps/docs`
- 生成器：Astro ^7.0.2
- 主题：Starlight ^0.41.3
- 权威工程来源：`README.md` 和 `apps/docs/*.md`

## 强制不变量

- SQLite、MySQL、PostgreSQL 数据库配置使用 `type` 加 `dsn` 或 `dsn_file`。
- `internal/app` 下严禁 YAML。
- 每个 Go `name_test.go` 必须有同目录 `name.go`。
- 测试使用真实依赖和真实 listener；严禁 mock、fake、stub 和 miniredis。
- Go 完整验收覆盖率至少 90%。
- 业务层级为 tenant -> entity -> business resources。
