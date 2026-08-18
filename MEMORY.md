# MEMORY.md

Cross-session memory for chaosplus. Read this first on a new session/device. Keep concise — full rule bodies live in `.rules/`, this file only holds pointers and non-obvious context needed to resume.

---

## 2026-08-10 — 7-domain PRD gap audit (protocol/auth, workflow-engine, data-model, agent-model, artifact-validation, runner, web-ui)

**What:** Dispatched 7 parallel read-only review agents to compare the full codebase against `PRD.md`. No code was written. Full per-domain reports (Implemented / Partially / Not Implemented / deviations, with file:line evidence) were delivered in the team chat transcript of this session — not reproduced here, only the synthesis.

**Why:** Establish ground truth on how far the implementation has drifted from PRD.md before planning further work.

**Headline cross-cutting findings (confirmed by 3+ reviewers independently):**
1. **P1 violated at one chokepoint**: node output = agent-self-reported `output.json` (`apps/server-ai/internal/workflow/runner_executor.go:74-83`), never scanned/checksummed against `outputSpec.produces`. Fixing this one spot fixes it for workflow-engine, runner, artifact-validation, and agent-model simultaneously.
2. **Event-sourcing is decorative**: `events` table is schema-correct but run state lives in an in-memory map (`runs.go:58,142`, "workflow_runs table deferred") that's lost on restart. No replay-on-boot, no snapshot, no World Reconstruction Test, 3 broken idempotency-key implementations (one uses `randHex(8)`). `apps/server-ai` tests don't run in CI at all (`.github/workflows/ci.yml` is pinned to `apps/server`).
3. **20 of 23 PRD §16 tables don't exist** (`instances`, `projects`, `members`, `skills`, `workflows`, `workflow_runs`, `node_executions`, `artifacts`, `artifact_deps`, `executor_procs`, `validation_results`, `feedback_log`, `accounts`, `leases`, `meta`, `executors`). `agent_specs` uses 15 flat columns instead of mandated `spec_json` — root cause of `memory`/`skills`/`allowedMCPTools`/`actionPolicy` being unrepresentable.
4. **Specs declared, not enforced**: `ExecutorAgentSpec` has 17 PRD fields in Go+JSON Schema, only 2 affect runtime. `Approve()` is hardcoded `return true` ("V1-M2 TODO").
5. **3 ADR (C4/C9) violations**: runner is TS/Bun not Go static binary (`apps/runner`); admin frontend is Vite SPA not Next.js; goose (forbidden for SQLite) is used, sqlc (mandated) doesn't exist anywhere.
6. **Auth is thin**: zero `Authorization`/`Bearer` handling in server-ai `server.go`; long-lived machine token passed via argv (readable via `/proc/<pid>/cmdline`); executors get full `process.env` inheritance, no allowlist.
7. **Two backends, not one**: `apps/server` (Go/Postgres, IAM/SSO, ~40% of admin UI unrouted dead code) and `apps/server-ai` (Go/SQLite, optional, in-memory) are separate modules, soft-coupled only via `entity_id`/`owner_id` with no referential integrity.
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
4. **ADR vs reality** (`PRD.md §4`): Added v1-implementation-deviation table acknowledging Bun runner (not Go runner), Vite admin (not Next.js), goose (not sqlc), NATS+HTTP (not gRPC) as deliberate v1 choices.
5. **Approve() defense** (`runner_executor.go`): Changed from silent `return true` to `return error` — prevents accidental bypass of ApprovalExecutor. (+1 test)
6. **CI for server-ai** (`.github/workflows/ci.yml`): Added parallel `go-server-ai` job (vet + race test + build).
7. **n8n onError pattern** (`def.go`, `engine.go`): Node gains `onError: "continue"` — failed node passes stub output to downstream instead of killing run.

**Key architecture decision — Mastra reuse:** runner already has `@mastra/core` installed (v1.0, GA Jan 2026). Mastra provides mature graph workflow engine (.then/.parallel/.foreach/.branch, Zod-typed schemas, durable execution suspend/resume, human-in-the-loop). **Decision**: runner should use Mastra workflows for DAG execution; server-ai thin-layers API/auth/persistence/NATS scheduling. This eliminates the need to build suspend/resume, approval timeouts, and retry from scratch.

**Research — ComfyUI patterns for v2:**
- **Signature-keyed output cache** (`CacheKeySetInputSignature`): deterministic recursive hash of node inputs → incremental re-run free. Unchanged upstream = cached downstream skip.
- **IS_CHANGED hook**: fold external state (file hashes, seeds) into cache key.
- **Hierarchical cache** for subgraphs: ephemeral nodes cached under parent.
- **DynamicPrompt expansion**: subworkflows = graph expansion at runtime, not separate files.

**How to apply:** server-ai now requires `CONTROL_API_TOKEN` env for production (empty = desktop mode). Workflow authors can add `"onError": "continue"` to nodes that should not block the run on failure. CI covers server-ai alongside server.

See also: [[7-domain-prd-gap-audit]], [[mastra-workflow-reuse]]

---

## 2026-08-18 — UI 缺陷根因修复 + 门户架构目标确认

**What:** 用户反馈 UI 多处问题(注册入口消失、下拉背景吞字、下拉显示 id)。根因调查并修复 4 处。

- **下拉背景吞字根因**:全站 CSS 未设置 `color-scheme`。next-themes 把 `.dark` 类加在 `<html>`,但浏览器按系统配色渲染原生 `<select>` 弹层 → 暗色页面 + 系统浅色弹层 = 浅色文字压白底。**修复**:`globals.css` `:root{color-scheme:light}` + `.dark{color-scheme:dark}`;`themes.css` `[data-theme]{color-scheme:light}` + `[data-theme].dark{color-scheme:dark}`。两份包各改:apps/admin 与 apps/admin-ai 的 packages/ui。已用构建产物 + Playwright computed style 验证(暗/浅切换正确,select 文字 250/23 高对比)。
- **下拉显示不可读字符串**:workflow-editor `property-panel.tsx` 的 `SelectField` 直出原始枚举(`manual/webhook/script/http/stop`)。改为 `{value,label}` 结构,显示中文标签(手动/定时/脚本/停止/继续/暂停/自动拒绝/重试),存储值不变。
- **IAM 租户切换器显示 raw id**:`apps/admin/apps/iam/src/app/layout.tsx` 的 datalist 用 `value={tenant.id}` 而 input 显示 value → 顶栏展示 id。改为 datalist value 用 `名称 (slug)`,展示时映射名字,commit 时解析回 id。注:该 workspace 未安装依赖,无法本地 tsc,逻辑已逐步推演(见 diff)。
- **注册入口消失根因**:登录页的"创建账号"链接由 `GET /authn/capabilities` 的 `registration` 门控 → `WebService.RegistrationEnabled()` = web enabled && registration.enabled && email_verification.enabled && notification enabled。dev 配置(iam-local-dev.yaml)全开,compose 生产配置(config.yaml)全关 → 生产注册默认隐藏。**按用户既定配置驱动,未改**。

**门户架构目标(用户口述 2026-08-18):** 最终形态需要三个不同视角的 web 管理:`/sys`=系统运维、`/ops`=平台运营、`/platform`=租户/实例/用户平台(默认开放注册)。当前是两套扁平路由前端(apps/admin IAM、apps/admin-ai platform),无前缀分段,注册姿态全局单一配置。与 `.rules/3.ARCH.md` 的 SYS/OPS/APP 门户模型一致,抓手是"端路由前缀隔离 + 按端鉴权姿态"。**纳入下一轮规划(用户已选"先修 UI bug,门户后置")**。

**How to apply:** 后续新增下拉一律走 `SimpleSelect`(value→label 单一事实来源),别手写原生 select/datalist;暗色下任何原生控件异常先查 color-scheme;注册可见性永远先查三层配置链(registration→email_verification→notification)。
