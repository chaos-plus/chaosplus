# Chaosplus 生产部署手册

应用 SQL migration 和 SpiceDB schema 已编译进 Go 二进制。Zitadel 自身数据库始终由
官方镜像的 `init/setup/start` 管理，不复制其内部 schema。

## 自动初始化范围

首次启动时，Compose 按以下顺序执行：

1. PostgreSQL 空卷初始化 `zitadel`、`spicedb`、`chaosplus` 三个隔离数据库及账号。
2. Zitadel 官方 `init`、`setup` 创建实例、首个人类管理员、bootstrap machine user 和 Login V2 client。
3. SpiceDB 官方 migration 执行到 `head`，随后启动服务。
4. `chaosplus-bootstrap` 获取数据库 advisory lock，执行内嵌的 dlock/wuid/IAM migration。
5. bootstrap 使用 machine JWT profile 的短期 token，幂等创建 Project 和 Native PKCE App。
6. bootstrap 写入生成的 SpiceDB schema，并把首个管理员绑定为初始 tenant admin。
7. API 只使用 DML 数据库账号启动；它不执行生产 migration，schema 不存在时直接退出。

生产初始化不会创建测试角色、测试菜单或测试用户。密码、MFA、登录会话和 token 全部属于
Zitadel；Chaosplus 数据库只保存 tenant membership 和业务侧展示信息。

## 单机快速启动

要求 Docker Engine 24+、Docker Compose v2.20+，本机 80 端口可用。

Windows PowerShell：

```powershell
cd deploy/compose
./init-secrets.ps1
docker compose --env-file .env -f compose.yaml up -d --build --wait
```

Linux：

```bash
cd deploy/compose
./init-secrets.sh
docker compose --env-file .env -f compose.yaml up -d --build --wait
```

打开：

- 管理端：`http://app.localhost`
- Zitadel Console：`http://auth.localhost/ui/console`
- API 文档：`http://app.localhost/docs`

首个账号为 `.env` 中的 `ZITADEL_FIRST_ADMIN_LOGIN`，密码由初始化脚本输出并写入
`ZITADEL_FIRST_ADMIN_PASSWORD`。首次登录必须修改密码。生成后的 `.env` 和 `secrets/`
已被 Git 忽略，必须纳入主机密钥备份，权限限制为部署账号可读。

```bash
docker compose --env-file .env -f compose.yaml ps -a
docker compose --env-file .env -f compose.yaml logs bootstrap zitadel-setup spicedb-migrate
```

## 公网 TLS

1. 将 `APP_DOMAIN`、`ZITADEL_DOMAIN` 改成两个已解析到服务器的真实域名。
2. 设置 `APP_URL=https://<APP_DOMAIN>`、`ZITADEL_URL=https://<ZITADEL_DOMAIN>`、
   `PUBLIC_SCHEME=https` 和 `ACME_EMAIL`。
3. 保持公网 80/443 可达，执行：

```bash
docker compose --env-file .env -f compose.yaml -f compose.tls.yaml up -d --build --wait
```

TLS 模式下 bootstrap 和 API 也通过同一个公网 issuer 访问 Zitadel。Traefik 在内部网络为公网
域名提供 alias，因此 JWT audience、OIDC issuer、Host/SNI 和浏览器地址一致，没有跳过证书校验的旁路。

## 配置覆盖

配置优先级为结构体默认值、YAML、环境变量、CLI 参数。基础文件为
`deploy/compose/chaosplus.yaml`。环境变量使用大写下划线形式，例如：

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

同一个值同时配置明文和 `_file` 会启动失败。文件必须是非空普通文件，不能超过限制。完整模板可
通过 `chaosplus-server config generate` 生成，并用 `config validate` 检查。

## 外部基础设施

可以单独运行两个命令：

```bash
chaosplus-bootstrap -c /etc/chaosplus/config.yaml
chaosplus-server -c /etc/chaosplus/config.yaml
```

使用外部服务时遵守以下边界：

- 应用数据库只选择一个 writable MySQL 或 PostgreSQL。
- bootstrap 使用 DDL/migration 账号；API 使用另一个仅有 DML 权限的账号。
- PostgreSQL migration owner 需设置 `ALTER DEFAULT PRIVILEGES`，为 runtime role 授予表的
  `SELECT/INSERT/UPDATE/DELETE` 和 sequence 的 `USAGE/SELECT`。
- MySQL migration 账号需 DDL；runtime 账号只授予目标库的 `SELECT, INSERT, UPDATE, DELETE`。
- 外部 Zitadel 可设置 `bootstrap.zitadel.enabled=false`，直接配置已有 project/client ID，
  并通过 `bootstrap.initial_admin.subject` 绑定已有 subject。
- 外部 SpiceDB 仍由 bootstrap 声明式写入生成 schema；API 的 `apply_schema` 保持 `false`。
- 应用改用 MySQL 时，Zitadel 和默认 SpiceDB 仍需要 PostgreSQL，除非它们也改为外部服务。

## 幂等与重跑

`chaosplus-bootstrap` 可重复执行。数据库 advisory lock 会跨进程、跨主机串行化整个流程；SQL
migration、SpiceDB schema/relationship、membership upsert 和 Zitadel lookup-or-create 均可收敛。

```bash
docker compose --env-file .env -f compose.yaml run --rm bootstrap
```

如果同名 Zitadel Project/App 出现多个，bootstrap 会拒绝猜测并退出。不要删除
`zitadel-bootstrap` volume：其中的 machine key 与 Zitadel 数据库是一组恢复资产。

`postgres/01-databases.sh` 只在 PostgreSQL **空数据卷首次启动**时执行。已有卷缺少账号或数据库
时不会自动修复，需由 DBA 创建后再重跑 bootstrap。不要用 `docker compose down -v` 处理普通
启动故障，该命令会删除全部持久数据。

## 备份、升级与回滚

至少备份：PostgreSQL 三个数据库、`postgres-data`、`redis-data`、`zitadel-bootstrap`、
`chaosplus-runtime`、部署 `.env` 和 `secrets/`。

升级前固定并修改 `.env` 中的镜像版本，先备份，再执行 `docker compose pull` 和 `up -d`。
Zitadel/SpiceDB 官方 migration 与 Chaosplus bootstrap 都必须成功后 API 才会启动。数据库 migration
通常不能仅通过回退镜像撤销；回滚应恢复升级前数据库和 volume 快照。

Zitadel bootstrap machine key 为高权限凭据且有到期时间。到期前按 Zitadel 官方流程轮换，监控
其使用，并同步更新 `zitadel-bootstrap` volume 备份。运行期 API 不挂载该 key。
