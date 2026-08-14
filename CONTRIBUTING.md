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

- Project tests and self-checks must exercise real internal implementations;
  mocks, fakes, stubs, protocol simulators, and test-only product branches are
  forbidden. Isolated Go tests may use miniredis as a lightweight external Redis
  implementation for local feedback, but production Go files cannot import it
  and real Redis release acceptance remains mandatory.
- Every `xx_test.go` must have a sibling production file `xx.go`.
- No YAML files under `internal/app`.
- The data source comes only from `database.type` + `database.dsn` /
  `dsn_file`; there is no `CHAOSPLUS_DATABASE_TYPE`.
- All user-facing errors must have clear, stable messages in
  `en-US`, `zh-CN`, and `ms-MY`.
- Repository test coverage must stay at or above 90%.

## Before opening a pull request

Run the full gate locally and fix every failure:

```bash
# macOS / Linux
python3 .claude/skills/dev-quality-gate/scripts/check_gates.py --scope all --full

# Windows
py -3 .claude/skills/dev-quality-gate/scripts/check_gates.py --scope all --full
```

`staticcheck`, `golangci-lint` (`.golangci.yml`), and `govulncheck` are installed
at pinned versions when missing and are part of the gate.
Commit messages use a conventional prefix (`feat`, `fix`, `test`, `docs`,
`ci`).

When a contributor explicitly needs to preserve or share incomplete work while
gates are blocked, `.rules/3.TEST.md` allows a `WIP:` commit only on a
non-protected, non-default development branch. The commit body must record the
exact failed and unrun gates and state that it is not merge/release ready.
Architecture, schema, test-policy, secret, conflict, diff-hygiene, and security
checks remain mandatory; WIP commits cannot be tagged, released, force-pushed,
or marked ready for review.
