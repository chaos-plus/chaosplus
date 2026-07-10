# Chaosplus API

Go backend for Chaosplus, exposing an [OpenAPI 3.1](https://spec.openapis.org/oas/v3.1.0)
API built with [huma v2](https://github.com/danielgtaylor/huma) running on the
[GoFr](https://gofr.dev) framework.

## Highlights

- **huma + GoFr bridge** — a custom huma adapter (`pkg/extension/huma/adapters`)
  lets huma operations run on GoFr, which owns the server lifecycle, structured
  logging, and graceful shutdown.
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
`http://localhost:8000`.

- API docs: `http://localhost:8000/docs`
- OpenAPI spec: `http://localhost:8000/openapi.json` (and `.yaml`)

## Configuration

Configuration is read from environment variables or `configs/.env` (GoFr
convention):

| Variable | Default | Description |
|----------|---------|-------------|
| `HTTP_PORT` | `8000` | HTTP listen port |
| `DOCS_RENDERER` | `all` | Docs UI: `all` (tabbed), `scalar`, `swagger-ui`, `stoplight`, or `none` |
| `SHUTDOWN_GRACE_PERIOD` | `30s` | Graceful shutdown timeout |

## Test

```bash
go test -race ./...
```
