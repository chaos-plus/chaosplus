---
name: dev-docs
description: 开发、重组、验证和发布 Dev 文档站与架构文档。涉及 README、Astro/Starlight、Mermaid、API/部署指南、导航、文档同步、链接检查或文档容器时必须使用。
---

# Dev 文档开发

维护可查找、可验证、可直接指导实现的文档系统。

## 建立唯一事实来源

1. 运行 `python3 .claude/skills/dev-quality-gate/scripts/skill-runtime.py refresh`。
2. 完整读取 `../dev-quality-gate/references/repository-facts.md` 和 `references/lessons.md`。
3. 根 README 和仓库指定的架构文档是工程事实来源，发布站只是呈现层。
4. 镜像页面使用既有同步脚本，严禁手工修改生成副本。
5. 所有声明必须由生产代码、测试、配置、迁移和 OpenAPI 证明。

## 编写可实施文档

- 明确目的、范围、非目标和当前实现状态。
- 先描述模块归属与依赖方向，再描述文件细节。
- 精确定义 principal、credential、session、tenant、membership、entity、role、permission、policy 和 business resource。
- 三个及以上组件交互时提供请求流或 Mermaid。
- 明确数据归属、事务边界、授权检查、错误、配置、迁移和可观测性。
- 提供真实仓库路径、interface shape、API 示例和测试位置。
- 严格区分已实现与目标架构，路线图功能不得写成已完成。
- 示例使用明显虚构值，严禁 secret 和真实 DSN。

## 维护发布站

- 导航按任务组织：概览、架构、后端、IAM、运维、质量。
- 首页首先是文档，不做营销 landing page。
- 保持搜索、无障碍导航、Mermaid、响应式和生产构建。
- 结构数据使用 parser 和确定性脚本，生成文件仅在内容变化时写入。
- 删除复制项目专用检查，并替换为当前仓库需要的 build、link、structure 和 protected-build 门禁。

运行：

```text
python3 .claude/skills/dev-quality-gate/scripts/check_gates.py --scope docs
```

Windows 使用 `py -3` 启动同一脚本。布局变化时检查桌面/移动首页和至少一篇长架构文档。只有有证据的通用文档失败才写入 lessons，变化中的实现事实由 refresh 生成。
