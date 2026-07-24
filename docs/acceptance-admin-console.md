# Admin console 人工验收清单

适用版本：`main`（authz 写入面 + admin console + 部署栈合入之后）。
前置：Docker 环境；按 `docs/deployment.md` 起本地栈。

## 0. 环境准备

```text
cd deploy/compose
./init-secrets.sh        # Windows: .\init-secrets.ps1
docker compose up -d
# chaosplus 启动时自动完成 Goose migration 和 Zitadel/SpiceDB provisioning
cd ../../web/admin && npm install && npm run dev
```

- [ ] `docker compose ps` 中长期服务全部 healthy，Chaosplus migration 日志成功
- [ ] API `GET /healthz`（或 `/authn/me` 返回 401）可访问

## 1. 注册与登录（Zitadel 托管）

- [ ] 打开控制台 `/register` → 跳到 Zitadel 注册页，能创建账号（密码/MFA 全在 Zitadel 侧）
- [ ] `/login` → Zitadel 登录 → 跳回控制台，右上角显示用户名
- [ ] DevTools：Application 中无任何 token（localStorage 只有 `chaosplus.tenant`）；`cp_session` cookie 为 HttpOnly
- [ ] 刷新页面会话保持（`GET /authn/session` 200）

## 2. 成员绑定（首个管理员需手工引导）

首个租户管理员关系需手工写入 SpiceDB（`tenant:<tid>#admin@user:<sub>`，
sub 见 `/authn/session` 响应），之后全部走界面。

- [ ] 未绑定成员的账号访问 `/iam/members` → 403，界面有可见错误提示（不是静默无反应）
- [ ] 管理员在"用户成员"绑定一个新 Zitadel subject → 列表出现、状态 active
- [ ] 停用该成员 → 该成员的会话再访问任意 `/iam/*` → 403（先于 SpiceDB 检查被拒）

## 3. 角色与权限（核心链路：403 → 授权 → 200，全程不重登）

- [ ] 新建角色，勾选若干权限（如 `role_view`、`menu_view`）
- [ ] 把角色授予成员 B；B **不重新登录**，几秒内（outbox 投递后）访问对应页面从 403 变 200
- [ ] 取消勾选权限 → B 再访问回到 403
- [ ] 删除角色 → B 的相关权限全部失效；删除失败时界面有错误提示
- [ ] 故意断开 SpiceDB（`docker compose stop spicedb`）→ 受守卫接口返回 503（fail-closed），界面提示错误；恢复后自动可用

## 4. 菜单

- [ ] 创建父/子菜单并绑定权限码；未持权限的成员侧边栏不显示对应项
- [ ] 菜单接口失败时侧边栏显示"菜单加载失败"，而不是显示全部管理入口
- [ ] 权限为空的成员看到"当前租户暂无可用菜单"

## 5. 租户切换与越权

- [ ] 顶栏把租户改成成员不属于的 `X-Tenant-Id` → 所有 `/iam/*` 403
- [ ] 直接用 curl 携带 cookie 但伪造 `Origin` 发 POST → 403（CSRF 拒绝）

## 6. 登出

- [ ] 点击退出 → 浏览器跳转 Zitadel end_session → 回到登录页
- [ ] 返回后 `GET /authn/session` 401；再点登录需要重新输入 Zitadel 凭据（SSO 会话已结束）

## 已知限制（验收时不算失败）

- 授权变更经 outbox 异步投递，生效有秒级延迟（`at_least_as_fresh` 收敛）
- 角色成员/权限列表暂无分页（文档已声明，P2+ 处理）
- e2e 目前仅覆盖登录页静态渲染，功能链路依赖本清单人工验收
