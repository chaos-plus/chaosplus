# Chaosplus Repository Facts

This file is generated from repository manifests and source trees. Do not edit it manually.

## Backend

- Go module: `github.com/chaos-plus/chaosplus`
- Go version: `1.26.5`
- Go package directories: 41
- Feature modules: audit, authn, federation, governance, iam, identity, oauth, organization, provisioning
- IAM SQL dialects: mysql, postgres, sqlite
- HTTP framework: Huma v2 on chi
- Persistence: Bun plus Goose
- Primary configuration: `internal/app/config.go`
- Composition root: `internal/app`

## Frontend

- Workspace: `chaosplus-admin`
- Package manager: `bun@1.3.12`
- Workspaces: apps/*, packages/*
- Application: `web/admin/apps/web` (React, Vite, TypeScript)
- Shared UI: `web/admin/packages/ui`
- App test command: `bun test src`

## Documentation

- Package: `chaosplus-docs`
- Site: `web/docs`
- Generator: Astro ^7.0.2
- Theme: Starlight ^0.41.3
- Authoritative engineering sources: `README.md` and `docs/*.md`

## Required Invariants

- Database configuration uses `type` plus `dsn` or `dsn_file` for SQLite, MySQL, and PostgreSQL.
- No YAML belongs under `internal/app`.
- Every Go `name_test.go` has sibling `name.go`.
- Tests use real dependencies and real listeners; mocks, fakes, stubs, and miniredis are forbidden.
- Full Go acceptance coverage is at least 90%.
- Future business hierarchy is tenant -> entity -> business resources.
