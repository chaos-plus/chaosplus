---
name: dev-engineering
description: Dev 全仓强制工程与产品门禁。任何产品功能、评审、设计、编码、重构、IAM、持久化、UI、文档、部署、提交或推送任务都必须自动使用；先确认真实用户需求与竞品证据，再读取统一规则和领域 Skill，执行检索复用、最终设计、编码和生产级交付检查。
---

# Dev 工程门禁

本 Skill 是全仓统一入口，不复制工程规范。规范唯一来源是 `.rules/`，领域流程由
`.claude/skills` 下的共享 Skill 维护；Codex 通过 `.agents/skills` 的符号链接发现它们。

1. 完整读取仓库 `AGENTS.md`、`.rules/3.BACKEND.md` 以及当前任务涉及的其他
   `.rules` 文件。
2. 涉及新增或改变产品功能时，完整读取 `.rules/3.PROTOTYPE.md` 的产品价值门禁与
   `.rules/4.PRD_TEMPLATE.md`。逐功能确认产品角色、真实问题、证据、价值指标和竞品结论；
   缺少已批准证据时先向用户提问并等待明确确认，严禁用编造内容进入设计或实现。
3. 运行 `python3 .claude/skills/dev-quality-gate/scripts/skill-runtime.py refresh`，
   读取生成的仓库事实，再完整读取匹配的领域 Skill：`dev-backend`、
   `dev-frontend`、`dev-docs`，以及最终验收所需的 `dev-quality-gate`。
4. 每次写入前把 `.rules/3.BACKEND.md` 的设计记录和全部不变量作为停止门禁。
   门禁未完成前只允许只读调查。
5. 涉及持久化时，额外完整读取
   [持久化契约](references/persistence-contract.md)。
6. 架构或 Schema 修改前后分别运行
   `scripts/check_architecture_contract.py` 和
   `scripts/check_schema_contract.py`。
7. 最终使用共享 `dev-quality-gate` 验收。严禁降低检查或声称未运行的门禁已通过。
   正式交付仍须全部适用门禁通过；用户明确要求保存未完成快照时，只可按
   `.rules/3.TEST.md` 创建并推送受控 `WIP:` 提交，不得用于合并、发布或就绪声明。

严禁仅为源码兼容而保留错误 Schema。已有生产数据时，必须设计显式兼容迁移、确定性回填、必要的双读写窗口和最终移除步骤；没有生产部署时直接修正原始迁移。严禁把自然字符串静默解释为雪花 ID。
