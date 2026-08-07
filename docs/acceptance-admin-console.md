# IAM 管理端验收清单

适用范围：纯自研身份认证、租户 RBAC、OAuth 2.0/OIDC 和管理控制台。

## 0. 环境准备

```text
cd deploy/compose
./init-secrets.sh        # Windows: .\init-secrets.ps1
docker compose up -d --build --wait
docker compose ps
docker compose stats --no-stream
```

- [ ] `proxy`、`postgres`、`redis`、`chaosplus`、`web` 全部运行正常。
- [ ] Chaosplus 自动完成 Goose migration 和初始管理员引导。
- [ ] `GET /.well-known/openid-configuration` 和 `GET /.well-known/jwks.json` 返回 200。
- [ ] 记录每个容器的 CPU、内存、网络 I/O、块 I/O、PID 数和镜像大小。

## 1. 本地身份与会话

- [ ] 使用初始管理员账号在 `/login` 登录，随后进入控制台。
- [ ] 浏览器不保存 access token；`cp_session` 为 `HttpOnly` Cookie。
- [ ] 刷新页面后 `GET /authn/session` 仍返回当前身份。
- [ ] 连续错误密码达到阈值后账号被临时锁定，正确密码不能绕过锁定期。
- [ ] 退出后旧 Cookie 再访问 `GET /authn/session` 返回 401。
- [ ] 禁用 Principal 后，其全部浏览器会话失效。

### 1.1 TOTP 与恢复码

- [ ] 输入当前密码开始 TOTP enrollment，数据库中不存在明文 secret，过期 enrollment 不能确认。
- [ ] 使用二维码或手工密钥绑定验证器；首次有效动态码确认后生成 10 枚至少 128 bit 的恢复码。
- [ ] 恢复码只展示一次，数据库只保存 HMAC；每枚只能消费一次，重新生成后旧码全部失效。
- [ ] 已启用 MFA 的账号通过密码后只获得一次性 challenge，不设置 session Cookie。
- [ ] TOTP 接受当前时间步前后各一步；同一时间步不能重放，challenge 达到失败上限后立即失效。
- [ ] TOTP 或恢复码完成 challenge 后才创建 session；challenge 不能重放。
- [ ] 重新生成恢复码和停用 MFA 都要求当前密码加有效因子，并撤销其他 session 与全部 refresh token。
- [ ] 管理端在桌面和 390px 移动端完成绑定、一次性展示、MFA 登录、轮换和停用，无横向溢出。

### 1.2 Passkey/WebAuthn

- [ ] `authn.passkey.rp_id` 与 `origins` 由部署配置固定，不能从浏览器 Host/Origin/Forwarded header 推导。
- [ ] 注册要求有效 session 和当前密码，创建选项强制 discoverable credential 与 user verification。
- [ ] 注册与登录 challenge 随机、服务端保存 hash、五分钟失效，并在首次 finish 请求时一次性消费。
- [ ] Credential Record 使用 AES-256-GCM 密文落库，credential ID 只保存 SHA-256 索引，user handle 为每账号 32 字节随机值。
- [ ] 无密码登录只在 assertion 的 RP ID、origin、challenge、签名、UV 和 credential owner 全部验证后创建 Cookie session。
- [ ] sign counter 回退拒绝登录并记录安全事件；并发 counter 更新不能创建两个成功 session。
- [ ] 安全中心支持多凭据列表、重命名和单独删除；删除要求当前密码并撤销其他 session 与 refresh token。
- [ ] 真实 Chromium CTAP2 认证器完成注册、退出、无密码登录、重命名、删除和删除后拒绝；桌面与 390px 无横向溢出或控制台错误。

### 1.3 自助注册

- [ ] Compose 在没有真实通知供应商时保持 `authn.registration.enabled=false`；启用时同时要求 Web、邮箱验证和通知能力可用。
- [ ] `GET /authn/capabilities` 准确返回 registration、password recovery 和 passkey 能力；登录页只显示已启用入口。
- [ ] `POST /authn/register` 创建规范化邮箱 login name、Argon2id Credential 和 `activation_required=true` 的全局 Principal，不创建 Tenant Membership。
- [ ] Principal、Credential、验证 token、加密通知和 `_system` hash-chain audit 同事务；任一真实数据库写入失败均无残留。
- [ ] 已存在邮箱与新邮箱返回完全相同的 `202 {accepted:true}`；重复邮箱不新增 Principal、token 或通知。
- [ ] 注册账号在邮箱验证前不能通过密码、Passkey 或 token 登录；验证完成原子清除 `activation_required` 后才可登录。
- [ ] `/register` 成功页只提示检查邮箱，不泄露账号是否存在；真实 Chromium 完成注册、Webhook 链接验证和登录，1440px/390px 无溢出或控制台错误。
- [ ] Origin 拒绝、非法输入和功能关闭分别返回清晰的 `en-US/zh-CN/ms-MY` 403、422 和 503 错误。

### 1.4 邮箱验证

- [ ] Compose 在没有真实通知供应商时保持 `authn.email_verification.enabled=false`；启用时 verify/Webhook URL、鉴权 secret、timeout 和重试上限均通过配置校验。
- [ ] 已认证未验证账号可发起验证；匿名请求返回 401，Cookie 请求 Origin 不匹配返回 403，无主邮箱返回 409。
- [ ] 验证值至少 256 bit 随机熵，数据库只保存 keyed HMAC；token 绑定 Principal ID、规范化邮箱快照和过期时间。
- [ ] 同一 Principal 只有一个活动验证凭证；同一 token 并发提交只有一个成功，过期、已消费和邮箱变化均失败。
- [ ] 完成时仅在 Principal active、邮箱快照匹配且尚未验证时更新 `email_verified`，并在同一事务追加可验证审计。
- [ ] 邮箱变更与 Bootstrap 安全状态重建会作废活动验证 token；完成验证不撤销现有 session。
- [ ] 邮箱验证和密码找回共用加密 outbox、Webhook client、重试与陈旧锁恢复 worker；恢复关闭时验证 worker 仍运行。
- [ ] `/security` 和 `/verify-email` 在桌面与 390px 完成发起、pending、成功、无效和不可用状态；token 读取后从 URL 清除，不进入 localStorage/sessionStorage，不发送 tenant header。

### 1.5 密码找回

- [ ] Compose 在没有真实通知供应商时保持 `authn.recovery.enabled=false`；启用时 reset/Webhook URL、鉴权 secret、timeout 和重试上限均通过配置校验。
- [ ] 不存在、禁用、无邮箱和未验证邮箱与有效账号返回相同 `202` envelope，且前四类不产生恢复凭证或通知。
- [ ] 恢复值至少 256 bit 随机熵，数据库只保存 keyed HMAC；加密 outbox、日志、错误和浏览器存储中不出现明文值。
- [ ] 同一恢复值并发提交只有一个成功；过期、已消费、邮箱已变更和密码历史命中均失败且事务不产生部分更新。
- [ ] 完成恢复后旧密码、全部 Cookie session、access token 和 refresh token 立即失效，`credential_version` 恰好递增一次。
- [ ] 完成恢复写入可验证的高风险审计、发送安全通知，并在冷静期拒绝新增 TOTP 和 Passkey。
- [ ] Webhook 使用 payload `id` 与 `Idempotency-Key` 幂等；网络/非 2xx 重试到上限，损坏密文永久失败，陈旧 delivery lock 可恢复。
- [ ] `/recover` 在桌面和 390px 完成申请、错误、设置新密码与成功状态，不持久化恢复值、不发送 tenant header、无横向溢出或控制台错误。

## 2. Principal 与租户成员

- [ ] 在用户页面创建本地 Principal，并将其加入当前租户。
- [ ] 登录名全局唯一；重复创建被拒绝且不产生半条数据。
- [ ] 重置密码后旧密码失效，新密码可登录。
- [ ] 未加入租户或成员状态为 disabled 的用户访问该租户 `/iam/*` 返回 403。
- [ ] 伪造 `X-Tenant-Id` 不能读取、修改其他租户的数据。

### 2.1 平台租户生命周期

- [ ] `GET/POST/PATCH/DELETE /iam/tenants...` 只允许 active 平台管理员，普通 tenant administrator 返回 403 且侧边栏不显示入口。
- [ ] 平台租户接口不发送或信任 `X-Tenant-Id`；租户内管理接口仍必须携带准确 tenant header。
- [ ] slug 在平台内唯一且符合小写格式；创建、改名、停用、恢复和软删除使用清晰的 `404/409/422` 三语错误。
- [ ] PATCH 与 DELETE 使用 version 乐观锁，陈旧请求不能覆盖并发修改；有效写入与 policy revision、hash-chain audit 同事务。
- [ ] suspended/deleted tenant 的 membership、role 和 entity binding 在下一次授权立即 fail closed；恢复 active 后才重新允许进入。
- [ ] `/iam/tenants` 匿名深链登录后返回原页面；真实浏览器完成创建、改名、停用、恢复、进入、软删除和显示已删除，1440px/390px 无页面溢出、控制台错误或失败请求。

### 2.2 成员邀请

- [ ] `invitation_view/create/resend/revoke` 独立守卫列表、创建、轮换和撤销；只有具备 `invitation_view` 的有效菜单显示 `/iam/invitations`。
- [ ] 创建只接受合法邮箱、1 至 720 小时有效期、active 同租户部门和存在的同租户角色；相同租户邮箱的新邀请撤销旧 pending 邀请，跨租户 ID 不可见。
- [ ] 凭据具有至少 256 bit 随机 secret，数据库只保存用途隔离 HMAC；明文仅在创建或重发响应显示一次，日志、审计、列表和错误中均不出现。
- [ ] 重发轮换凭据并使旧值立即失效；撤销和过期凭据不能接受。无效、过期、状态冲突、登录名冲突、绑定缺失/停用均返回清晰的 `en-US/zh-CN/ms-MY` 稳定错误。
- [ ] 接受在一个事务内创建 Principal、Credential、active Membership、可选主部门和角色成员，并推进 policy revision、标记 accepted、追加 hash-chain 审计；注入任一写入失败不留下半状态。
- [ ] 同一有效凭据重复或并发接受只创建一个 Principal，后续响应标记 `already_accepted=true`；空 `role_ids` 在 HTTP JSON 中是 `[]` 而不是 `null`。
- [ ] `/iam/invitations` 完成创建、重发、撤销和一次性凭据展示；公开 `/accept-invitation` 完成账号创建。真实浏览器在 1440px/390px 无页面溢出、控制台错误、失败请求或异常响应。

## 3. 角色与权限

- [ ] 新建角色、授予权限并绑定成员后，无需重新登录即可访问对应接口。
- [ ] 撤销权限后下一次请求立即返回 403，不存在异步同步窗口。
- [ ] 删除角色后其权限和成员关系一并失效。
- [ ] `tenant_administer` 可管理当前租户；普通租户权限不能越过租户边界。
- [ ] 菜单只返回当前成员有权访问的节点，接口失败时前端不能展示完整管理菜单。

### 3.1 部门组织树

- [ ] `dept_view/create/update/delete` 分别守卫列表、创建、更新和删除接口；只有具备 `dept_view` 的有效菜单才显示 `/iam/departments`。
- [ ] 创建顶级部门、子部门和孙部门后，列表按同级 `sort_order/name/id` 稳定深度优先返回，depth 与 closure table 一致，空租户返回 `[]`。
- [ ] 同一父节点下规范化名称重复返回 409；不同父节点可使用相同名称；跨租户 parent 或 department ID 返回 404。
- [ ] 部门可移动到另一分支或根节点；移动到自身或后代返回 `department_hierarchy_cycle`，事务不留下半条 closure。
- [ ] PATCH 和 DELETE 使用 version 乐观锁，陈旧版本返回 `department_version_conflict`，不能覆盖并发管理员的修改。
- [ ] 有直接子部门时删除返回 `department_has_children`；从叶节点向上删除后领域状态、policy revision 和 hash-chain 审计同时提交。
- [ ] 人为制造审计失败时，创建、更新和删除全部回滚；no-op PATCH 不递增 version、revision 或审计事件。
- [ ] 管理端桌面与 390px 完成创建、编辑、移动、启停、折叠和删除，无页面级横向溢出、遮挡或控制台错误。

### 3.2 岗位与任职关系

- [ ] `position_view/create/update/delete/manage_member` 分别守卫读取、创建、更新、删除和成员 mutation；有效菜单按 `position_view` 显示 `/iam/positions`。
- [ ] code 规范化为小写 ASCII 并在 tenant 内唯一；同一 code 可在不同 tenant 使用，跨租户 position 或 principal ID 不可见。
- [ ] PATCH 和 DELETE 使用 version 乐观锁；成员存在时返回 `position_has_members`，角色绑定存在时返回 `position_role_bound`，两者清空后才能删除。
- [ ] 只有 active tenant member 可被分配岗位；可选结束时间必须晚于开始时间，PUT 可把定时任职更新为长期任职。
- [ ] no-op PATCH、相同成员 PUT 和不存在成员 DELETE 不推进 policy revision 或追加审计；审计失败时领域 mutation 回滚。
- [ ] 创建、更新、成员分配/更新/移除和删除产生完整 `position_*` hash-chain 事件，目标 ID 可检索。
- [ ] 管理端匿名深链可登录返回；桌面与 390px 完成岗位和成员工作流，页面与弹窗无溢出、遮挡、控制台错误或失败请求。

### 3.3 静态与动态用户组

- [ ] `group_view/create/update/delete/manage_member` 分别守卫读取、创建、更新、删除和成员 mutation；有效菜单按 `group_view` 显示 `/iam/groups`。
- [ ] 规范化名称在 tenant 内不区分大小写唯一；相同名称可在不同 tenant 使用，跨租户 group 或 principal ID 不可见；非 `static` 类型返回 `422`。
- [ ] PATCH 和 DELETE 使用 version 乐观锁；成员存在时返回 `group_has_members`，角色绑定存在时返回 `group_role_bound`，两者清空后才能删除。
- [ ] 只有 active tenant member 可加入用户组；可选结束时间必须晚于开始时间，PUT 可把定时成员更新为长期成员。
- [ ] no-op PATCH、相同成员 PUT 和不存在成员 DELETE 不推进 policy revision 或追加审计；审计失败时领域 mutation 回滚。
- [ ] 创建、更新、成员分配/更新/移除和删除产生完整 `group_*` hash-chain 事件，目标 ID 可检索。
- [ ] 管理端匿名深链可登录返回；桌面与 390px 完成用户组和成员工作流，页面与弹窗无溢出、遮挡、控制台错误或失败请求。
- [ ] 动态组规则只接受版本 1、`all|any`、白名单成员字段和 `in|not_in`，并强制 bytes/条件/值数量上限；未知或损坏规则失败关闭。
- [ ] 动态成员不物化，成员预览、目录角色授权与 ReBAC 都使用当前 active tenant member 属性；手工成员 PUT/DELETE 返回三语 `409`。
- [ ] 管理端可创建/编辑动态规则并只读预览计算成员；桌面和 390px 无溢出、遮挡、手工 mutation 控件、控制台错误或失败请求。

### 3.4 用户组/岗位角色绑定

- [ ] `role_manage_assignee` 独立守卫绑定和解绑，不能复用 `role_manage_member`；列表只需 `role_view`。
- [ ] `GET/PUT/DELETE /iam/roles/{role_id}/directory-bindings...` 具有准确 operation ID、summary、tag、安全声明、tenant header 和统一 envelope。
- [ ] group/position、role 与请求 tenant 任一不匹配都返回稳定 404；停用目录不能新增绑定，但已有绑定可以解除。
- [ ] 重复 PUT 和不存在绑定 DELETE 返回 `changed=false`，不推进 policy revision、不追加审计；有效写入与 revision、`role_directory_binding_added/removed` 同事务，审计失败整体回滚。
- [ ] 角色删除在同一事务清理直接成员、权限和全部目录绑定；目录仍被绑定时删除返回稳定 409。
- [ ] `CheckBulk` 同时计算直接成员、active 静态组有效成员、active 动态组当前规则匹配和 active 岗位有效任职；tenant member/目录停用、属性变化、开始时间未到或结束时间到达后下一请求立即生效。
- [ ] 角色管理端按直接成员、用户组、岗位、权限分区；停用项可见但不可新增，桌面和 390px 无页面溢出、控制台错误或失败请求。

### 3.5 角色数据范围与成员主部门

- [ ] `GET/PUT /iam/roles/{role_id}/data-scope` 的 operation ID、Guard、tenant header、schema、错误状态和 envelope 与生产 OpenAPI 一致。
- [ ] 缺省角色为 `all`；`self`、`department`、`department_and_descendants`、`selected_departments` 分别编译到 owner/department 约束，直接、组和岗位派生角色结果一致。
- [ ] selected department 必须非空且全部属于当前 tenant 并为 active；其他 scope 拒绝 department IDs；缺失或停用的成员主部门 fail closed。
- [ ] 范围与选择在同一事务替换并写 policy revision/hash-chain audit；排序去重后的相同 PUT 不推进 revision、不追加审计。
- [ ] Membership 资料和主部门原子更新；失败不留下部分资料。被成员或角色范围引用的部门返回 `409 department_in_use`。
- [ ] 角色页可保存五种范围和指定部门，主体页可维护主部门；桌面与 390px 无溢出、遮挡、控制台错误或失败请求。

## 4. 实体与作用域绑定

实体是租户内的通用递归节点，不固定为公司、企业、商户或门店；业务层级保持 `tenant -> entity -> business resources`。

- [ ] 实体、关系与授权检查共 14 个 OpenAPI operation 的 operation ID、summary、tag、Cookie/Bearer security、`X-Tenant-Id`、Guard 权限和响应状态与生产路由完全一致。
- [ ] 根/子实体 CRUD 始终按 tenant 隔离；跨租户 parent 或 entity ID 返回 404，同一实体 ID 在不同租户中互不影响。
- [ ] 父链最大深度为 16，移动到自身/后代和超深层级返回稳定冲突；同级 `type + name` 唯一，类型只写数据而不增加授权分支。
- [ ] metadata 只接受最大 16 KiB 的 JSON object；非法 JSON、数组、标量和超限输入在前端与后端均被拒绝。
- [ ] 有直接子节点或实体角色绑定时删除返回 409，清空依赖后才允许删除。
- [ ] 直接绑定只接受同 tenant role 与 active tenant member，支持 `allow | deny` 和未来到期时间；重复 PUT、不存在绑定 DELETE 为 no-op。
- [ ] 子实体继承父实体和 tenant scope 的 allow；目标、祖先或 tenant scope 上匹配权限的显式 deny 优先于 allow，到期后下一请求立即失效。
- [ ] `DataConstraint` 对 tenant role、组/岗位派生 role、owner/department scope 和 entity binding 生成一致的 allow-all/owner/department/resource/deny 范围，携带稳定 policy revision；空权限 fail closed，SQL 下推始终参数化并同时过滤 tenant。
- [ ] `ExplainEntity` 与 `CheckEntity` 共用判定实现，返回 permission/role/source/scope/effect 和稳定 reason；inactive member、继承 allow、显式 deny、管理员兜底及不存在实体均有真实测试。
- [ ] 业务资源关系携带 `tenant_id + entity_id + resource_type + opaque resource_id`，不复制业务对象；业务 repository 先按 tenant/entity 加载对象，业务资源作为终端节点参与 `CheckResource/ExplainResource`，permission 类型不匹配返回三语 422。
- [ ] 直接 Principal、active 用户组/岗位成员和实体关系链都能授权业务资源；显式 deny 优先、撤销即时生效，实体、组和岗位删除会保护两张关系表中的引用。
- [ ] 实体和绑定有效 mutation 与 tenant policy revision、`entity_*` hash-chain audit 同事务，审计失败时领域状态与 revision 全部回滚。
- [ ] `/iam/entities` 完成匿名深链返回、层级创建/折叠、编辑/启停、allow/deny/expiry 绑定、实体/业务资源关系、检查与解释、即时撤销、删除保护和清理；桌面与 390px 无页面溢出、控制台错误或失败请求。

## 5. OAuth 2.0 / OIDC

- [ ] OIDC discovery 声明 Authorization Code、Refresh Token、Client Credentials 和 PKCE S256。
- [ ] 公共客户端必须使用 PKCE S256；redirect URI 必须精确匹配登记值。
- [ ] 授权码只能使用一次，过期、错误 verifier 或错误 redirect URI 均被拒绝。
- [ ] `openid` scope 返回 ID Token，nonce 原样写入并可通过 JWKS 验签。
- [ ] Refresh Token 每次使用都轮换；复用旧令牌会撤销整个令牌族。
- [ ] Confidential Client 的密钥只在创建或轮换时返回一次，数据库只保存 Argon2id 哈希。
- [ ] 公开客户端不能调用 Token Introspection；撤销端点不能撤销其他客户端的令牌。
- [ ] OAuth Client 的管理、更新和删除严格受 `X-Tenant-Id` 隔离。
- [ ] 管理端可创建、编辑、启停、轮换和删除 OAuth Client，公共客户端不能选择 Client Credentials。
- [ ] 密钥一次性展示支持复制和下载；桌面和 390px 移动端完整流程无横向溢出或隐藏操作。

## 6. SCIM 2.0 预配

- [ ] `/iam/scim-directories` 只允许 active tenant administrator，匿名深链登录后返回原页面，跨 tenant directory ID 返回 404/403 而不泄露存在性。
- [ ] 创建、改名、停用和恢复 directory 使用 version 乐观锁；同 tenant 规范化重名与陈旧 version 返回清晰的三语 `409`。
- [ ] active directory 最多 10 枚有效凭据；过期时间必须在未来，完整 Bearer token 只显示一次，数据库、列表、日志和审计中不出现 secret。
- [ ] 无效、过期、撤销凭据和 disabled directory 请求均返回 `application/scim+json` 的 `401` 与 `WWW-Authenticate`；`en-US/zh-CN/ms-MY` detail 清晰且不包含内部错误。
- [ ] ServiceProviderConfig、Schemas、ResourceTypes 与 OpenAPI 声明一致；Users、Groups 的 CRUD、分页、支持的 Filter、PATCH、Bulk 和弱 ETag 通过真实 HTTP contract。
- [ ] User 停用/删除立即撤销相关 session/授权，Group 成员只接受同 directory active User；最后管理员保护拒绝破坏 tenant invariant 的预配写入。
- [ ] User/Group、mapping、policy/session 和 hash-chain audit 同事务；真实数据库写入失败不留下半状态，Bulk 只保留阈值前已完成操作。
- [ ] provisioning migration 在 SQLite、MySQL、PostgreSQL 均完成 up/down/up/down-to-zero；三方言过滤比较产生相同结果。
- [ ] 真实 Chromium 完成目录创建、编辑、启停、凭据创建/撤销、一次性 token、Base URL 复制和协议预配；1440px/390px 无溢出、控制台错误或意外失败请求。

## 7. 审计日志

- [ ] `GET /iam/audit-events` 只返回当前 `X-Tenant-Id` 的事件，跨租户 ID 不能读取详情。
- [ ] 列表按事件类型、结果、操作主体、目标和时间范围筛选，分页 `total` 与真实记录一致。
- [ ] `GET /iam/audit-integrity` 从 sequence 1 验证到 tenant head；篡改、删除或链断裂不能返回 valid。
- [ ] `GET /iam/audit-events/export` 需要 `audit_event:export`，只导出当前 tenant 与当前筛选结果，并先追加 `audit_export_requested`。
- [ ] 导出 manifest 固定已验证的 `head_sequence/head_hash`；请求期间新增事件不进入该快照，历史 `sequence=0` 事件不伪装为链式证据。
- [ ] NDJSON 最后一行必须是 `complete`，其事件数和内容 SHA-256 与事件行一致；数据库中断、连接截断、错误 MIME 或缺少完成记录时管理端拒绝保存。
- [ ] SQLite、MySQL、PostgreSQL 均拒绝对 `iam_audit_events` 执行 UPDATE 和 DELETE。
- [ ] OAuth Client 创建、编辑、secret 轮换和删除与对应审计事件处于同一事务，任一写入失败时整体回滚。
- [ ] 密码修改、单会话撤销和全会话撤销与 `_system` hash-chain 审计处于同一事务，审计失败时安全状态保持不变。
- [ ] 角色、权限、直接角色成员、用户组/岗位角色绑定、tenant member 和菜单管理写入在同一事务中更新领域状态、tenant policy revision 和审计；任一步失败时三者全部回滚，幂等 no-op 不递增 revision。
- [ ] Identity 创建在同一事务中写入 Principal、Credential、当前 tenant Membership、tenant policy revision 和 `principal_created`；revision 或审计失败时不得留下任一半状态。
- [ ] Identity 邮箱更新与验证状态重置、credential version 递增、session/refresh 撤销、recovery/verification token 消费、当前 Membership 同步和 `principal_updated` 同事务；禁用/恢复与状态、必要的撤销及对应审计同事务。
- [ ] 全局 Principal 的更新、禁用和恢复影响不限于发起租户，但审计按操作来源 `X-Tenant-Id` 分区；禁用后所有旧 session/token 立即失效，恢复不得复活旧会话。
- [ ] MFA enrollment 保存、启用、成功登录、恢复码轮换和停用与对应成功审计同事务；审计失败时 enrollment、凭证、恢复码、counter、session 和撤销状态全部回滚。
- [ ] Passkey 注册、成功登录、重命名和删除与对应成功审计同事务；审计失败时凭证、counter/last-used、session 和其他认证撤销全部回滚。
- [ ] challenge 一次性消费和无效因子失败次数不因 denied 审计失败而回滚；审计不可用不得复活 challenge 或清零防爆破状态。
- [ ] 审计 detail、错误、日志和 UI 不出现 OAuth client secret、密码、OTP、恢复码、Cookie 或 Token。
- [ ] `/iam/audit-events` 匿名深链登录后回到原页面；桌面、详情和 390px 移动端无横向溢出或隐藏操作。
- [ ] `audit.anchor` 启用后，`POST /iam/audit-anchor` 使用真实 S3/MinIO object-lock 桶：锚对象以 COMPLIANCE 保留写入，未锁桶写入失败；同一 head 重复锚定幂等，已存在锚对象不允许覆盖或删除（write-once）。
- [ ] 篡改数据库事件、替换/伪造锚对象或把本地链回滚到已锚定 head 之后时，`GET /iam/audit-integrity` 的 `valid`/`anchor.valid` 为 `false`，且继续锚定返回失败（fail closed）；锚对象通过 `previous_anchor_hash` 链接成链。
- [ ] 未启用 `audit.anchor` 时，`POST /iam/audit-anchor` 返回 503 `audit_anchor_not_enabled`，完整性接口如实报告 `anchor.enabled:false`。归档/保留策略和完整合规治理不包含在本次验收内。

## 8. Web 安全

- [ ] 携带会话 Cookie 的写请求若 `Origin` 不在允许列表中，返回 403。
- [ ] Cookie 在生产环境启用 `Secure`、`HttpOnly` 和 `SameSite=Lax`。
- [ ] 登录返回地址和 OAuth redirect URI 均使用精确允许列表，不能构造开放重定向。
- [ ] `allowed_return_urls` 精确包含每个可直接访问的管理端路由，受保护深链登录后能返回原页面。
- [ ] 日志、错误响应和配置校验输出不包含密码、DSN、签名私钥、TOTP secret、恢复码或客户端密钥。

## 9. 数据库兼容性

- [ ] SQLite、MySQL、PostgreSQL 分别从空库完成 migration、bootstrap 和登录冒烟测试。
- [ ] 数据源只由配置中的 `database.type` 与 `database.dsn` / `dsn_file` 决定。
- [ ] 三种方言的唯一约束、外键、租户隔离和令牌轮换行为一致。

## 10. 结构约束

- [ ] `internal/app` 下没有 YAML 文件。
- [ ] 每个 `xx_test.go` 在同目录存在 `xx.go`。
- [ ] 不存在以其他文件名代替被测文件的测试布局。
- [ ] 生产代码、Go 依赖、Compose 和部署配置中不存在 Zitadel、SpiceDB、Authzed 或 Ory 运行依赖（门禁自动扫描）。

## 验收命令

```text
go test ./...
go vet ./...
go run ./cmd/chaosplus-server config validate -c deploy/compose/config.yaml
cd web/admin
bun install --frozen-lockfile
bun run lint
bun run typecheck
bun run test
bun run build
bun run audit:tenants -- 9333 http://127.0.0.1:8091
bun run audit:invitations -- 9333 http://127.0.0.1:8091
bun run audit:passkey -- 9333 http://localhost:8091

# 真实 MySQL / PostgreSQL 方言验收（一次性数据库，跑完全部方言测试）
$env:IAM_DB_LIFECYCLE_TYPE='mysql'
$env:IAM_DB_LIFECYCLE_ADMIN_DSN='root@tcp(127.0.0.1:3308)/'
go test ./internal/modules/iam ./internal/modules/provisioning ./internal/modules/federation ./internal/deployment -run 'DialectLifecycle|ProvisionAndLoginRealDialect|SCIMFilterDialectComparison' -count=1
$env:IAM_DB_LIFECYCLE_TYPE='postgres'
$env:IAM_DB_LIFECYCLE_ADMIN_DSN='postgres://postgres@127.0.0.1:5432/postgres?sslmode=disable'
go test ./internal/modules/iam ./internal/modules/provisioning ./internal/modules/federation ./internal/deployment -run 'DialectLifecycle|ProvisionAndLoginRealDialect|SCIMFilterDialectComparison' -count=1
```

> 通行密钥审计必须使用与 `authn.passkey.rp_id` 匹配的浏览器来源（本机验收配置为 `localhost`，因此 baseURL 用 `http://localhost:8091`）；其余审计使用 `127.0.0.1` 不受影响。
## 验收记录（2026-08-05/2026-08-06）

以下证据均在本仓库 HEAD `iam-ds` 上真实执行；条目对应上方清单各章节。人工/硬件类条目（TOTP 物理验证器、真实二维码扫码、真实邮箱供应商投递）仍标记为待验。

| 清单章节 | 验证方式 | 证据 |
| --- | --- | --- |
| 0 环境准备 | 本地 SQLite 实例运行中，`/.well-known/openid-configuration` 与 `/.well-known/jwks.json` 均 200；资源占用实测 | backend(18080) WS 117MB/21 线程；web(8091) WS 129MB；docs(4322) WS 63MB；通知接收器(18081) WS 55MB；MySQL(3308) WS 564MB；PostgreSQL(5432) WS 23MB |
| 1 本地身份与会话 | 真实 Chromium 浏览器审计（13 个 audit 脚本） | `audit:tenants`、`audit:invitations`、`audit:passkey` 等全部通过：登录/退出/深链回跳、无控制台错误、1440px/390px 无横向溢出；`TestSessionAndPasswordSecurityCenter` 覆盖会话列表、单会话撤销即时失效、无效撤销与密码修改约束 |
| 1.1 TOTP 与恢复码 | 真实后端测试 + 浏览器绑定截图 + RFC 6238 官方向量 | `internal/modules/authn/mfa_test.go`：`TestTOTPEnrollmentAndLogin`、`TestRecoveryCodesRegenerationAndDisable`、`TestMFAChallengeAttemptsAndExpiration`、`TestMFAAuditFailuresRollBackSecurityMutations`；新增 `TestTOTPGoogleGitHubCompatibleAlgorithm`（RFC 6238 Appendix B 官方向量，即 Google Authenticator 测试套件同源值：SHA1/6 位/30s，逐条断言生产库生成与 `validateTOTP` 接受）与 `TestTOTPProvisioningURIGoogleCompatible`（otpauth://totp/ URI 含 secret/SHA1/digits=6/period=30/issuer，secret 160 bit，独立流程完成绑定与 MFA 登录）；`.local/screenshots/admin-mfa-*` 绑定/轮换/停用截图 |
| 1.2 Passkey/WebAuthn | 真实 Chromium CTAP2 虚拟认证器（CDP WebAuthn） | `audit:passkey` 全流程：注册→重命名→无密码登录→删除→删除后拒绝；rp_id=localhost，来源匹配要求已写入验收命令 |
| 1.3 自助注册 | 真实浏览器 + Webhook 通知 + 直接查库 | `audit:registration`：验证 token 一次性消费、`activation_required` 清除、tenant 成员数 0、注册后登录成功 |
| 1.4/1.5 邮箱验证与密码找回 | 真实后端测试 + 注册审计的验证链 | `internal/modules/authn`：`TestEmailVerification*` 与 `TestPasswordRecovery*`（生命周期、过期、一次性、回滚、并发） |
| 2-6 主体/角色/实体/OAuth/SCIM | 真实浏览器审计 + 后端测试 + 实库过滤对比 | 13 个 audit 全部通过：租户生命周期、邀请、角色+数据范围、部门/岗位/用户组、实体关系、访问申请/复核、服务账号、SCIM 预配；截图见 `.local/screenshots/`；`TestSCIMFilterDialectComparison` 在 MySQL/PG 实库与 SQLite 得到相同过滤结果（startswith/contains/eq） |
| 7 审计日志 | 运行实例 API + 三方言测试 | `GET /iam/audit-integrity` valid=true（639 条事件链，head_sequence=639 且 head_hash 一致）；UPDATE/DELETE 拒绝由三方言实测覆盖（SQLite 见 audit_test.go；MySQL 8.0.42 与 PostgreSQL 17.5 实库跑 TestAuditAppendOnlyDialectLifecycle，UPDATE/DELETE 均被触发器拒绝）；`TestAuditExport*` 系列（`TestAuditExportIsVerifiedTenantSnapshot`、`TestAuditExportValidationAndFailures`、`TestAuditExportEmptyFilteredAndTamperedSnapshots`、`TestAuditExportWriterAndEncodingFailures`、`TestAuditExportRejectsInvalidRangeAndBrokenChain`）覆盖 manifest 固定 head_sequence/head_hash、请求期间新增事件不进快照、内容 SHA-256、空筛选/篡改/中断/错误 MIME 拒绝；`TestPrincipalSecurityStatesAndReconciliation` 覆盖邮箱更新与验证状态重置、credential version 递增、旧 session/token 撤销、recovery/verification token 消费、恢复不复活旧会话 |
| 8 Web 安全 | 真实请求 + 配置校验 | 坏 Origin 写请求返回 403；`cp_session` 实测 `HttpOnly; SameSite=Lax`；生产 TLS overlay（`compose.tls.yaml`）设 `AUTHN_WEB_COOKIE_SECURE=true`；`config validate` 输出无密码/DSN/密钥 |
| 9 数据库兼容性 | 真实 MySQL 8.0.42 / PostgreSQL 17.5 一次性数据库 | IAM/Provisioning/Federation `Test*MigrationDialectLifecycle` 全部 PASS（create→migrate→down→re-migrate）；`TestProvisionAndLoginRealDialect` 在 MySQL/PG 实库完成全模块迁移 + 幂等 bootstrap + 初始管理员密码登录 + session 认证 + 令牌轮换（凭据版本提升撤销旧 session/token）冒烟；`TestIAMConstraintBehaviorDialectLifecycle` 实库验证唯一约束、外键 RESTRICT/CASCADE、租户隔离与审计链唯一性；SQLite 由全量门禁覆盖 |
| 10 结构约束 | 门禁扫描 | `check-gates.ps1 -Scope all -Full` 通过：Go race 测试、90.0% 覆盖率、govulncheck 0 漏洞、vet、golangci-lint（11 linters，已接入本地门禁与 CI）、前端 lint/typecheck/真实测试/构建、文档构建/链接/Mermaid；门禁在 Windows PowerShell 5.1 下实测全绿（此前 stderr 被误报为失败的缺陷已修复并回归）；门禁新增外部 IAM/PDP 依赖扫描（Zitadel/SpiceDB/Authzed/Ory），go.mod、go.sum、deploy/、cmd/、internal/、pkg/ 全部零命中 |

TOTP 算法兼容性已由自动化测试替代真机验证（见 1.1 行证据）；仍待人工/硬件验证：真实邮箱供应商投递、实体/层级真机扫码。
