# Contributing

Chaosplus IAM is developed on the `iam-ds` branch and guarded by repository
quality gates. Pull requests must keep every gate green.

## Repository layout

- `apps/server/` — Go backend. Domain code lives in
  `apps/server/internal/modules/<module>`; every module registers its own i18n catalog.
- `apps/admin` — React admin console (Bun/Turborepo workspace).
- `apps/docs/` — documentation site (Astro/Starlight).
- `.claude/skills/` — agent skills; the quality gate is
  `.claude/skills/dev-quality-gate`.

## Rules enforced by the gate

- Tests must use real dependencies; mocks, fakes, stubs, and miniredis are
  forbidden.
- Every `xx_test.go` must have a sibling production file `xx.go`.
- No YAML files under `internal/app`.
- The data source comes only from `database.type` + `database.dsn` /
  `dsn_file`; there is no `CHAOSPLUS_DATABASE_TYPE`.
- All user-facing errors must have clear, stable messages in
  `en-US`, `zh-CN`, and `ms-MY`.
- Repository test coverage must stay at or above 90%.

## Before opening a pull request

Run the full gate locally and fix every failure:

```powershell
$env:GOSUMDB='sum.golang.org'
.\.claude\skills\dev-quality-gate\scripts\check-gates.ps1 -Scope all -Full
```

`golangci-lint` (`.golangci.yml`) and `govulncheck` are part of the gate.
Commit messages use a conventional prefix (`feat`, `fix`, `test`, `docs`,
`ci`).
