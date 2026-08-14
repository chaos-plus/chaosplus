---
name: dev-ui-ux
description: Dev Web 与移动端 UI/UX/UE 设计、实现和深度评审能力。涉及页面、组件、颜色、排版、布局、响应式、可访问性、交互、动画、数据可视化或前端体验优化时必须使用；支持 React、Next.js、Vue、Svelte、SwiftUI、React Native、Flutter、Tailwind 和 shadcn/ui。
---

# Dev UI/UX/UE

使用本 Skill 的本地设计数据库评审和实现专业、可访问、响应式的产品界面。

## 优先级

| 优先级 | 类别 | 要求 |
| --- | --- | --- |
| 1 | 可访问性 | 正文对比度至少 4.5:1、可见 focus、语义结构、键盘完整操作、表单 label、图片 alt |
| 2 | 触控与交互 | 触控目标至少 44x44px、异步防重复、错误靠近问题、click/tap 承担主交互 |
| 3 | 性能 | 图片响应式与延迟加载、预留异步空间、尊重 `prefers-reduced-motion` |
| 4 | 布局与响应式 | 无横向溢出、稳定尺寸、明确 z-index、移动端正文至少 16px |
| 5 | 排版与颜色 | 正文行高 1.5-1.75、合理行长、字体性格与产品一致 |
| 6 | 动效 | 微交互 150-300ms，优先 transform/opacity，不引起布局位移 |
| 7 | 风格 | 服从产品类型和既有 design system，跨页面一致，图标严禁 emoji |
| 8 | 图表 | 图型匹配数据关系，颜色可访问，重要数据提供 table 替代 |

## 必须流程

1. 明确产品类型、目标用户、行业、既有 design system、技术栈和核心工作流。
2. 先检查仓库现有 token、共享组件、icon 库、页面模式和 API 状态，不重复建设。
3. 必须先运行 design system 检索：

```bash
python3 .claude/skills/dev-ui-ux/scripts/search.py "<产品类型> <行业> <关键词>" --design-system -p "<项目名>"
```

4. 按需补充领域检索：

```bash
python3 .claude/skills/dev-ui-ux/scripts/search.py "<关键词>" --domain ux
python3 .claude/skills/dev-ui-ux/scripts/search.py "<关键词>" --domain chart
python3 .claude/skills/dev-ui-ux/scripts/search.py "<关键词>" --domain typography
python3 .claude/skills/dev-ui-ux/scripts/search.py "<关键词>" --stack react
```

5. 实现完整状态：loading、empty、error、authorization、validation、pending、success、retry、disabled、focus 和 responsive。
6. 复用现有 Lucide/shadcn/共享组件。熟悉动作使用图标，歧义动作使用图标加文字；禁止手绘已有图标。
7. 用真实浏览器在 375、768、1024、1440 宽度检查；涉及 canvas/3D 时同时做像素非空和交互验证。

## 产品设计规则

- SaaS、CRM、IAM 和运维工具保持安静、紧凑、可扫描，优先表格、分组表单和稳定导航；避免营销 hero、装饰卡片和大面积单色。
- 页面 section 不做漂浮卡片；卡片只用于重复实体、modal 和真正框定的工具；严禁 card 套 card。
- 圆角不超过 8px，除非既有 design system 明确要求。
- 二元设置用 switch/checkbox，模式用 segmented control，数值用 input/stepper/slider，选项集用 select/menu，视图切换用 tabs。
- 固定格式 board/grid/toolbar/button/counter 必须使用稳定尺寸、grid track 或 aspect ratio，动态内容不得导致位移。
- 文字不得与其他内容重叠；长词必须换行或自适应；严禁按 viewport width 缩放字号和负 letter-spacing。
- 不使用装饰 orb、bokeh blob、通篇单一色族、主导紫色渐变、米色/棕橙或深蓝灰单调主题。
- 时间在展示层按用户时区转换，空值、错误和权限限制使用清楚且本地化的反馈。
- 运维终端使用全高可调整工作区、稳定 toolbar 和清晰的连接/只读/退出状态；支持复制、搜索、字体缩放和 resize，但不得用装饰性的“黑客终端”视觉损害可读性。
- 节点市场优先搜索、分类、兼容性、已验证来源、权限能力、版本和安装状态；节点配置由 schema 生成紧凑表单，危险能力在安装与首次使用时明确确认。

## 交付检查

- 无 emoji 图标；图标集、尺寸和对齐一致。
- hover/focus/active/disabled 不造成布局移动。
- 明暗主题均满足对比度，border 和透明层均可见。
- 无 fixed navigation 遮挡、横向滚动、文字溢出、重叠或空白渲染。
- 交互可用键盘完成，screen reader 名称准确，颜色不是唯一信息载体。
- lint、typecheck、真实测试、生产 build 和浏览器截图检查全部通过。
