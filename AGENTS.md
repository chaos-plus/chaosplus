# Dev 工程规则

这些规则适用于整个仓库和所有任务类型，包括问答、只读调查、评审、规划、设计、实现、测试、配置、文档、PRD、UI/UX/UE、数据、IAM、安全、基础设施、部署、提交、推送和交付。规范唯一来源位于 `.rules/`；`.claude/skills/dev-*` 是统一 Skill 源码，`.agents/skills/dev-*` 是 Codex 官方发现入口的符号链接，严禁复制第二份规则。Codex 和 Claude 都必须自动读取并执行 `.claude/skills/dev-engineering/SKILL.md` 与匹配的领域 Skill，严禁等待用户点名。

## Non-negotiable design baseline

- Every product, architecture, API, data, security, deployment, operations, UI, UX, UE, and testing design MUST use the most formal, broadly adopted, standards-based solution supported by global engineering consensus for its context. Use official specifications and components, established protocols, conventional interoperable models, and actively maintained mainstream libraries. Demo/sample designs, mocks or fake services in a product path, temporary implementations, expedient shortcuts, obscure or proprietary alternatives without necessity, local-only assumptions, and unverified fallbacks are strictly prohibited. Test doubles are allowed only inside isolated automated tests and MUST NOT enter runtime code or replace integration, protocol, migration, security, accessibility, performance, or end-to-end acceptance. When no generally accepted solution exists, stop implementation and add a reviewed ADR that compares mainstream alternatives and defines interoperability, security, operations, migration, rollback, and verification before proceeding.

## Mandatory pre-code design and reuse gate

- STOP GATE: before `apply_patch`, a formatter, code generator, migration generator, dependency update, or any other command/tool that can modify the worktree, publish the complete pre-code design record below. Investigation may use read-only commands only. If the record is incomplete, do not edit.
- Before ANY code or file edit, search the whole repository with `rg`/`rg --files` for existing implementations, extension packages, module ports, interfaces, configuration, lifecycle hooks, tests, migrations, call sites, and consumers that may already solve or constrain the requirement. Search by domain term, public symbol, route, table, configuration key, and import path; a directory listing or one exact-name search is not sufficient.
- Read the complete relevant owner implementation before designing or editing: package source, public ports, constructors/registration, configuration, migrations for every supported dialect, tests, and principal callers. Do not infer behavior from filenames, snippets, one caller, generated documentation, or an old design document.
- Before editing, publish a concise pre-code design record in the working plan with these exact headings: `Requirement/Invariants`, `Repository Searches`, `Inspected Owner/Consumers`, `Existing Owner Capability`, `Verified Gap`, `Final Ownership`, `Dependency/Composition`, `API/Data/IAM Contract`, `Failure/Migration/Rollback`, and `Acceptance Gates`. Name the searched terms and inspected paths, state what will be reused unchanged, and justify every new public abstraction. Missing search evidence, incomplete owner/consumer reading, an unverified gap, or a missing final-state design blocks every edit.
- Search first, design second, code third. Never write a candidate implementation to discover whether the capability already exists. Never create a second implementation while planning to consolidate it later.
- Re-run the reuse search when new evidence changes ownership, scope, or dependencies. Stop and redesign immediately when an existing owner capability is discovered; never continue a parallel implementation because work has already started.
- Before ANY architecture, module, IAM, security, persistence, infrastructure, or cross-application change, define the final ownership boundary, dependency direction, composition point, API/data contract, security path, failure behavior, migration/rollback policy, and verification gate. Do not create staging packages, compatibility wrappers, temporary moves, duplicate abstractions, or intermediate directory layouts in product code.
- Reuse and, when necessary, extend the existing owner implementation. Never create parallel authentication, authorization, IAM claims, Origin/CORS/CSRF/security, database, GUID/ID, migration, transport, or infrastructure logic in a consuming application. `apps/server` owns shared IAM and security; `apps/server-ai` consumes those capabilities and contains only AI control-plane business modules plus its application composition.
- For IAM or security work, search and fully trace `apps/server/internal/core/extension/authn`, `authz`, `authzsql`, `secure`, relevant `apps/server/internal/modules/{authn,iam,identity,organization,audit}`, `apps/server/internal/app`, their registrations, migrations/tests, and every affected client before adding logic. Record the exact existing entry point, guard, claims source, repository scope, and audit path. New IAM behavior is allowed only after this trace proves a concrete gap, and only as an extension of the verified `apps/server` owner package and its established registration/composition path. If the capability already exists, consume it without reimplementing it.
- 编辑前、结构修改后和交付前运行 `.claude/skills/dev-engineering/scripts/check_architecture_contract.py`。任何违规都必须阻止后续功能开发、提交和推送，直到完全修复。

## Backend and persistence

- Follow `apps/server/internal/app` and `apps/server/internal/modules` as the architecture reference. Business code belongs to `internal/modules/<domain>` and owns its domain, service, repository, REST, migration, SQL, i18n, and lifecycle.
- Never introduce a shared business `internal/store`, central feature handler, sqlc, server-ai RSQL, or an embedded infrastructure daemon.
- Goose owns migrations. Bun owns ORM, queries, and transactions. Create `*bun.DB` once in `internal/app` and inject it into modules.
- Every internally generated entity/resource/event ID and every FK to one uses Sonyflake `guid.ID` stored as `BIGINT`. This includes tenant, entity, principal, owner, creator, updater, and deleter IDs. Generate IDs through an injected generator backed by the leased `guid`/`wuid` lifecycle. Never use UUIDs, random short strings, timestamps, or auto-increment for business IDs.
- Encode `guid.ID` as a decimal string at JSON, HTTP, NATS, and JavaScript boundaries to prevent IEEE-754 precision loss; parse it before domain/repository code. A string wire representation never justifies a `TEXT` ID column.
- External provider subjects are natural strings and must be named `external_subject`, not `*_id`. Token hashes, idempotency keys, enum values, node keys, paths, and checksums are also strings but are not IDs. Sequences, versions, fencing tokens, attempts, and counters are numeric but are not entity IDs.
- Persist all instants as UTC Unix milliseconds in `BIGINT`; use `time.Now().UTC()` and `time.UnixMilli(...).UTC()`. Convert timezone only in the presentation layer. Never persist local wall-clock time.
- Mutable aggregates must explicitly define ownership, audit, concurrency, and deletion policy. When applicable use `owner_id`, `created_at`, `created_by`, `updated_at`, `updated_by`, `deleted_at`, `deleted_by`, and `version`. Ownership and creation audit are different fields.
- Use typed Go enums plus service validation and equivalent DDL `CHECK` constraints. Use real booleans (`bool`/`BOOLEAN`) with SQLite domain checks. Do not represent booleans, enums, JSON, time, money, or IDs using arbitrary `TEXT`.
- Every module migration provides schema-equivalent `sql/sqlite`, `sql/mysql`, and `sql/postgres` files. Verify PK/FK types, nullability, defaults, checks, unique constraints, indexes, and down migrations across all three.

## IAM and infrastructure

- Production authentication is deny-by-default, including reads. Trust only verified IAM claims. Never trust client-supplied tenant, entity, principal, owner, role, or permission headers.
- Any IAM schema change must trace authn, authz, sessions, tenant membership, entity scope, organization, audit, migrations, data compatibility, and admin clients end to end.
- Development and production NATS use a fixed official NATS image. Do not add `apps/server-ai/cmd/nats` or embed NATS.
- Preview tunneling uses fixed official FRP `frps`/`frpc`. Production deploys official `frps` separately and runner supervises official `frpc`. Do not use Cloudflare Tunnel, fork/embed FRP, or invent a tunnel protocol. Public preview traffic must pass through the IAM-aware gateway.

## Delivery

- Run the schema contract checker and affected migration tests before Go tests.
- Run formatting, race tests, vet, staticcheck, govulncheck, frontend lint/typecheck/tests/build, and real end-to-end checks proportional to the change.
- Never claim production readiness when a required environment-dependent check was not run. Record the exact residual blocker.
- On an explicit delivery request, commit and push only after required checks pass and the worktree has been reviewed for unrelated changes. If the user explicitly requests an incomplete snapshot while gates remain blocked, `.rules/3.TEST.md` permits only a `WIP:` commit on a non-protected, non-default development branch with exact failed/unrun gates and a not-ready declaration; architecture/schema/test-policy, secrets, conflicts, diff hygiene, security invariants, merge, release, tag, and force-push restrictions remain non-waivable.
