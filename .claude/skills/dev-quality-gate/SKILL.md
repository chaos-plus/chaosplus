---
name: dev-quality-gate
description: Route Dev repository work to the correct domain skill and enforce repository-wide quality gates. Use for any implementation, refactor, review, test, release, architecture, API, frontend, documentation, deployment, or skill change in this repository, especially when changed paths span multiple domains or acceptance readiness must be assessed.
---

# Dev Quality Gate

Act as the repository-wide router and final acceptance gate. Domain skills guide implementation; this skill determines which ones apply and refuses unsupported quality claims.

**Canonical rules:** `.rules/3.ARCH.md` (architecture, cloud persistence, multi-machine dispatch), `.rules/3.API.md` (REST conventions, auth), `.rules/3.TEST.md` (test methodology), `.rules/4.PRD_TEMPLATE.md` (spec format). All domain skills SHOULD reference these instead of duplicating. When a decision changes a rule, update `.rules/` → note in `MEMORY.md` → skills pick it up automatically.

## Refresh Context First

Run:

```text
python .claude/skills/dev-quality-gate/scripts/skill-runtime.py refresh
```

Read `references/repository-facts.md`. The script derives stable facts from actual manifests and source trees and writes only when content changes. Do not manually add transient facts to that file.

## Route By Changed Surface

Use every matching domain skill:

| Changed surface | Required skill |
| --- | --- |
| `cmd/**`, `internal/**`, `pkg/**`, `go.mod`, `go.sum`, backend deployment | `$dev-backend` |
| `apps/admin/**`, frontend container or proxy | `$dev-frontend` |
| `apps/docs/**`, `README.md` | `$dev-docs` |
| `.claude/**`, `AGENTS.md`, `.github/**`, cross-domain release | `$dev-quality-gate` plus every affected domain |

When a contract crosses domains, inspect the producer first, then the consumers. For example, backend OpenAPI precedes the frontend API client and public docs.

## Apply Non-Negotiable Gates

- Preserve user changes and use repository patterns.
- Reject YAML files under `internal/app`.
- Reject a Go `name_test.go` without sibling `name.go`.
- Reject tests that use mocks, fakes, stubs, miniredis, monkey patching, or fixture interception as substitutes for real dependencies.
- Require SQLite, MySQL, and PostgreSQL compatibility for shared persistence behavior; state clearly which live dialects were actually exercised.
- Require at least 90% real Go coverage for a full acceptance claim.
- Require accurate OpenAPI operation IDs, summaries, tags, response envelopes, authentication, and tenant authorization.
- Require frontend lint, typecheck, real tests, production build, responsive inspection, and deployability.
- Require documentation sync, internal-link validation, navigation integrity, Mermaid rendering, and production build.
- Never claim a feature, database, workflow, browser, or deployment was verified when it was not run.

Run path-aware checks during work:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .claude/skills/dev-quality-gate/scripts/check-gates.ps1
```

Run all release gates before an acceptance or open-source-ready claim:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .claude/skills/dev-quality-gate/scripts/check-gates.ps1 -Scope all -Full
```

Do not remove a check, exclude production packages, suppress a warning, reduce coverage scope, or alter a threshold merely to pass.

## Controlled Self-Learning

Use `scripts/skill-runtime.py record` only after a failure has been reproduced and the fix has passed its proving test or gate. The command routes the entry to the relevant domain's `references/lessons.md`, removes line breaks, rejects likely secrets, locks concurrent writes, and deduplicates by content.

Each entry must contain:

- Observable symptom.
- Confirmed root cause.
- General prevention rule.
- Test, command, screenshot, or artifact that proves the rule.

Never auto-edit `SKILL.md`, gate scripts, security invariants, thresholds, architecture decisions, or dependency policy from a lesson. Such changes require ordinary reviewed code changes and all applicable gates.

## Finish With Evidence

Report the commands run, their outcomes, coverage percentage when claimed, untested external systems, and any remaining gap. A partial pass is useful evidence but is not full acceptance.
