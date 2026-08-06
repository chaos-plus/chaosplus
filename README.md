# Chaosplus API

IAM 的纯自研目标架构、模块边界、数据模型和迁移计划见
[docs/iam-platform-architecture.md](docs/iam-platform-architecture.md)。
访问申请、四眼审批与临时授权契约见
[docs/access-governance.md](docs/access-governance.md)。

Production bootstrap, Docker Compose quick start, TLS, and external-service
configuration are documented in [docs/deployment.md](docs/deployment.md).

Go backend for Chaosplus. It exposes an [OpenAPI 3.1](https://spec.openapis.org/oas/v3.1.0)
HTTP API built with [huma v2](https://github.com/danielgtaylor/huma) served over a
[chi](https://github.com/go-chi/chi) router, plus a gRPC server on a separate port.

## Highlights

- **huma on chi + gRPC** — huma operations are mounted on a chi router via the
  official `humachi` adapter. The application (`internal/app`) owns its own
  lifecycle, structured logging (`log/slog`), and graceful shutdown for both the
  REST (`:8080`) and gRPC (`:9090`, reflection enabled) servers; no external web
  framework.
- **Uniform response envelope** — every response, success or error, is shaped as
  `{code, message, meta, data}` by `internal/core/extension/humax/respx`. The
  `message` is an i18n key localized per request; timestamps are UTC.
- **Internationalized messages** — `pkg/i18n` resolves the request locale from
  `?lang=` → `X-Lang` → `Accept-Language`, normalized to a supported locale
  (`en-US`, `zh-CN`, `ms-MY`; fallback `en-US`). A huma transformer localizes the
  envelope message for success, business, and framework errors alike. Public
  business errors are owned by each feature module's `i18n/locales` directory;
  startup and repository gates require complete key parity across all locales.
- **Multi-database via bun + goose** — datasources (`internal/core/extension/bunx`)
  support SQLite, MySQL, and PostgreSQL with read/write routing; schema migrations
  run per module via goose (`internal/core/extension/goosex`).
- **Multi-renderer API docs** — a tabbed `/docs` page hosting five renderers
  (Scalar, Swagger UI, ReDoc, Stoplight, openapi-ui), each also reachable at its
  own path. See `internal/core/extension/humax/docs`.
- **Self-hosted IAM** — local principals, Argon2id credentials, opaque browser
  sessions, email-verified self-service registration without implicit tenant
  membership, tenant service accounts with one-time client credentials, Ed25519
  JWT/JWKS, OAuth2/OIDC, tenant RBAC, generic entity scopes,
  department, position, static group, and rule-driven dynamic group directories, direct/group/position role assignments,
  tenant-scoped entity and business-resource relationship grants, revision-stable data constraints and authorization explanations,
  access requests with four-eyes approval, time-limited role grants, direct/temporary grant reviews, menus, OAuth clients, and audit events
  use the configured primary database. Native SCIM 2.0 inbound provisioning provides
  tenant-bound directories, hashed Bearer credentials, Users, Groups, PATCH, Bulk,
  filtering, discovery, ETags, deprovisioning, and transactional audit; see
  [docs/scim-provisioning.md](docs/scim-provisioning.md).
- **Feature modules** — the composition root (`internal/app/modules.go`) wires
  identity, authn, OAuth, IAM, organization, provisioning, governance, audit, GUID and GeoIP modules.
  Supporting infrastructure includes WUID worker leases and distributed
  deployment locks.

## Requirements

- Go 1.26+

## Run

```bash
go run ./cmd/chaosplus-server
```

The server logs its listening addresses on startup. By default REST serves on
`http://localhost:8080` and gRPC on `:9090`.

```bash
# generate an annotated config template, then run with it
go run ./cmd/chaosplus-server config generate -o config.yaml
go run ./cmd/chaosplus-server -c config.yaml
```

The `config` command has two subcommands, both driven by `internal/app/config.go`
so they can never drift from the schema:

- `config generate [-o FILE]` — write a template with every key, its default, and
  a description comment (`-o` defaults to `config.yaml`).
- `config validate [-c FILE]` — load a config file strictly; unknown or misspelled
  keys are reported as errors.

## Endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /docs` | Tabbed API docs (also `/docs/scalar`, `/docs/swagger`, `/docs/redoc`, `/docs/stoplight`, `/docs/openapi-ui`) |
| `GET /openapi.json`, `GET /openapi.yaml` | OpenAPI 3.1 spec |
| `GET /guid` | Next generated id |
| `GET /guid/{count}` | A batch of `count` new ids |
| `GET /geoip` | Detect the caller's IPv4 and 307-redirect to its lookup |
| `GET /geoip/{ip}` | Geolocation for an IPv4 address, one entry per provider |

## Configuration

Configuration is loaded by `pkg/configurator` (viper + cobra flags). The struct
in `internal/app/config.go` is the source of truth for every key and default.
Values are resolved as: **struct defaults → YAML config file (`-c/--config`) →
environment variables → CLI flags** (later wins). Every config field is also a
CLI flag; run with `--help` to list them.

Main settings (YAML path → default):

| Key | Default | Description |
|-----|---------|-------------|
| `name` | _(empty)_ | Application name |
| `debug` (`-d`) | `false` | Debug mode; uses an in-memory SQLite database |
| `timezone` | `UTC` | Process timezone; timestamps are emitted in UTC regardless |
| `worker_lease` | `3600` | GUID worker-id lease seconds (heartbeat renews at a third of this) |
| `rest.host` / `rest.port` | `0.0.0.0` / `8080` | REST listen address |
| `grpc.host` / `grpc.port` | `0.0.0.0` / `9090` | gRPC listen address |
| `log.file` / `log.level` / `log.format` | `logs/app.log` / `info` / `json` | Logging (empty `file` = stdout only) |
| `redis.addrs` / `redis.master_name` | _(empty)_ | Redis client (see Rate limiting); empty `addrs` disables Redis |
| `ratelimit.enabled` | `false` | Per-IP / per-account rate limiting (see below) |
| `database` | _(none)_ | Map of datasources (see below) |
| `geoip` | _(none)_ | GeoIP provider settings |

### Rate limiting

When `ratelimit.enabled` is true and Redis is configured, a Redis-backed limiter
(GCRA token bucket) enforces independent **per-IP** and **per-account** dimensions;
exceeded requests get a localized `429` with `Retry-After` and `X-RateLimit-*`
headers. The account id comes from the `ratelimit.account.header` request header
(default `X-Account-Id`); anonymous requests skip the account dimension. It fails
open — if Redis is unavailable, requests are allowed and a warning is logged.

Redis supports standalone, sentinel, and cluster deployments via one `redis.addrs`
list plus `redis.master_name`:

```yaml
redis:
  addrs: ["127.0.0.1:6379"]   # one = standalone; many = cluster; sentinel addrs + master_name = sentinel
  master_name: ""             # set (e.g. mymaster) to use sentinel/failover
ratelimit:
  enabled: true
  ip:      { enabled: true, rate: 100, period: 1m, burst: 20 }
  account: { enabled: true, rate: 600, period: 1m, burst: 60, header: X-Account-Id }
```

### Databases

`database` is a map of named datasources, each a `bunx.Datasource`:

```yaml
database:
  primary:
    type: mysql          # sqlite | mysql | postgres
    dsn: "user:pass@tcp(127.0.0.1:3306)/chaosplus?parseTime=true&loc=UTC"
    writable: true
    readable: true
```

The first writable datasource is the primary — migrations and the GUID worker-id
lease run against it. With no writable database configured, the `guid` module is
skipped and the app still serves endpoints that don't need one. In debug mode an
in-memory SQLite database is used.

### GeoIP endpoints

GeoIP providers use their public upstream endpoints by default. Trusted
deployments can route lookups and database downloads through internal mirrors
without replacing HTTP transports in code:

```yaml
geoip:
  geolite2:
    api_base_url: "https://github-api.example.invalid"
  ip2location:
    token: "replace-with-secret"
    download_base_url: "https://geoip-mirror.example.invalid/ip2location"
  ipapi:
    base_url: "https://geoip-mirror.example.invalid/ipapi"
```

These endpoint values are trusted operator configuration. IP2Location tokens
are added as query parameters only for the outbound request and are never
written to application logs.

## Project layout

```
cmd/chaosplus-server        # entry point (cobra root command)
internal/
  app/                      # composition root: config, lifecycle, REST + gRPC servers, modules
  modules/                  # identity, authn, oauth, iam, organization, provisioning, governance, audit
  core/extension/           # framework adapters
    humax/respx             # uniform {code,message,meta,data} envelope + i18n
    humax/docs              # multi-renderer OpenAPI docs
    bunx                    # bun datasources + read/write routing
    goosex                  # per-module goose migrations
  infra/                    # feature & infra modules: guid, geoip, wuid, dlock
pkg/                        # reusable libraries
  configurator  i18n  geoip  interpreter  sysinfo  timezone  utils
```

## Test

```bash
go test -race ./...
```

The repository gate is the authoritative quality check. On Windows PowerShell:

```powershell
$env:GOSUMDB='sum.golang.org'
.\.agents\skills\chaosplus-quality-gate\scripts\check-gates.ps1 -Scope all -Full
```

It enforces Go race tests with a repository-wide **90% statement coverage
threshold**, `golangci-lint`, `govulncheck`, `go vet`, formatting, module i18n
parity (`en-US` / `zh-CN` / `ms-MY`), mock-free tests (real dependencies only),
test/production file pairing, and frontend lint/typecheck/tests/build plus the
documentation build and link check. The same checks run in CI
(`.github/workflows/quality.yml`), including real MySQL 8.4 and PostgreSQL 17.5
migration, constraint, and SCIM filter comparisons.

## Contributing

See `CONTRIBUTING.md` at the repository root for the layout, enforced
rules, and how to run the full gate before opening a pull request.

## Security

Report vulnerabilities through GitHub private vulnerability reporting. See
`SECURITY.md` at the repository root for what to include and how reports are handled.
