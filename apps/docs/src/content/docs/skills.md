---
title: 开发技能与质量闸门
description: Chaosplus 仓库级技能路由、自主学习边界和验收流程
---

仓库在 `.claude/skills` 下维护四个技能。全局技能负责识别变更范围、刷新仓库事实、调度领域技能并执行验收；领域技能只约束自己负责的代码面。

## 自动调度

| 变更路径 | 技能 | 主要责任 |
| --- | --- | --- |
| `apps/server`（`cmd`、`internal`、`pkg`）、Go 与数据库文件 | `dev-backend` | 模块边界、三数据库、IAM 安全、真实测试 |
| `apps/admin` | `dev-frontend` | React 工作区、API 客户端、浏览器流程、可访问性 |
| `docs`、`README.md` | `dev-docs` | 工程文档、同步、链接、构建 |
| `.agents`、`.github`、跨域发布 | `dev-quality-gate` | 路由、结构规则、全量验收 |

跨域契约先检查生产者，再检查消费者。例如后端 OpenAPI 先于前端客户端和对外文档。

## 受控自主学习

自主更新只有两条路径：

1. `skill-runtime.py refresh` 从清单和源码目录原子重建稳定的仓库事实；内容未变化时不写文件。
2. `skill-runtime.py record` 在故障已复现、修复已通过证明测试后，以并发锁保护方式追加现象、根因、预防和证据。

学习脚本不会自动修改技能正文、架构决策、安全规则、依赖策略、排除项、覆盖率阈值或闸门脚本。此类变化必须按普通代码变更评审并通过相关闸门。

## 执行闸门

按当前 Git 变更自动选择范围：

```bash
python3 .claude/skills/dev-quality-gate/scripts/check_gates.py
```

发布前执行全量闸门：

```bash
python3 .claude/skills/dev-quality-gate/scripts/check_gates.py --scope all --full
```

同一 Python 标准库脚本支持 Windows、macOS 和 Linux；Windows 使用 `py -3`。
缺失的 Go 静态工具由门禁按固定版本自动安装。完整验收要求真实 Go 覆盖率至少
90%，并明确报告尚未实际连接的数据库、浏览器或外部运行环境。
