# Chaosplus 身份认证与权限设计

状态：**实施中**
当前里程碑：`feat/iam-authorization-writes`

## 1. 结论

Chaosplus 采用三层职责边界：

| 组件 | 职责 | 不负责 |
|---|---|---|
| Zitadel | 用户、组织、登录、密码、MFA、OIDC/OAuth2、Token 签发 | 业务对象权限 |
| Chaosplus DB | tenant/merchant/store/dept、成员映射、角色与菜单展示元数据、outbox | 密码认证、最终权限判断 |
| SpiceDB | RBAC + ReBAC 关系、接口权限、对象数据权限、反向权限查询 | 用户密码、业务数据、菜单展示信息 |

系统不再实现本地密码用户表、自签 JWT、refresh token 或密码散列。Zitadel 的
OIDC `sub` 是全局身份标识，在 SpiceDB 中映射为 `user:<sub>`。

可以存在不含密码的业务成员表，例如：

```text
tenant_members(zitadel_subject, tenant_id, status, ...)
user_profiles(zitadel_subject, display_name, avatar, ...)
```

## 2. 权限模型

整体模型是 **以 ReBAC 为底座的 RBAC + ReBAC 组合**：

- RBAC：用户加入角色，角色获得权限码。
- ReBAC：用户、角色、组织和业务对象之间通过关系链推导权限。
- SpiceDB 是最终授权决策源，业务库不维护可独立做决定的 `role_permissions` 副本。

示例关系：

```text
role:store_manager#member@user:<zitadel_sub>
tenant:t1#store_view_role@role:store_manager#member
store:s1#admin@user:<zitadel_sub>
store:s1#merchant@merchant:m1
```

## 3. 组织层级

第一阶段统一使用以下业务术语：

```text
platform
  tenant       品牌租户
    merchant   商户
      store    店铺
    dept       部门树
```

不再混用 `instance`、`outlet` 等旧名称。部门是 tenant 下的独立树，可通过业务关系
关联 merchant/store；这类关系在实际数据权限用例出现时再加入 SpiceDB schema。

## 4. 认证流程

```text
客户端 -> Zitadel 登录 -> access token
客户端 -> Chaosplus API (Bearer token)
Chaosplus -> OIDC issuer/JWKS 校验签名、iss、aud、exp、nbf
Chaosplus -> user:<sub>
```

当前实现：

- OIDC discovery 和 JWKS key 缓存。
- 仅接受 `RS256`，拒绝未知算法。
- 校验 `iss`、可配置 `audience`、`exp`、`nbf`。
- `/authn/me` 返回当前 Zitadel subject 及 SpiceDB subject。
- 权限不写入 JWT，角色关系变化不要求用户重新登录。

生产环境必须配置明确的 `audience`，不能保留空数组跳过 audience 校验。

## 5. 接口权限声明

权限目录、路由声明、OpenAPI 和 SpiceDB schema 使用同一份 `authz.Registry`。
权限码固定为 canonical `resource_verb`，不允许通过显式 `Code` 创建别名或覆盖 schema
内置标识符。

路由使用 guarded registration：

```go
authz.Register(registrar, api, huma.Operation{
    OperationID: "update-store",
    Method:      http.MethodPatch,
    Path:        "/stores/{id}",
}, authz.Guard{Resource: "store", Verb: "update"}, handler)
```

注册动作同时完成：

1. 校验 `store_update` 是否存在于权限目录。
2. 在 Huma operation metadata 中记录 Guard。
3. 在 OpenAPI 中输出 `x-authz-permission: store_update`。
4. 附加 Zitadel token 验证和 SpiceDB Check middleware。
5. 声明 401、403、503 响应。

请求检查目标为：

```text
tenant:<X-Tenant-Id>#<resource>_<verb>@user:<zitadel_sub>
```

当前 active tenant 暂由 `X-Tenant-Id` 提供。后续应替换为经过服务端验证的会话 scope，
不能长期信任客户端任意切换 tenant header。

### 路由声明门禁

所有 HTTP operation 必须满足以下之一：

- 通过 `authz.Register` 声明 Guard；
- 对健康检查、公开基础设施接口、OIDC callback，或自行完成认证的 `/authn/me`，
  通过 `authz.RegisterPublic` 显式声明绕过 SpiceDB Guard gateway。

`authz.ValidateOperations` 在服务启动前执行，同时有仓库级测试
`TestRESTOperationsDeclareAuthorization`。漏声明会导致启动失败和 CI 失败。

## 6. SpiceDB schema

当前 schema 覆盖：

- `user`
- `role#member`
- `platform#admin`
- `tenant` 的角色权限关系和 `administer`
- `merchant`
- `store`
- `dept`
- `menu`

角色授权关系：

```text
tenant:T#<resource>_<verb>_role@role:R#member
role:R#member@user:U
```

接口检查：

```text
tenant:T#<resource>_<verb>@user:U
```

对象数据权限会在 P2 扩展到：

```text
store:S#view@user:U
order:O#view@user:U
```

并通过 `LookupResources` 回答“用户可以看哪些对象”。

## 7. 菜单权限

菜单展示信息保存在业务库或静态元数据中：

```text
id, parent_id, route, icon, order, permission_code
```

`permission_code` 必须引用同一个 authz catalog，不建立独立的菜单权限体系。最终菜单树
通过批量权限检查后裁剪。当前只完成 menu catalog，CheckBulk 和按用户裁剪尚未实现。

## 8. 数据权限

数据权限不采用容易漏调用的 repository scope enum。目标方案：

- 详情：对具体 SpiceDB object 执行 `CheckPermission`。
- 列表：`LookupResources` 得到可访问 ID，再下推到 SQL 查询。
- 分享：写入 `shared_viewer` 等对象关系。
- 层级：通过 merchant/store/dept 关系箭头推导。

列表查询必须分页，并限制一次下推的 ID 数量。大数据量时需要游标、物化关系或按业务
层级转换为 SQL 谓词，不能无界加载全部 ID。

## 9. 一致性与写入

- 关系写使用 SpiceDB `TOUCH` / `DELETE`，保证幂等重试与撤权。
- 写入返回的 ZedToken 用于后续 `at_least_as_fresh` Check。
- 没有 token 的关键检查使用 `fully_consistent`。
- Lookup 使用 `minimize_latency`，最终对象读取仍需按一致性要求校验。
- SpiceDB 故障时权限检查 fail closed；当前 Huma middleware 返回 503 并记录日志。

业务 DB 与 SpiceDB 的双写必须通过 outbox：

```text
业务事务：业务元数据 + relationship desired-state authz_outbox
worker：WriteRelationshipUpdates(TOUCH/DELETE)
成功：记录 ZedToken，标记 outbox 完成
```

outbox 与第一批角色授权/成员绑定写接口一起实现，不单独创建没有消费者的空壳。

## 10. 缓存策略

第一版不增加 Redis 权限决策缓存：

- 优先使用 SpiceDB 自身缓存。
- 同一请求内使用 CheckBulk 合并检查。
- 先压测真实延迟和吞吐。

只有确认需要时才加入 Redis perm-set cache，并必须同时定义：

- cache key 的 tenant/scope/subject 维度；
- 关系变更后的失效机制；
- ZedToken 或 auth version 的一致性边界；
- SpiceDB 不可用时是否允许使用旧缓存。

## 11. 模块边界

```text
internal/core/extension/authn/       Zitadel OIDC/JWKS verifier
internal/core/extension/authz/       catalog、Guard 注册、middleware、CI gate
internal/core/extension/spicedbx/    官方 authzed-go client 封装
internal/modules/authn/              /authn/me 等认证相关 API
internal/modules/iam/                当前访问控制管理/read model
```

`internal/modules/iam` 不是第二个身份提供方。它只负责编排角色、菜单、组织元数据和
SpiceDB 关系。只有 Zitadel authn 与 SpiceDB authz 同时启用时才挂载 IAM 管理 API，
避免权限栈未配置时意外公开管理面。模块增大后可改名/拆分为 `access` 与
`organization`。

## 12. 交付状态

| 阶段 | 状态 | 已完成 | 未完成 |
|---|---|---|---|
| P0 基础 | 部分完成 | Zitadel 部署、正式 PKCE/JWT 应用与固定 audience、OIDC verifier、SpiceDB client/schema apply、事务 outbox、真实 smoke | 生产 TLS |
| P1 接口权限 | 进行中 | catalog、Guard 注册、OpenAPI 扩展、单条 Check、启动/CI 门禁、角色授权与成员写 API | CheckBulk、完整业务路由 |
| P2 层级和数据权限 | 未开始 | 基础 merchant/store/dept schema | LookupResources 下推、分享、层级用例 |
| P3 菜单权限 | 已完成 | menu metadata CRUD、CheckBulk、按用户树裁剪、祖先容器保留 | Redis 权限缓存按压测决定 |
| P4 管理面 | 进行中 | BFF 登录/注册、tenant member、role CRUD、permission grant/revoke、member add/remove、menu CRUD、React 管理台 | Zitadel 管理 API 服务账号、正式 bootstrap CLI |

新增 authn/authz/spicedbx/iam 包要求保持 90% 以上测试覆盖率；远程依赖通过显式 smoke
环境变量运行，不让普通单元测试依赖 10.0.0.100。

## 13. 下一步

1. 实现非 HTTP 的初始 platform/tenant administrator bootstrap CLI。
2. 增加 tenant/merchant/store/dept 管理 API，并同步层级关系。
3. 实现 LookupResources 数据权限下推和对象分享。
4. 在 `/me/menus` 前实现 CheckBulk 与有效权限查询。
5. 根据真实延迟和流量决定是否增加 Redis perm-set cache。

## 14. 非目标与边界

- IoT 设备传输认证由 EMQX + mTLS/设备证书负责；SpiceDB只判断用户能否访问设备。
- AI 调用额度、计费和速率限制属于计量系统，不放入 SpiceDB。
- 业务数据不存入 SpiceDB，只存授权关系。
- 动态数值、频繁变化的重型 ABAC 不优先建模为关系；确有需求时再评估 caveat 或策略引擎。
