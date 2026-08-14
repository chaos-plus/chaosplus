# chaos.plus v2.1 决策记录

| 编号 | 标题 | 状态 | 影响范围 | 日期 |
| --- | --- | --- | --- | --- |
| ADR-01 | Runner WebSocket 的共享连接目录与中立集群路由 | 已采纳 | 后续版本起 | 2026-08-14 |

## ADR-01 Runner WebSocket 的共享连接目录与中立集群路由

> 日期：2026-08-14 | 状态：已采纳 | 影响范围：后续版本起

**背景与问题**：Runner 只应连接认证 WebSocket，不应知道控制面内部使用 NATS、MQTT 或其他消息系统。现有 Hub 直接订阅 NATS subject，并把 onboarding token、在线目录、runtime 与 OS 保存于单进程内存；多实例时，工作流无法可靠定位持有 socket 的实例，也无法拒绝崩溃实例、旧连接和乱序事件。

**备选方案与权衡**：

| 方案 | 优点 | 缺点 / 成本 |
| --- | --- | --- |
| A Runner 直连 broker | 控制面转发少 | 暴露内部基础设施；runner 配置复杂；协议绑定 provider；扩大攻击面 |
| B Broker queue group 竞争消费 | 实现短 | 命令可能被没有目标 socket 的实例消费；无法证明单活动连接和旧事件隔离 |
| C 共享数据库目录、lease/fencing 与中立 transport port | runner 协议稳定；路由确定；可替换 adapter；故障语义可验证 | 增加数据库事务、续租和 transport envelope |
| D 把 socket/在线状态存入 Redis | TTL 原生、读取快 | 新增第二状态权威；onboarding、scope 与审计仍需数据库事务；迁移和一致性更复杂 |

**决策**：采用 C。machine bounded context 唯一拥有 runner command/reply/event envelope、onboarding token hash、共享 connection directory、数据库时钟 lease 与单调 fencing token；进程内只保存本实例 socket handle。`internal/infra` adapter 实现 machine-owned transport port，首个实现使用固定官方 NATS。每个控制面实例启动时租用一个内部 `guid.ID` 作为 route holder，无新增 runner 配置。命令先按 machine ID 和 authenticated scope 解析活动 route，再发送到精确 holder；reply/event 必须携带相同 route 与 fencing token，过期或不匹配的 envelope 一律拒绝。

Pending onboarding token 只保存 SHA-256 hash、scope 和到期时间，原文仅在签发响应中出现。连接 lease 使用数据库服务器 UTC Unix 毫秒时钟；未过期 lease 拒绝第二连接，过期接管递增 fencing token。确认、取消、轮换、路由解析和机器选择全部由 verified tenant/entity/principal claims 约束。

**后果**：正面是 runner 与 broker 解耦、双实例调度确定、旧连接可被 fencing、NATS/MQTT adapter 可替换且不改变公开协议。代价是每个活动 socket 需要周期续租，命令增加一次共享目录读取，发布时必须先执行三方言 migration。回滚前需停止新版本实例并等待或释放活动 lease；confirmed machine 和长期 token hash 保留，新增 pending/connection 数据可删除。数据库或 transport 不可用时 fail closed，不得回退实例内路由或广播竞争。

**关联**：`PRD.md` §5.3.1、§16.3、§17.2；`docs/superpowers/plans/2026-08-14-paperclip-adoption-roadmap.md` Phase R；`docs/superpowers/specs/2026-08-08-machine-runner-onboarding-design.md`；`.rules/3.ARCH.md`、`.rules/3.BACKEND.md`、`.rules/3.TEST.md`。
