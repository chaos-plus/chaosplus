---
name: dev-frontend
description: 开发、评审、测试、优化和部署 Dev 管理前端。涉及 React、TypeScript、Bun workspace、共享 UI、API client、浏览器认证、响应式、可访问性、前端测试、Nginx 或容器时必须使用。
---

# Dev 前端开发

构建安静、高效、适合重复操作的管理应用，复用仓库现有 Bun、React、Vite 和共享 UI 体系。

## 从仓库事实开始

1. 运行 `python3 .claude/skills/dev-quality-gate/scripts/skill-runtime.py refresh`。
2. 完整读取 `../dev-quality-gate/references/repository-facts.md` 和 `references/lessons.md`。
3. 读取受影响 manifest、router、API client、相邻组件和真实后端 OpenAPI/source。
4. 复制来的工程只提供框架结构，不提供当前业务合同；发现残留假设必须删除。

## 保持前端边界

- deployable app、共享 UI、页面组合、业务组件和 typed API client 各归其位。
- 先复用现有 `@workspace/ui`、设计 token 和 Lucide，再评估新增依赖。
- 全应用只保留一个 request 实现；开发和生产浏览器调用统一经同源 `/api` proxy。
- `VITE_*` 是公开配置，严禁放 secret。
- 前端只消费后端已验证的 IAM 契约，不自创 tenant/entity/principal 可信来源。

## 完成真实工作流

- Cookie session、保护路由、return URL 校验、匿名/加载/错误态和 logout 必须完整。
- tenant/entity 上下文可见、稳定且不暗示跨租户访问。
- mutation 必须有 pending、success、validation、authorization、empty、retry 状态。
- 操作型界面优先紧凑表格和表单，避免营销页式装饰、嵌套卡片和过度圆角。
- 熟悉动作使用图标按钮，歧义动作带标签；具备可见 focus、语义 HTML、关联 label 和合理触控区域。
- 桌面与移动端文字不得溢出，固定格式控件使用稳定尺寸，加载状态不得引起布局跳动。

## 验证真实行为

- API client 测试使用真实 TCP listener。
- 认证、路由、Cookie、mutation 使用真实后端和浏览器。
- 禁止 mock、fake、stub、fixture interception 和测试专用应用分支。
- 同时验证失败 envelope 与非 2xx，不只验证成功 JSON。
- 检查桌面/移动截图的溢出、重叠、不可读、空白和 focus。
- lint warning 视为失败；构建后检查生产 bundle。

运行：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .claude/skills/dev-quality-gate/scripts/check-gates.ps1 -Scope frontend
```

发布前运行完整门禁。只有有复现和验证证据的通用失败才写入 `references/lessons.md`，严禁因单页偏好污染通用规则或降低检查。
