# Authorization & Authentication design (SpiceDB as source of truth)

Status: **DRAFT for review** — no code yet. Goal: a unified, production-grade authz
system that exceeds `7link-mall-api` on interface / data / menu / platform-scope
permissions, using SpiceDB (Zanzibar) as the single source of truth.

## 1. Goals / non-goals

Goals (each must beat 7link):
- **接口权限** (endpoint): declarative, auto-cataloged, impossible-to-forget enforcement.
- **数据权限** (row/object): relationship-based, reverse-indexable ("what can X see"),
  cross-cutting sharing ("X can see order Y"), hierarchical scope.
- **菜单权限**: menu visibility derived from the *same* permission source as the API
  (never diverges).
- **平台 scope + 分层**: platform > tenant > instance(HQ) > dept > outlet as relations.
- **统一授权面**: one management + query surface across all platforms/scopes, incl.
  "effective permissions of X", "who can do Y", "what can X see".
- Production-grade: high performance, HA-aware, testable, ≥90% coverage.

Non-goals (for now): billing/entitlement plans, audit UI, SSO/OIDC federation
(pluggable later).

## 2. Principles (grounded in Authzed best practices)

- **SpiceDB is the single source of truth** for every authorization relation. The
  app's business DB stores only non-authorization metadata (role display name/i18n,
  menu tree/route/icon, resource-field catalog) that *references* SpiceDB object ids.
  No parallel `role_permissions` decision table — checks always hit SpiceDB.
- **Tenant isolation is modeled in the schema** (SpiceDB has no built-in tenancy):
  a `tenant` object owns resources; every resource points to its tenant.
- **Relations over caveats**; caveats only for request-time context (ABAC) that
  can't be a relation.
- **Arrows point at permissions**, not relations (`resource->parent_permission`).
- **Additive schema, negation used sparingly**; a missing relation must mean *no*
  access (fail-secure).
- **Consistency: `at_least_as_fresh` + ZedTokens.** Capture a ZedToken on every
  write, persist it next to the affected resource/subject; use it on checks. Use
  `fully_consistent` only to bootstrap a token; `minimize_latency` for list ops
  (LookupResources) with the final content view still gated by a token-backed check.
- **Writes use TOUCH** (idempotent, retryable) with preconditions where ordering
  matters. Consistency between business DB and SpiceDB via an **outbox** (write the
  business row + an outbox record in one DB tx; a worker applies the SpiceDB write
  and records the returned ZedToken).

## 3. Architecture

```
             ┌──────────────────────────────────────────────┐
  request →  │ authn middleware  (JWT: subject-type, sub,    │
             │                    tenant, active scope, ver)  │
             │ authz gateway     (declared resource:action → │
             │                    SpiceDB CheckBulk, cached)  │
             │ data-scope pushdown (LookupResources → SQL)    │
             │ field-mask (at respx serialize boundary)       │
             └──────────────────────────────────────────────┘
                       │ Check/CheckBulk/LookupResources/LookupSubjects
                       ▼
   ┌───────────┐   ┌─────────────┐   ┌──────────────────────────┐
   │  Redis    │   │  SpiceDB    │   │ business DB (bun)         │
   │ perm-set  │◄──│ (authz SoT) │   │ role/menu metadata, UX,   │
   │ cache     │   │ 10.0.0.100  │   │ ZedToken columns, outbox  │
   └───────────┘   └─────────────┘   └──────────────────────────┘
                       ▲ WriteRelationships (TOUCH) via outbox worker
```

Components (DDD-lite modules, per docs/module-structure.md):
- `internal/core/extension/spicedbx` — SpiceDB client wrapper: Check, CheckBulk,
  LookupResources, LookupSubjects, WriteRelationships (TOUCH), schema apply,
  consistency helpers, ZedToken plumbing. Config for the authzed/grpc endpoint.
- `internal/core/extension/authz` — the **declaration registry** + gateway
  middleware + data-scope pushdown + field-mask hook. Route declares
  `authz.Guard{Resource, Action}`; the registry (a) enforces via CheckBulk, (b)
  emits the SpiceDB schema, (c) is the permission catalog.
- `internal/modules/authn` — login/refresh/logout, tokens, revocation.
- `internal/modules/iam` (or split): role, menu, permission-catalog, scope
  (tenant/instance/dept/outlet) management + the unified query API. Writes go to
  SpiceDB (via outbox); reads for the admin UI come from SpiceDB + metadata DB.

## 4. SpiceDB schema (draft — generated from route declarations)

The schema is **generated** from the same `authz.Guard` declarations that drive the
gateway, so catalog + enforcement + schema never drift. Illustrative shape:

```zed
definition user {}
definition operator {}

// ---- hierarchy (tenant isolation + layering) ----
definition platform {
  relation admin: user
  permission superadmin = admin
}
definition tenant {
  relation platform: platform
  relation admin: user | operator
  permission administer = admin + platform->superadmin
  // per (resource,action) grant relations + permissions are GENERATED, e.g.:
  relation order_view_role: role#member
  permission order_view = order_view_role + administer
  relation order_edit_role: role#member
  permission order_edit = order_edit_role + administer
  // ... one pair per declared (resource, action)
}
definition instance {           // HQ under a tenant
  relation tenant: tenant
  relation admin: user | operator
  permission administer = admin + tenant->administer
}
definition dept {               // materialized via parent relation
  relation instance: instance
  relation parent: dept
  relation manager: user
  permission manage = manager + parent->manage + instance->administer
}
definition outlet {
  relation tenant: tenant
  relation manager: user | operator
  permission manage = manager + tenant->administer
}

// ---- roles (custom, admin-defined groups) ----
definition role {
  relation member: user | operator
}

// ---- object-level resources (data permission + sharing) ----
definition order {
  relation tenant: tenant
  relation outlet: outlet
  relation owner: user | operator      // creator (SELF scope)
  relation shared_viewer: user | operator   // cross-cutting sharing
  permission view = owner + shared_viewer + outlet->manage + tenant->order_view
  permission edit = owner + outlet->manage + tenant->order_edit
}
```

Notes:
- **接口权限** = `tenant#<res>_<action>` (type-level: does the subject's role grant
  the action anywhere in the tenant).
- **数据权限** = `<res>:<id>#<action>` (object-level: own + shared + hierarchy).
  Reverse index "what orders can X view" = `LookupResources(order, view, X)`.
- **平台 scope 分层** = the arrow chain `dept->parent->…->instance->tenant->platform`.
- **Custom role** = a set of relationships `tenant:T#<res>_<action>_role@role:R#member`
  (one per granted permission); assigning the role to a user = `role:R#member@user:U`.
- The `~40 resource × ~4 verb` grant relations live on `tenant` (and are generated),
  ~4 relations per resource definition — mechanical, not hand-written.

## 5. The four dimensions vs 7link

| Dimension | 7link | This design (exceeds by) |
|---|---|---|
| 接口权限 | JWT-baked codes; catalog = static SQL seed (ids 1–219); only `roles` migrated to declarative | Declarative route→action; **catalog generated from schema** (reflectable); 100% enforced via gateway + CI gate; live (no bump/refresh) |
| 数据权限 | scope enum (SELF/DEPT/…); repo-cooperative `ApplyScope` (forgettable) | Relationship model + **LookupResources** reverse index + cross-cutting sharing; **structural pushdown** (bun query hook keyed by resource — can't forget) |
| 菜单权限 | `perm_code` advisory; menu vs API can diverge | Menu bound to the *same* declared action; tree = CheckBulk + prune, cached — always consistent with API |
| 平台 scope 分层 | fixed 5-level, no arbitrary relations | SpiceDB arrows: arbitrary depth, relationship-based; + signed session scope (adopt ADR-10) |
| 统一面 | 6 disjoint modules, no aggregate views | one management + **query** API: effective-perms (LookupResources per res), who-can-do-Y (LookupSubjects), reverse "what can X see" |
| 性能/一致性 | in-proc cache, per-pod; JWT staleness | CheckBulk + Redis perm-set cache (cross-replica) + ZedToken `at_least_as_fresh`; instant changes; small token |

## 6. Authentication

Adopt 7link's proven parts, fix its gaps:
- Single canonical claims struct shared by HTTP + gRPC; refresh **rotation** +
  re-derive authority from source on refresh (anti-escalation); stateful revocation
  via an auth-version bump (busts Redis perm-set cache).
- **Do not bake permissions into the JWT** — token carries `sub`, `subject_type`
  (user|operator — fixes 7link's shared uid namespace), `tid`, active `scope`/`soid`,
  `ver`. Permissions are checked live (cached). Token stays small; changes are instant.
- **EdDSA (Ed25519) with `kid` + key rotation + `iss`/`aud`** (exceeds HS256-no-kid).
- Rate-limit **all** realms incl. operator/POS/KDS; refresh for all realms; captcha
  **fail-closed** when configured (no silent skip).
- bcrypt cost 12 (or argon2id) for passwords/PINs; PIN lockout retained.

## 7. Performance & consistency

- **CheckBulkPermissions** for the per-request set (endpoint + visible menu items) in
  one round-trip.
- **Redis perm-set cache**: subject→(tenant, scope)→allowed-actions, short TTL,
  busted on auth-version bump / relevant WriteRelationships. Cross-replica (fixes
  7link's per-pod caches).
- **ZedToken** stored per subject (last authz-relevant write) and per shared object;
  checks use `at_least_as_fresh`. List/LookupResources use `minimize_latency`, with
  the content-view Check token-backed.
- SpiceDB connection-pool tuning; **fail-closed** if SpiceDB *and* cache both
  unavailable (deny), with clear alerting — availability tradeoff accepted per the
  decision to make SpiceDB the SoT.

## 8. Enforcement that can't be forgotten (exceeds 7link's opt-in seams)

- **接口**: gateway middleware enforces every operation from its `authz.Guard`
  declaration; a CI gate fails the build if any mutating op lacks a declaration.
- **数据**: a bun query hook injects the LookupResources id-set / scope predicate by
  resource — not a per-repo manual splice.
- **字段**: field-mask applied at the `respx` serialize transformer (we already have
  that seam), uniformly — not opt-in per handler.

## 9. Module structure (DDD-lite)

```
internal/core/extension/spicedbx/     # SpiceDB client + schema apply + zedtoken
internal/core/extension/authz/        # declaration registry + gateway + pushdown + fieldmask
internal/modules/authn/               # login/refresh/logout, tokens, revocation
internal/modules/iam/                 # role, menu, permission-catalog, scope mgmt + query API
  (each: api/{rest,grpc}, proto/, sql/, domain/service/repository as they grow)
```

## 10. Phased delivery (each independently acceptance-testable, ≥90% cov, real SpiceDB)

- **P0 — foundation**: spicedbx adapter + config + connect to 10.0.0.100 + schema
  apply/validate (`zed validate` style tests) + outbox skeleton; authn (Ed25519 JWT,
  subject-type, refresh rotation, revocation). Acceptance: login→token→revoke; a
  hand-written relation Check passes against real SpiceDB.
- **P1 — 接口权限**: `authz.Guard` declaration + generated schema + gateway CheckBulk +
  Redis cache + catalog reflect API + CI gate. Acceptance: a guarded endpoint allows/
  denies by role live; catalog lists all actions; changing a grant takes effect with
  no re-login.
- **P2 — 平台 scope 分层 + 数据权限**: hierarchy relations + LookupResources pushdown +
  sharing. Acceptance: dept-tree scoping + "share order Y to user X" both enforced;
  "what can X see" query.
- **P3 — 菜单 + 字段权限**: menu tree from CheckBulk; field-mask at respx.
- **P4 — 统一管理 + 查询面**: role/menu/scope CRUD (writes via outbox) + effective-perms
  / who-can-do-Y / what-can-X-see aggregate APIs.

## 11. Risks / open decisions

- **SpiceDB availability** is now on the request path (mitigated by Redis cache +
  fail-closed). Accepted per the SoT decision.
- **Generated-schema complexity**: the tenant definition grows with resource×action;
  mitigated by generation + reflection (no hand-editing).
- **Outbox** adds an async hop between business write and SpiceDB relation; brief
  window mitigated by ZedToken + at_least_as_fresh.
- **Realm model**: start with `user` (platform/tenant) + `operator`; add POS/KDS/
  customer later. Confirm the initial realm set.
- **Custom roles vs pre-canned**: fine-grained custom roles (arbitrary action subsets)
  chosen for the management surface; coarse pre-canned roles are a subset of it.

## 12. Decisions (confirmed — "best/most-correct" chosen)

1. **Modules**: `internal/core/extension/{spicedbx,authz}` + `internal/modules/authn`
   first; management (role/menu/scope) consolidated in `internal/modules/iam` and
   split into sub-packages when it grows (per module-structure.md).
2. **Realms**: implement `user` (platform/tenant) first; the SpiceDB subject union is
   designed extensible — `user | operator | device | service_account` — so IoT
   `device` and machine `service_account` subjects slot in later without schema churn.
3. **Password KDF**: **argon2id** (OWASP-preferred; stronger than bcrypt).
4. **JWT**: **Ed25519 (EdDSA)** with `kid`, key rotation, `iss`/`aud`.
5. **Redis** (already used by the rate limiter) is reused for the perm-set cache.

## 13. Platform boundaries — what SpiceDB is and is NOT (mall / IoT / AI)

SpiceDB is the unified **authorization** layer across all three platforms. Keep these
OUT of SpiceDB (using it for them is misuse):
- **Device transport auth (IoT)**: per-message device identity is EMQX + mTLS/device
  certs. SpiceDB answers "which *user/tenant* may access device/data D", not per-
  telemetry device auth.
- **Metering / quotas / rate limits (AI)**: token budgets, call quotas, compute
  limits are cumulative numeric state → Redis counters / a usage service (reuse the
  ratex foundation), not SpiceDB relations.
- **Heavy request-time ABAC**: moderate context via SpiceDB caveats; escalate to a
  policy engine only if genuinely needed later.
- **Business data**: lives in the app DB; SpiceDB holds only authorization relations.

Scale notes (IoT/AI): device/resource objects can reach 10^7+. `LookupResources`
("what can X see") must paginate + cache; relationship writes on provisioning go
through the outbox. This is within SpiceDB's proven envelope (10^9 relations, 10^6
QPS, ~5ms p95), but must be designed for, not assumed.
