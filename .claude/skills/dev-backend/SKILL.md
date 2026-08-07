---
name: dev-backend
description: Develop, review, test, and refactor the Dev Go backend and IAM APIs. Use for changes under cmd, internal, pkg, Go modules, SQL migrations, OpenAPI contracts, authentication, authorization, tenancy, database support, runtime plugins, deployment bootstrap, or backend tests.
---

# Dev Backend

Build backend changes that preserve the repository's modular Go architecture and self-hosted IAM guarantees.

## Start With Repository Truth

1. Run `python .claude/skills/dev-quality-gate/scripts/skill-runtime.py refresh` from the repository root.
2. Read `../dev-quality-gate/references/repository-facts.md`.
3. Read the nearest production files and their exact-name tests before editing.
4. Read the relevant architecture document under `docs/`; use `docs/iam-platform-architecture.md` for IAM boundaries.
5. Read `references/lessons.md` when the task touches a previously recorded failure class.

Never infer a contract from test names, old deployment files, or copied projects when production code provides the answer.

## Preserve Boundaries

- Keep `internal/app` as the composition root. Do not put business policy, migrations, or YAML configuration there.
- Keep a feature in its owning `internal/modules/<module>` package. Prefer `domain`, `service`, `repository`, `api`, and `sql/<dialect>` boundaries already present in that module.
- Keep framework adapters in `internal/core/extension`; keep generally reusable libraries in `pkg`.
- Keep API transport DTOs separate from persistence models when their lifecycle or exposure differs.
- Add an abstraction only for a real alternate implementation or to remove meaningful duplication.
- Use a WASM extension point only for explicitly untrusted, runtime-loaded policy behavior. Keep authentication, authorization enforcement, credential storage, and tenant isolation in compiled trusted code.

## Enforce Data And IAM Invariants

- Configure every datasource with the existing `type` plus `dsn` or `dsn_file` contract. Supported types are `sqlite`, `mysql`, and `postgres`; do not invent environment selectors such as `CHAOSPLUS_DATABASE_TYPE`.
- Keep equivalent migrations for all three dialects. Use parameterized queries and transactions for multi-write invariants.
- Treat tenant scope as a mandatory authorization boundary. A future entity scope is subordinate to a tenant and must never weaken tenant isolation.
- Model global principals separately from tenant memberships and entity-scoped business relationships.
- Recheck mutable principal and session state during authentication where the current design requires immediate revocation.
- Store passwords with the repository password helper, enforce history in the same transaction, and never log credentials, tokens, DSNs, or recovery material.
- Keep OAuth/OIDC behavior standards-based: exact redirect matching, PKCE, one-time codes, refresh rotation, revocation, issuer consistency, and minimal claims.
- Preserve the Huma OpenAPI contract and the `{code,message,meta,data}` response envelope.

## Implement In A Narrow Vertical Slice

1. Define or update domain invariants.
2. Add repository behavior and all three SQL dialect changes when persistence changes.
3. Add service orchestration and authorization checks.
4. Expose the smallest complete API operation with stable operation ID, summary, tags, status codes, and schemas.
5. Wire the module only at the composition root.
6. Add real tests through production constructors, real SQLite or reachable database servers, and real HTTP listeners when transport behavior matters.

Do not use mocks, fakes, stubs, miniredis, monkey patching, or test-only production branches. Every `name_test.go` must test a sibling `name.go`; rename or reorganize tests rather than creating unrelated test filenames.

## Verify And Learn

Run the backend gate:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .claude/skills/dev-quality-gate/scripts/check-gates.ps1 -Scope backend
```

For a release or coverage claim, run with `-Full`. The full gate requires repository Go coverage of at least 90%, race detection, vet, vulnerability scanning, OpenAPI checks, and the structural rules above.

After fixing a verified failure that is likely to recur, record only the reusable rule:

```text
python .claude/skills/dev-quality-gate/scripts/skill-runtime.py record --domain backend --symptom "..." --cause "..." --prevention "..." --evidence "test or gate output"
```

Do not record guesses, secrets, task narration, or one-off business decisions. Never lower a gate to make a change pass.
