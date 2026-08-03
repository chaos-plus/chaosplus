# IAM 授权写入契约

> 状态：当前实现
> 适用代码：`internal/modules/iam/repository*.go`、`service*.go`、`directory_binding.go`、`entity.go`、`transaction.go`、`api/rest.go`

## 1. 目标

本文定义角色、权限、租户成员和菜单如何写入本地主数据库，以及这些写入何时影响在线授权。系统不执行外部关系写入；管理 API 和 `Authorizer` 读取同一事实来源。

当前公开写入包括：

- role create/update/delete；
- permission grant/revoke；
- role member add/remove；
- group/position role binding add/remove；
- role data scope get/replace；
- tenant member create/update/disable；
- menu create/update/delete；
- entity create/update/delete；
- entity scoped direct Principal role binding put/delete；
- entity and business-resource relationship list/put/delete；
- Principal create/update/disable/restore，见 [IAM 管理端](iam-admin-console.md)。

## 2. 写入边界

所有 IAM 写入都必须带经过认证和授权的租户上下文：

```text
request
  -> authenticate principal
  -> require active tenant membership
  -> check operation permission
  -> validate DTO and domain invariant
  -> execute tenant-scoped transaction coordinator
       -> mutate domain rows
       -> advance tenant policy revision when state changed
       -> append hash-chain audit through AuditAppender
  -> return canonical envelope
```

业务主键即使全局唯一，repository 仍必须同时使用 `tenant_id`。禁止根据请求体中的角色、主体或菜单字段推导调用者权限。

## 3. 数据所有权

| 表 | 写入所有者 | 关键不变量 |
|---|---|---|
| `iam_roles` | IAM Repository | `(tenant_id, name)` 唯一 |
| `iam_role_permissions` | IAM Repository | permission 必须存在于代码目录 |
| `iam_role_members` | IAM Repository | 添加前 Membership 必须 active |
| `iam_group_role_bindings` | IAM Repository；Organization migration | Role 与静态/动态组必须同租户，组必须 active |
| `iam_position_role_bindings` | IAM Repository；Organization migration | Role 与岗位必须同租户，岗位必须 active |
| `iam_role_data_scopes` | IAM Repository；Organization migration | 缺失行表示 `all`，scope 为闭合集合 |
| `iam_role_scope_departments` | IAM Repository；Organization migration | 只用于 `selected_departments`，部门必须同租户且 active |
| `iam_member_departments` | IAM Repository；Organization migration | 每个 tenant member 最多一个主部门 |
| `iam_tenant_members` | IAM/Identity Service | `(tenant_id, user_subject)` 唯一 |
| `iam_menus` | IAM Repository | 父节点同租户、无环、权限码有效 |
| `iam_entities` | IAM Repository | 父节点同租户、最大深度 16、无环、同级类型和名称唯一 |
| `iam_role_bindings` | IAM Repository | tenant/entity scope、直接 Principal、allow/deny、可选到期时间 |
| `iam_relationships` | IAM Repository | 完整元组主键；tenant 内受限主体与 active entity 的 owner/editor/viewer 关系 |
| `iam_resource_relationships` | IAM Repository | 完整元组主键；active entity 所属终端业务资源的 opaque type/ID 关系，不保存业务对象正文 |
| `iam_policy_revisions` | IAM Transaction Coordinator | 每个 tenant 的授权状态单调 revision |
| `iam_audit_events`、`iam_audit_heads` | Audit Service | append-only hash chain，与管理写入共用事务 |
| `iam_principals` | Identity Service | login name 全局规范化且唯一 |
| `iam_credentials` | Identity/Authn Service | 只保存密码 hash 和安全状态 |

角色权限和成员关系都以三元组主键保证幂等。重复 grant/add 返回成功响应中的 `changed=false`；不存在的 revoke/remove 同理，而非法 role 或 permission 仍返回明确错误。

## 4. 角色事务

### 创建与更新

Service 负责规范化名称、长度校验和部分更新合并；Repository 负责唯一约束映射与持久化。角色名为空、超过 128 字符或描述超过 4096 字符时拒绝写入。

### 删除

角色删除在同一个主数据库事务中执行：

```text
ensure role exists
  -> delete iam_role_permissions
  -> delete iam_role_members
  -> delete iam_group_role_bindings
  -> delete iam_position_role_bindings
  -> delete iam_roles
  -> increment iam_policy_revisions
  -> append role_deleted audit event
  -> commit
```

任一步失败都会回滚，不存在远程提交成功但本地确认失败的双写窗口。数据库外键提供最后一道级联完整性保护，显式删除顺序则保持三种 dialect 行为一致。

## 5. 权限与成员写入

权限写入前，`Service.changePermission` 使用 `authz.Registry.Find` 验证 canonical code。数据库不能出现管理 API 写入的未知权限码。

成员加入角色前，`Service.changeMember` 调用 `Repository.IsMemberActive`。这保证：

- 主体必须已进入同一租户；
- disabled 成员不能获得新的角色；
- `tenant_id` 同时约束 Membership、Role 和 RoleMember。

Membership 被禁用后，受保护请求会在角色检查前被拒绝，因此历史角色行不能绕过租户准入。

### 用户组与岗位角色绑定

`assignee_type` 是闭合集合 `group | position`，不能由请求拼接任意表名。新增绑定前，Repository 在同一事务内确认 Role 和目录项都存在于当前 tenant，且新增时目录状态为 active。解除已存在绑定时即使目录后来被停用仍允许清理。

`Authorizer.CheckBulk` 在每次请求实时组合直接角色、有效静态组成员角色、动态规则匹配角色和有效岗位任职角色。组/岗位状态、tenant Membership、静态成员/岗位时间窗以及动态组当前成员属性均参与判定。动态规则损坏时授权失败关闭；绑定变化无需刷新 session。角色绑定中的用户组或岗位不能删除，角色删除则显式清理其绑定。

目录绑定 no-op 不推进 revision，也不追加审计；有效变化与 `role_directory_binding_added/removed` 在同一事务提交，审计失败时绑定和 revision 一并回滚。

### 实体与作用域绑定

实体类型是小写 ASCII 数据，不为公司、企业、商户或门店增加授权分支。父链必须属于同一 tenant，最大深度为 16，更新拒绝自身或后代环；metadata 必须是最大 16 KiB 的 JSON object。有直接子节点、作用域绑定或关系引用时删除返回冲突。

实体绑定只接受同 tenant role 和 active tenant member，可设置 `allow | deny` 及未来到期时间。重复 PUT 和不存在绑定 DELETE 返回 `changed=false`，不推进 revision 或写审计；有效创建、更新、删除和绑定变化与 tenant policy revision、`entity_*` hash-chain audit 同事务。`Authorizer.CheckEntity` 实时读取目标实体、祖先与 tenant scope，显式 deny 优先于 allow。

`POST /iam/authorization/constraints` 和 `POST /iam/authorization/explain` 是只读检查接口，不写 revision 或审计。两者读取变更前后 revision；只有 revision 一致时才返回结果，连续三次变化则返回 503。约束只包含参数值，业务查询通过 `authzsql.ApplyEntityConstraint` 同时下推 tenant 和 entity 边界，不允许拼接调用方 SQL。

### 实体与业务资源关系写入

关系写入只接受以下组合：直接 `principal`、`group#member`、`position#member`、`entity#owner|editor|viewer`，关系为 `owner|editor|viewer`。实体目标写入 `iam_relationships`，其 `resource_type` 必须等于当前 tenant active entity 的 type。业务资源目标写入 `iam_resource_relationships`，必须同时提供所属 active `entity_id`、权限目录声明的 `resource_type` 和业务模块持有的 opaque `resource_id`；IAM 不复制业务对象。主体缺失、停用、跨 tenant、未知组合、循环或超过 16 层均拒绝。可选 `starts_at/ends_at` 使用 RFC 3339，结束时间必须在未来且晚于可选开始时间；无效窗口返回三语 `422 invalid_relationship_window`。

可选 `condition` 必须是 `version: 1` 的受限 JSON AST，不接受 SQL、脚本或客户端自定义属性。组合 operator 为 `all/any/not`；比较为 `eq/neq/gt/gte/lt/lte/in/contains`；时间为 `between_time`。字段闭集是 `auth.acr`、`auth.amr`、`client.id`、`network.zone`，只从已验证认证上下文读取。原始 JSON 最大 4 KiB、深度 8、节点 32，字符串最长 128、列表最多 16；未知字段/operator/type/timezone 或超限返回三语 `422 invalid_relationship_condition`。

`POST /iam/relationships` 在一个事务中：

```text
lock iam_policy_revisions tenant row
  -> validate active subject and resource
  -> canonicalize condition and insert tuple or update starts_at/ends_at/condition_json
  -> validate the graph using all stored entity edges
  -> advance iam_policy_revisions
  -> append relationship_put audit event
  -> commit
```

完整元组、窗口和规范化条件都相同时 `changed=false`，不推进 revision 或审计；窗口或条件变化原位更新、保留原 `created_at`，并与 revision、`relationship_put` 审计同事务。`DELETE /iam/relationships` 使用实体关系的七个或业务资源关系的八个主键字段精确删除，不携带窗口/条件；不存在时同样为 no-op，有效删除与 revision、`relationship_deleted` 同事务。数据库只对实体目标或业务资源所属实体使用外键，因为 subject 是多态闭集；因此组和岗位删除在各自事务中显式查询两表 subject 引用并返回 `409 group_relationship_bound|position_relationship_bound`，实体删除同时检查两表的资源和主体引用。未来、当前、已过期或条件不成立的元组都保留引用保护，直到显式删除。

`POST /iam/authorization/check`、constraints 和 explain 都是只读操作。检查业务资源时，调用方必须先按 `tenant_id + entity_id` 从业务 repository 加载对象，再同时提交 `entity_id/resource_type/resource_id`；缺少成对资源字段或 permission/resource 类型不匹配返回三语 `422 invalid_resource_authorization`。授权遍历使用批量 frontier 查询和 visited set，不使用数据库递归 CTE；业务资源是终端节点。每次判定固定一个服务端 UTC 时间，仅遍历时间窗有效且条件对可信上下文求值为 true 的边；缺字段或损坏条件按 false 处理。关系、组成员或岗位任职达到 `ends_at` 后在下一次判定即时撤权，不依赖 worker；显式 entity scoped deny 始终优先于 relationship allow。

### 角色数据范围与成员主部门

角色数据范围使用 `all | self | department | department_and_descendants | selected_departments`。缺省 `all` 不写范围行，保留历史角色行为；只有 `selected_departments` 接受非空部门 ID。写入在同一事务校验 role、tenant 和 active department，替换范围与选择、推进 revision 并追加 `role_data_scope_updated`；规范化后相同的 PUT 不写 revision 或审计。

tenant member 的可选 `department_id` 与 Membership upsert 在同一事务提交。清空字符串删除主部门关系；不存在或停用部门分别返回明确错误，失败时成员资料、revision 和审计全部回滚。部门删除前检查成员主部门和角色范围引用，存在任一引用返回 `409 department_in_use`，外键 `ON DELETE RESTRICT` 是并发保护。

## 6. 菜单写入

菜单创建和更新必须满足：

- label 非空且长度不超过 128；
- route 为空或以单个 `/` 开始；
- icon、排序和状态在规定范围内；
- `permission_code` 为空或存在于代码权限目录；
- parent 存在于同一租户；
- 更新后的父链无循环，遍历深度不超过 100。

删除前会检查直接子节点；存在子节点时返回冲突，调用方必须先移动或删除子节点。有效菜单读取只选择 active 项，并通过 `CheckBulk` 裁剪。

## 7. Principal 与 Membership 原子性

`identity.Service.Create` 在一个事务内完成：

1. 规范化 login name 和 email；
2. 使用 Argon2id 生成密码 hash；
3. 写入全局 `iam_principals`；
4. 写入 `iam_credentials`；
5. 写入目标租户的 active Membership。

任何一步失败都不留下半个账号。禁用 Principal 时，在同一事务内更新主体状态并撤销其全部 browser session 和 refresh token；恢复只恢复主体状态，不自动恢复租户 Membership 或历史会话。

## 8. 一致性和并发

授权变更提交后，新请求直接查询已提交数据，立即生效。当前不使用授权结果跨请求缓存。

- `transactionCoordinator` 只编排数据库内的领域写入、revision 和审计，不在事务内调用网络服务；
- IAM 通过本地 `AuditAppender` port 依赖审计能力，`internal/app/modules.go` 在组合根注入真实 `audit.Service.AppendTo`，模块之间不存在反向依赖；
- 角色、权限、直接角色成员、用户组/岗位角色绑定、tenant member 和菜单的有效管理写入都追加 tenant 审计；目录绑定 no-op 不写审计，现有直接 grant/member no-op 记录 `changed=false`，两者都不递增 revision；
- 角色数据范围有效变化写 revision/audit，相同范围和部门集合为严格 no-op；
- 实体及其直接角色绑定的有效写入追加 `entity_*` 审计并推进 policy revision，绑定 no-op 不产生审计噪声；
- 审计失败、revision 失败或领域失败都阻止提交；
- 唯一约束处理并发重复创建；
- grant/revoke 和 add/remove 使用 insert-ignore/delete 形成幂等操作；
- 密码修改使用旧 hash 条件更新防止并发覆盖；
- password history、密码更新、其他会话撤销和 refresh 撤销在一个事务中完成。

SQLite 写场景启用 foreign keys、busy timeout 和可写文件数据库 WAL；MySQL/PostgreSQL 通过各自等价 migration 保持同一领域约束。

## 9. HTTP 接口

| 资源 | 接口 |
|---|---|
| Roles | `GET/POST /iam/roles`，`GET/PATCH/DELETE /iam/roles/{role_id}` |
| Grants | `GET /iam/roles/{role_id}/permissions`，`PUT/DELETE .../{permission_code}` |
| Role members | `GET /iam/roles/{role_id}/members`，`PUT/DELETE .../{subject}` |
| Directory role bindings | `GET /iam/roles/{role_id}/directory-bindings`，`PUT/DELETE .../{assignee_type}/{assignee_id}` |
| Role data scope | `GET/PUT /iam/roles/{role_id}/data-scope` |
| Memberships | `GET/POST /iam/members`，`GET/PATCH /iam/members/{subject}` |
| Menus | `GET/POST /iam/menus`，`GET/PATCH/DELETE /iam/menus/{menu_id}` |
| Entities | `GET/POST /iam/entities`，`GET/PATCH/DELETE /iam/entities/{entity_id}` |
| Entity role bindings | `GET /iam/entities/{entity_id}/role-bindings`，`PUT/DELETE .../{role_id}/{principal_id}` |
| Relationships | `GET/POST/DELETE /iam/relationships` |
| Authorization inspection | `POST /iam/authorization/check`，`POST /iam/authorization/constraints`，`POST /iam/authorization/explain` |

受保护接口接受 bearer JWT 或 `cp_session` Cookie，并强制要求 `X-Tenant-Id`。业务响应统一为：

```json
{
  "code": 0,
  "message": "success",
  "meta": {},
  "data": {}
}
```

OAuth/OIDC 协议端点按标准返回协议原生 payload，不套业务 envelope。

## 10. 代码落点

```text
internal/modules/iam/domain/       值对象、状态和领域错误
internal/modules/iam/service.go    role/permission/member 编排
internal/modules/iam/service_admin.go
                                  membership/menu 编排
internal/modules/iam/repository.go role/grant/member 事务
internal/modules/iam/repository_admin.go
                                  membership/menu 持久化
internal/modules/iam/transaction.go
                                  领域写入 + policy revision + audit 原子提交
internal/modules/iam/entity.go     entity 与 scoped binding 持久化、校验和事务编排
internal/modules/iam/relationship.go
                                  关系元组写入、图验证、批量遍历和解释路径
internal/modules/iam/data_scope.go role scope、member department 与约束事实
internal/modules/iam/api/rest.go   DTO、状态码、Guard
internal/modules/iam/api/relationship_api.go
                                  关系 CRUD 与实体/业务资源授权检查 API
internal/modules/iam/sql/{sqlite,mysql,postgres}/
                                  等价迁移，含 00015-00018 关系、窗口与条件及 00019 认证强度
```

写新能力时按 domain -> repository -> service -> API -> composition root 的顺序实现。所有管理 mutation 必须复用现有事务协调器；只有出现不同提交语义时才增加新抽象，不能绕开 revision 或审计直接写 repository。

## 11. 必测场景

- 同名角色并发创建；
- grant/revoke、add/remove 重复提交；
- 跨租户 role/member/menu ID；
- disabled Membership 添加角色和访问接口；
- role 删除回滚与关联清理；
- 菜单环、跨租户 parent、有子节点删除；
- 实体跨租户 parent、环、最大深度、同级冲突、metadata 上限和删除保护；
- 实体绑定的 allow/deny/expiry/祖先继承、active Membership、no-op 和审计失败回滚；
- 关系主体/资源状态、资源类型、组/岗位成员窗口、条件 AST 限制、可信上下文、缺上下文/损坏 JSON fail-closed、循环、深度、显式 deny、幂等、解释路径和删除保护；
- 五种角色数据范围、直接/组/岗位来源、主部门停用、selected department、no-op revision/audit 和跨租户拒绝；
- 权限写入后立即 allow，撤销后立即 deny；
- 每类管理 mutation 的真实审计拒绝 trigger 都会回滚领域状态和 policy revision；
- revision 表不可写时领域状态和审计均不提交；
- 真实 HTTP 审计失败返回 500，且不会留下角色或 revision；
- Principal 禁用同步撤销 session/refresh；
- SQLite 真实文件并发写，以及可达时的 MySQL/PostgreSQL contract suite。

测试必须使用生产 constructor、真实数据库和真实 HTTP listener，不得使用 mock、fake、stub、miniredis 或拦截 fixture。
