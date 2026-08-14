---
name: dev-quality-gate
description: 为 Dev 仓库任务选择正确领域 Skill 并执行全仓质量门禁。任何实现、重构、评审、测试、发布、架构、API、前端、文档、部署、Skill 修改或跨领域验收都必须使用。
---

# Dev 质量门禁

负责全仓路由和最终验收。领域 Skill 指导实现，本 Skill 决定必须使用哪些领域流程，并拒绝无证据的质量声明。

规范唯一来源是 `.rules/`。决定变化时先更新 `.rules/`，再更新必要的 lessons 与可执行检查；各 Skill 严禁复制一份平行规范。

## 首先刷新上下文

运行：

```text
python3 .claude/skills/dev-quality-gate/scripts/skill-runtime.py refresh
```

完整读取 `references/repository-facts.md`。该文件由真实 manifest 和 source tree 生成，只在内容变化时写入，严禁手工添加临时事实。

## 按变更面路由

| 变更面 | 必须使用的 Skill |
| --- | --- |
| `cmd/**`、`internal/**`、`pkg/**`、Go module、SQL、后端部署 | `$dev-backend` |
| 管理前端、共享 UI、browser proxy/container | `$dev-frontend` 与 `$dev-ui-ux` |
| 文档站、README、架构/API/部署文档 | `$dev-docs` |
| `.claude/**`、`.agents/**`、`AGENTS.md`、`.rules/**`、跨领域发布 | `$dev-engineering`、`$dev-quality-gate` 和全部受影响领域 Skill |

合同跨域时先检查 producer，再检查 consumers，例如先确认后端 OpenAPI，再修改前端 client 和公开文档。

## 强制门禁

- 保留用户已有修改，遵循仓库模式。
- 拒绝 `internal/app` 下 YAML、无同名生产文件的 Go 测试、任何产品代码或测试中的 mock/fake/stub 等内部测试替代物，以及生产 Go 文件对 `miniredis` 的引用；只有隔离 `*_test.go` 可用 `miniredis` 做外部 Redis 的本地快速反馈，且不能替代真实 Redis 发布验收。
- 共享持久化要求 SQLite、MySQL、PostgreSQL 等价；明确哪些 live dialect 实际运行过。
- 完整验收要求真实 Go coverage 不低于 90%。
- OpenAPI operation ID、summary、tags、响应、认证、tenant/entity 授权必须准确。
- 前端必须通过 lint、typecheck、真实测试、生产构建、响应式浏览器检查和部署检查。
- 文档必须通过同步、内部链接、导航、Mermaid 和生产构建。
- 远程 Shell 必须在真实 Windows ConPTY、macOS/Linux PTY、真实 runner 与浏览器上验证；workflow 节点包必须验证签名/digest/SBOM、能力拒绝、版本 pin、撤销与真实执行，不能用内置替代节点验收。
- 严禁声称未运行的功能、数据库、workflow、浏览器或部署已通过。

工作中运行 path-aware gate：

```text
python3 .claude/skills/dev-quality-gate/scripts/check_gates.py
```

发布或生产就绪声明前运行：

```text
python3 .claude/skills/dev-quality-gate/scripts/check_gates.py --scope all --full
```

脚本只使用 Python 标准库并支持 Windows、macOS 和 Linux；Windows 可用
`py -3` 替代 `python3`。后端静态工具缺失或版本不符时，门禁按固定版本自动
安装 `staticcheck`、`golangci-lint` 和 Full 模式所需的 `govulncheck`。

严禁为了通过而删除检查、排除生产 package、压制 warning、缩小 coverage 或降低阈值。

## 受控 WIP 快照

完整门禁失败时默认禁止提交和推送。只有用户明确要求保存或共享当前未完成工作，
才执行 `.rules/3.TEST.md` 的 WIP 例外：先通过不可豁免检查，确认非保护、非默认、
非 release 开发分支，再使用 `WIP:` 标题和包含精确 `Failed-Gates`、`Unrun-Gates`
及 not-ready 声明的正文提交。不得 force push、tag、release、自动合并、降低门禁，
也不得把 WIP 状态描述为验收通过。

## 受控学习

只有失败已复现、修复已验证且规则可复用时，才运行 `skill-runtime.py record`。记录必须包含可观察 symptom、已确认 cause、通用 prevention 和验证 evidence。

严禁自动修改 `SKILL.md`、门禁脚本、安全不变量、阈值、架构决策或依赖策略；这些必须作为正常代码变更接受完整评审和门禁。

## 以证据结束

报告运行过的命令、结果、声明 coverage 时的精确比例、未测试外部系统和剩余缺口。局部通过只能作为局部证据，不能作为全量验收。
