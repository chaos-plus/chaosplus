# 持久化契约

## 标识类型

| 类型 | Go | SQL | 传输边界 |
|---|---|---|---|
| 内部实体/资源/事件 ID | `guid.ID` | `BIGINT` | 十进制字符串 |
| 内部 PK/FK，包括 tenant/entity/principal/owner/审计 actor | `guid.ID` | `BIGINT` | 十进制字符串 |
| 有序日志序号 | `int64` | 方言原生生成的 `BIGINT` | 明确定义的数字/字符串 |
| 版本/围栏/尝试/计数 | 有界整数 | 带约束的 `BIGINT`/`INTEGER` | 数字 |
| 外部身份 | 命名字符串类型 | 有界字符串 | 字符串 |
| Token/hash/幂等键/checksum/自然键 | 命名字符串类型 | 有界字符串 | 字符串 |

内部 ID 严禁使用 `string`/`TEXT`。JavaScript 无法安全表示 int64，因此 `guid.ID` 在 JSON 中编码为十进制字符串，在数据库中存为 `BIGINT`。

## 时间

- 时间点统一以 UTC Unix 毫秒存入 `BIGINT`。
- 仅当字段契约明确表示“尚未/永不”时才允许使用 `0`。
- 写入使用 `time.Now().UTC()`，转换使用 `time.UnixMilli(value).UTC()`。
- 严禁为时间点持久化本地时间、偏移量、格式化日期或 SQL session 本地时间戳。
- IANA 时区名只可作为展示偏好保存。

## 可变聚合

必须评估并记录 `tenant_id`、`entity_id`、`owner_id`、`created_at`、`created_by`、`updated_at`、`updated_by`、`deleted_at`、`deleted_by` 和 `version`。软删除资源的默认查询必须包含 `deleted_at = 0`。

`owner_id` 表示当前归属，`created_by` 表示创建审计，两者严禁互相替代。只追加日志不添加无意义的更新/删除字段；关联表和凭据按自身生命周期选择字段。

## 语义类型

- 枚举：Go 类型化常量、service 校验、三方言等价 `CHECK`。
- 布尔：Go `bool`；PostgreSQL/MySQL `BOOLEAN`；SQLite 使用 `BOOLEAN CHECK (value IN (0,1))`。
- JSON：写入前类型化并校验；查询需要时使用方言 JSON 类型，否则添加可用的有效性约束。
- 金额：最小货币单位整数加币种，或定精度 `DECIMAL`；账务事实严禁使用 `float64/REAL`。
- 时长：带非负边界的整数毫秒，与时间点严格区分。
- 密钥和原始 token：严禁持久化。

## 方言一致性

必须逐表对比 SQLite、MySQL、PostgreSQL 的名称/归属、PK/FK 类型、引用动作、可空性、默认值、约束、唯一范围、索引、审计/删除/版本字段、Goose Up/Down 行为、Bun 类型/tag、repository 谓词、历史回填和回滚安全性。
