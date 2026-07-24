# Chaosplus 生产部署手册

应用 SQL migration 和 SpiceDB schema 已编译进 Go 二进制。Zitadel 自身数据库始终由
官方镜像的 `init/setup/start` 管理，不复制其内部 schema。

## 自动初始化范围

首次启动时，Compose 按以下顺序执行：

1. PostgreSQL 空卷初始化 `zitadel`、`spicedb`、`chaosplus` 三个隔离数据库及账号。
2. Zitadel 官方 `init`、`setup` 创建实例、首个人类管理员、bootstrap machine user 和 Login V2 client。
3. SpiceDB 官方 migration 执行到 `head`，随后启动服务。
4. `chaosplus` 进程启动时自动获取数据库 advisory lock，对内嵌的 dlock/wuid/IAM Goose
   migrations 执行 `up`。
5. migration 完成后，进程幂等创建 Zitadel Project/Native PKCE App，更新 SpiceDB schema，
   并绑定初始 tenant admin。
6. 全部完成后 Chaosplus 才监听 HTTP/gRPC 端口；任一步失败都会退出并由 Compose 报错。

生产初始化不会创建测试角色、测试菜单或测试用户。密码、MFA、登录会话和 token 全部属于
Zitadel；Chaosplus 数据库只保存 tenant membership 和业务侧展示信息。

## 单机快速启动

要求 Docker Engine 24+、Docker Compose v2.20+，本机 80 端口可用。首次部署或从备份恢复时
先生成部署密钥；以后启动、升级和重启都不再执行密钥脚本。

Windows PowerShell：

```powershell
cd deploy/compose
./init-secrets.ps1
docker compose up -d --build --wait
```

Linux：

```bash
cd deploy/compose
./init-secrets.sh
docker compose up -d --build --wait
```

正常部署只有一条启动命令：

```bash
docker compose up -d --build --wait
```

Compose 自动执行 `postgres → zitadel/spicedb → chaosplus（Goose up → provision → serve）`，
不需要手工运行任何 init、setup 或 migration 容器。

打开：

- 管理端：`http://app.localhost`
- Zitadel Console：`http://auth.localhost/ui/console`
- API 文档：`http://app.localhost/docs`

首个账号为 `.env` 中的 `ZITADEL_FIRST_ADMIN_LOGIN`，密码由初始化脚本输出并写入
`ZITADEL_FIRST_ADMIN_PASSWORD`。首次登录必须修改密码。生成后的 `.env` 和 `secrets/`
已被 Git 忽略，必须纳入主机密钥备份，权限限制为部署账号可读。

```bash
docker compose ps -a
docker compose logs chaosplus zitadel-setup spicedb-migrate
```

## 公网 TLS

1. 将 `APP_DOMAIN`、`ZITADEL_DOMAIN` 改成两个已解析到服务器的真实域名。
2. 设置 `APP_URL=https://<APP_DOMAIN>`、`ZITADEL_URL=https://<ZITADEL_DOMAIN>`、
   `PUBLIC_SCHEME=https` 和 `ACME_EMAIL`。
3. 保持公网 80/443 可达，执行：

```bash
docker compose -f compose.yaml -f compose.tls.yaml up -d --build --wait
```

TLS 模式下 migration 和 Chaosplus 也通过同一个公网 issuer 访问 Zitadel。Traefik 在内部网络为公网
域名提供 alias，因此 JWT audience、OIDC issuer、Host/SNI 和浏览器地址一致，没有跳过证书校验的旁路。

## 配置覆盖

默认 Compose 配置已通过 `go:embed` 编译进 `chaosplus-server`。`deploy/compose/compose.yaml` 只需设置
`CHAOSPLUS_CONFIG_PRESET=compose`，不再挂载额外的应用 YAML。配置优先级为结构体默认值、
内置预设、显式 YAML、环境变量、CLI 参数。环境变量使用大写下划线形式，例如：

```text
AUTHN_HTTP_TIMEOUT=15s
RATELIMIT_IP_RATE=500
BOOTSTRAP_INITIAL_ADMIN_TENANT_ID=platform
```

敏感值优先使用文件配置：

- `database.<name>.dsn_file`
- `bootstrap.database.dsn_file`
- `redis.password_file`
- `authz.spicedb.token_file`
- `authn.web.encryption_key_file`
- `authn.web.login_client_token_file`（仅启用 Web 账号密码直登时需要）

同一个值同时配置明文和 `_file` 会启动失败。文件必须是非空普通文件，不能超过限制。完整模板可
通过 `chaosplus-server config generate` 生成，并用 `config validate` 检查。

## 外部基础设施

不使用仓库内 Compose 时，可以只维护一个显式 YAML，由同一个服务进程在监听端口前完成
migration 和 provisioning：

```bash
chaosplus-server -c /etc/chaosplus/config.yaml
```

使用外部服务时遵守以下边界：

- 应用数据库只选择一个 writable MySQL 或 PostgreSQL。
- migration 使用 DDL 账号；业务数据库连接使用另一个仅有 DML 权限的账号。启用启动时自动
  migration 的进程需要能读取前者，但业务查询不会复用该连接。
- PostgreSQL migration owner 需设置 `ALTER DEFAULT PRIVILEGES`，为 runtime role 授予表的
  `SELECT/INSERT/UPDATE/DELETE` 和 sequence 的 `USAGE/SELECT`。
- MySQL migration 账号需 DDL；runtime 账号只授予目标库的 `SELECT, INSERT, UPDATE, DELETE`。
- 外部 Zitadel 可设置 `bootstrap.zitadel.enabled=false`，直接配置已有 project/client ID，
  并通过 `bootstrap.initial_admin.subject` 绑定已有 subject。
- 外部 SpiceDB 仍由 bootstrap 声明式写入生成 schema；API 的 `apply_schema` 保持 `false`。
- 应用改用 MySQL 时，Zitadel 和默认 SpiceDB 仍需要 PostgreSQL，除非它们也改为外部服务。

## Migration 与重跑

Chaosplus 每次启动都会自动执行嵌入二进制的 Goose `up`。Goose version table 会跳过已应用版本，
数据库 advisory lock 会跨进程、跨主机串行化 migration；之后的 SpiceDB schema/relationship、
membership upsert 和 Zitadel lookup-or-create 也都是幂等操作。

```bash
docker compose restart chaosplus
```

普通升级不需要单独执行 migration。需要主动回滚数据库时，使用同一个镜像和同一个 Go 二进制；
由于 dlock、wuid、IAM 使用独立 Goose version table，必须明确目标模块：

```bash
docker compose run --rm chaosplus migration down iam
docker compose run --rm chaosplus migration down-to iam 1
```

回滚完成后再部署对应旧版本镜像。不要让旧镜像猜测并自动执行 `down`。

如果同名 Zitadel Project/App 出现多个，bootstrap 会拒绝猜测并退出。不要删除
`zitadel-bootstrap` volume：其中的 machine key 与 Zitadel 数据库是一组恢复资产。

`postgres/01-databases.sh` 只在 PostgreSQL **空数据卷首次启动**时执行。已有卷缺少账号或数据库
时不会自动修复，需由 DBA 创建后再重跑 bootstrap。不要用 `docker compose down -v` 处理普通
启动故障，该命令会删除全部持久数据。

## 备份、升级与回滚

至少备份：PostgreSQL 三个数据库、`postgres-data`、`redis-data`、`zitadel-bootstrap`、
`chaosplus-runtime`、部署 `.env` 和 `secrets/`。

升级前固定并修改 `.env` 中的镜像版本，先备份，再执行 `docker compose pull` 和 `up -d`。
Zitadel/SpiceDB 官方 migration 与 Chaosplus 内嵌 Goose migration 都必须成功后服务才会启动。数据库 migration
通常不能仅通过回退镜像撤销；回滚应恢复升级前数据库和 volume 快照。

Zitadel bootstrap machine key 为高权限凭据且有到期时间。到期前按 Zitadel 官方流程轮换，监控
其使用，并同步更新 `zitadel-bootstrap` volume 备份。启用自动 provisioning 的 Chaosplus 进程需要
以只读方式访问该 key。
