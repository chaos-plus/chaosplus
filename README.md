# Chaosplus API

Go backend for Chaosplus, exposing an [OpenAPI 3.1](https://spec.openapis.org/oas/v3.1.0)
API built with [huma v2](https://github.com/danielgtaylor/huma) served over a
[chi](https://github.com/go-chi/chi) router.

## Highlights

- **huma on chi** — huma operations are mounted on a chi router via the official
  `humachi` adapter. The application (`internal/app`) owns its own server
  lifecycle, structured logging (`log/slog` via `pkg/logx`) and graceful
  shutdown; no external web framework.
- **koanf configuration** — layered config (struct defaults → `configs/config.yaml`
  → `CP_`-prefixed environment overrides), see `internal/app/config.go`.
- **Multi-renderer API docs** — a tabbed `/docs` page hosting five renderers
  (Scalar, Swagger UI, ReDoc, Stoplight, openapi-ui), each also reachable at its
  own path (`/docs/scalar`, `/docs/redoc`, …). See `pkg/extension/huma/docs`.

## Requirements

- Go 1.26+

## Run

```bash
go run ./cmd/chaosplus-server
```

The server logs its listening address on startup. By default it serves on
`http://localhost:8080`.

- API docs: `http://localhost:8080/docs`
- OpenAPI spec: `http://localhost:8080/openapi.json` (and `.yaml`)

## Configuration

Spring Boot-style layered configuration, lowest to highest precedence:

1. struct defaults
2. `configs/application.yaml` (base)
3. `configs/application-<profile>.yaml` (active profile, selected by `CP_PROFILE`)
4. `CP_`-prefixed environment variables

The config directory defaults to `./configs` (override with `CONFIG_DIR`).
`.env` and `configs/.env` are loaded into the environment first. Environment
overrides map `_` to config nesting, e.g. `CP_SERVER_PORT` sets `server.port`.

```bash
# run with the dev profile (loads application-dev.yaml over application.yaml)
CP_PROFILE=dev go run ./cmd/chaosplus-server
```

| Variable | Default | Description |
|----------|---------|-------------|
| `CP_PROFILE` | _(none)_ | Active profile, e.g. `dev`, `prod` |
| `CONFIG_DIR` | `configs` | Directory searched for config files |
| `CP_SERVER_HOST` | `0.0.0.0` | HTTP listen host |
| `CP_SERVER_PORT` | `8080` | HTTP listen port |
| `CP_LOGGER_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `DOCS_RENDERER` | `all` | Docs UI: `all` (tabbed), `scalar`, `swagger-ui`, `stoplight`, or `none` |

Databases are configured under `databases:` in the YAML file (each with `name`,
`dialect`, `dsn`); with none configured a local SQLite database is used. The
first database is the primary — migrations and the worker-id lease run against
it.

## Test

```bash
go test -race ./...
```
