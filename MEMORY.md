# MEMORY.md

Cross-session memory for chaosplus. Read this first on a new session/device. Keep concise — full rule bodies live in `.rules/`, this file only holds pointers and non-obvious context needed to resume.

---

## 2026-08-10 — 7-domain PRD gap audit (protocol/auth, workflow-engine, data-model, agent-model, artifact-validation, runner-daemon, web-ui)

**What:** Dispatched 7 parallel read-only review agents to compare the full codebase against `PRD.md`. No code was written. Full per-domain reports (Implemented / Partially / Not Implemented / deviations, with file:line evidence) were delivered in the team chat transcript of this session — not reproduced here, only the synthesis.

**Why:** Establish ground truth on how far the implementation has drifted from PRD.md before planning further work.

**Headline cross-cutting findings (confirmed by 3+ reviewers independently):**
1. **P1 violated at one chokepoint**: node output = agent-self-reported `output.json` (`apps/control-plane/internal/workflow/runner_executor.go:74-83`), never scanned/checksummed against `outputSpec.produces`. Fixing this one spot fixes it for workflow-engine, runner-daemon, artifact-validation, and agent-model simultaneously.
2. **Event-sourcing is decorative**: `events` table is schema-correct but run state lives in an in-memory map (`runs.go:58,142`, "workflow_runs table deferred") that's lost on restart. No replay-on-boot, no snapshot, no World Reconstruction Test, 3 broken idempotency-key implementations (one uses `randHex(8)`). `apps/control-plane` tests don't run in CI at all (`.github/workflows/ci.yml` is pinned to `apps/server`).
3. **20 of 23 PRD §16 tables don't exist** (`instances`, `projects`, `members`, `skills`, `workflows`, `workflow_runs`, `node_executions`, `artifacts`, `artifact_deps`, `executor_procs`, `validation_results`, `feedback_log`, `accounts`, `leases`, `meta`, `executors`). `agent_specs` uses 15 flat columns instead of mandated `spec_json` — root cause of `memory`/`skills`/`allowedMCPTools`/`actionPolicy` being unrepresentable.
4. **Specs declared, not enforced**: `ExecutorAgentSpec` has 17 PRD fields in Go+JSON Schema, only 2 affect runtime. `Approve()` is hardcoded `return true` ("V1-M2 TODO").
5. **3 ADR (C4/C9) violations**: runner is TS/Bun not Go static binary (`apps/daemon`); admin frontend is Vite SPA not Next.js; goose (forbidden for SQLite) is used, sqlc (mandated) doesn't exist anywhere.
6. **Auth is thin**: zero `Authorization`/`Bearer` handling in control-plane `server.go`; long-lived machine token passed via argv (readable via `/proc/<pid>/cmdline`); executors get full `process.env` inheritance, no allowlist.
7. **Two backends, not one**: `apps/server` (Go/Postgres, IAM/SSO, ~40% of admin UI unrouted dead code) and `apps/control-plane` (Go/SQLite, optional, in-memory) are separate modules, soft-coupled only via `entity_id`/`owner_id` with no referential integrity.
8. **D.2 machine-access wizard** (the most heavily specified UI interaction in Appendix D) has zero implementation — no state machine, confirm clickable immediately at idle.

**Suggested priority order:** see chat transcript for full P0–P3 roadmap. P0 = artifact self-report fix, event-sourcing inversion, ADR decisions (Go-runner vs Bun, Next.js vs Vite), auth middleware, idempotency-key fixes.

**How to apply:** Before starting any new chaosplus feature work, check this list — don't build on top of the in-memory run store or the self-reported-output pattern without first flagging that it contradicts PRD P1/§15.1.

---

## ✅ Resolved: `.rules/` genericized to match chaos.plus (2026-08-10)

`.rules/` previously described a multi-tenant **restaurant/POS system** (系统运维端/商户收银端/商户后厨端/顾客端, SYS/OPS/MCH/POS/KDS/H5/WWW/NUM portal isolation, 商户/门店 RBAC) — an unrelated domain to **chaos.plus**, the agentic workflow orchestration platform described in `PRD.md` (digital humans, DAG workflows, artifact validation, IM/chat).

**Resolution:** user directed (2026-08-10) to strip all POS/KDS/MCH/NUM and other retail-specific terminology from every `.rules/*.md` file and replace with generic descriptions, since `.rules/` methodology (REST conventions, authz architecture, state-machine docs, test design, i18n, design tokens, PRD template, ADR conventions) is reusable but its retail examples were not. Applied across all files except `2.META.md` and `5.ADR.md` (already generic, untouched). Abbreviation scheme: SYS/OPS kept, MCH→APP (业务管理端), POS/KDS/NUM removed entirely (no generic equivalent — device/peripheral-specific), H5→WEB (终端用户端), 商户→租户/业务, 门店/outlet→实例/instance (matches chaosplus's actual `instances` table). Restaurant-domain examples (下单/桌台/菜单/购物车/结算 etc.) replaced with AI-platform equivalents (任务/工作流/运行/资源/参数).

**How to apply:** `.rules/` is now safe to use as the tie-breaker per `CLAUDE.md` ("when in doubt, follow `.rules/`") without the prior domain-mismatch caveat. Verified via `grep -rniE '\b(POS|KDS|MCH|NUM)\b' .rules/` returning zero matches.

---

## 2026-08-10 — Production hardening sprint (P0/P1 fixes delivered)

**What:** Implemented 7 changes to close the gap between demo-quality and production-delivery quality, based on the 7-domain PRD gap audit. All changes tested (16/16 tests pass), CI updated.

**Delivered:**
1. **Artifact trust boundary** (`runner_executor.go`): `outputSpec.produces` validation — required artifacts must exist + match declared type. Fixes P1 violation where agent self-reported output was trusted blindly. (+4 tests)
2. **Auth middleware** (`server.go`, `server_auth_test.go`): Bearer token + query-param (WS fallback) via `CONTROL_API_TOKEN` env var. Uses `subtle.ConstantTimeCompare`. Token NOT in argv (was `/proc`-readable). (+7 tests)
3. **Run persistence + replay** (`runs_store.go`, `00011_workflow_runs.sql`): `workflow_runs` table added. Launch saves def, completion updates status, boot-time `LoadFromStore()` rehydrates run history. Goroutines NOT resumed (v1 limitation).
4. **ADR vs reality** (`PRD.md §4`): Added v1-implementation-deviation table acknowledging Bun daemon (not Go runner), Vite admin (not Next.js), goose (not sqlc), NATS+HTTP (not gRPC) as deliberate v1 choices.
5. **Approve() defense** (`runner_executor.go`): Changed from silent `return true` to `return error` — prevents accidental bypass of ApprovalExecutor. (+1 test)
6. **CI for control-plane** (`.github/workflows/ci.yml`): Added parallel `go-control-plane` job (vet + race test + build).
7. **n8n onError pattern** (`def.go`, `engine.go`): Node gains `onError: "continue"` — failed node passes stub output to downstream instead of killing run.

**Key architecture decision — Mastra reuse:** daemon already has `@mastra/core` installed (v1.0, GA Jan 2026). Mastra provides mature graph workflow engine (.then/.parallel/.foreach/.branch, Zod-typed schemas, durable execution suspend/resume, human-in-the-loop). **Decision**: daemon should use Mastra workflows for DAG execution; control-plane thin-layers API/auth/persistence/NATS scheduling. This eliminates the need to build suspend/resume, approval timeouts, and retry from scratch.

**Research — ComfyUI patterns for v2:**
- **Signature-keyed output cache** (`CacheKeySetInputSignature`): deterministic recursive hash of node inputs → incremental re-run free. Unchanged upstream = cached downstream skip.
- **IS_CHANGED hook**: fold external state (file hashes, seeds) into cache key.
- **Hierarchical cache** for subgraphs: ephemeral nodes cached under parent.
- **DynamicPrompt expansion**: subworkflows = graph expansion at runtime, not separate files.

**How to apply:** control-plane now requires `CONTROL_API_TOKEN` env for production (empty = desktop mode). Workflow authors can add `"onError": "continue"` to nodes that should not block the run on failure. CI covers control-plane alongside server.

See also: [[7-domain-prd-gap-audit]], [[mastra-workflow-reuse]]
