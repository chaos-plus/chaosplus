# Backend Lessons

Evidence-backed reusable backend lessons are appended here by `skill-runtime.py record`.


## L-57b6e23e76fa

- Symptom: Concurrent authenticated dashboard requests intermittently returned SQLite locked errors and HTTP 500 or 503 responses.
- Root cause: Writable SQLite connections lacked WAL mode and a busy timeout while session authentication performed concurrent reads and updates.
- Prevention: Apply foreign_keys and busy_timeout defaults to SQLite, use WAL for writable file databases, and preserve explicitly configured pragmas.
- Evidence: The real-file 12-worker update test passed five consecutive runs and the real Chromium concurrent dashboard audit returned no 500 or 503 responses.


## L-7aec08442b2d

- Symptom: The effective-menu endpoint returned data null for an empty result and crashed the administration UI when it called array methods.
- Root cause: The service returned a nil slice even though the API contract represents menu collections as arrays.
- Prevention: Return non-nil empty slices for collection contracts and assert the serialized HTTP data field is an empty JSON array.
- Evidence: Real Huma HTTP tests assert data is an empty array and the real browser dashboard renders an empty menu state without errors.


## L-df3f73f36416

- Symptom: The full backend gate reported 89.3 percent repository coverage after MFA implementation even though focused authentication tests passed.
- Root cause: The vertical slice lacked real tests for fail-closed MFA state mismatches, storage failures, REST recovery-code rotation, and small reachable module boundaries.
- Prevention: For every security vertical slice, cover success, expired or concurrent state, invalid factor, unavailable storage, REST error mapping, and production module registration with real dependencies, then run the full repository coverage gate before acceptance.
- Evidence: Backend Full gate passed with race coverage 90.0 percent, go vet clean, and govulncheck reporting no called vulnerabilities.


## L-3e53c5af3e75

- Symptom: A signed credential for a deleted local principal remained valid because a missing principal row was treated as an arbitrary service identity.
- Root cause: Locally issued JWTs had no explicit subject classification, so authentication could not distinguish principals, OAuth clients, and trusted service identities.
- Prevention: Include a closed subject classification claim in every locally issued JWT, reject missing or unknown values, and revalidate mutable principal or OAuth client state during every authentication.
- Evidence: Real SQLite regression coverage for deleted principals and disabled or deleted OAuth clients passed under race detection; the backend quality gate passed.


## L-58b25d69eca2

- Symptom: WUID up-down-up-down-to-zero migration lifecycle failed because worker_ids was missing and existing worker leases were discarded.
- Root cause: Migration 00004 replaced worker_ids with DROP and CREATE in both directions instead of mapping the v3 and v4 schemas.
- Prevention: For schema-replacement migrations, rename the source table, create the target schema, copy compatible data explicitly, drop the legacy table, and prove up-down-up-down-to-zero lifecycle behavior.
- Evidence: go test -race ./internal/infra/wuid passed; check-gates.ps1 -Scope all -Full passed with WUID coverage 90.6 percent.


## L-0944f7dc03e9

- Symptom: Password changes and session revocations could commit before their audit event was appended.
- Root cause: Authentication security-center methods wrote audit records after the business transaction using a best-effort insert.
- Prevention: Append high-risk security audit events through the hash-chain service inside the same database transaction and prove audit-write failure rolls back credential or session state.
- Evidence: Real SQLite failure triggers and go test -race ./internal/modules/authn passed; check-gates.ps1 -Scope backend -Full passed at 90.0 percent coverage.


## L-7f8ebf9916dd

- Symptom: IAM transactional audit introduced an audit-to-IAM test import cycle.
- Root cause: The IAM production package imported the concrete audit module instead of declaring an audit append port at its own boundary.
- Prevention: Cross-module transaction orchestration must depend on a local port and let internal/app inject the concrete adapter; real tests should inject the real service through that port.
- Evidence: check-gates.ps1 -Scope backend -Full passed after dependency inversion with race total coverage 90.1 percent and zero reachable vulnerabilities.


## L-d9763f1ab8a3

- Symptom: Identity principal mutations could commit global identity or token state before the tenant audit chain recorded the operation.
- Root cause: The identity service owned direct database transactions but had no shared transaction-local audit appender or policy revision primitive.
- Prevention: Inject the shared auditx appender at the composition root, append through the caller transaction, advance policy revision only for membership-affecting writes, and read mutable principal state inside the same transaction.
- Evidence: Real SQLite failure triggers and Huma HTTP tests passed; check-gates.ps1 -Scope all -Full passed with 90.2 percent race coverage and no reachable vulnerabilities.


## L-9d6e2382bcce

- Symptom: Notification lifecycle test intermittently observed delivering after the real provider accepted the request
- Root cause: Provider receipt and the worker's sent-state database update are separate asynchronous steps, but the test read the outbox between them
- Prevention: Async outbox tests must wait for the persisted terminal state before asserting row contents; receipt alone does not prove the worker committed its final transition
- Evidence: go test -race ./internal/modules/authn -run TestEmailVerificationLifecycle -count=20 passed


## L-427185ed590d

- Symptom: Race detector reported time.Local being changed while a GeoIP maintenance goroutine from a shut down application still read file timestamps
- Root cause: GeoIP shutdown cancelled provider contexts but did not join the goroutines before returning
- Prevention: Every background provider started by a lifecycle module must implement an explicit Stop that cancels and joins its worker before shared process state or dependencies change
- Evidence: go test -race ./internal/app ./internal/infra/geoip ./pkg/geoip ./pkg/geoip/providers passed


## L-a626cbd917c3

- Symptom: Disabling a tenant membership did not revoke role-derived tenant or entity authorization immediately.
- Root cause: The compiled IAM authorizer joined roles and permissions but did not require the subject's iam_tenant_members row to remain active.
- Prevention: Every tenant or entity scoped authorization query must recheck active tenant membership in the same database decision; role bindings alone are not sufficient.
- Evidence: go test -race ./internal/modules/iam passed and check-gates.ps1 -Scope all -Full passed with disabled-membership regression coverage.


## L-8f993217ca62

- Symptom: A rejected browser login return URL surfaced as authentication_unavailable with HTTP 500.
- Root cause: The REST adapter did not map the domain ErrReturnURL validation error and fell through to its generic server-error response.
- Prevention: Every public authentication validation error must have an explicit stable HTTP status and error code mapping covered through the real Huma transport.
- Evidence: The focused race test and backend Full gate passed; the running API returned 422 return_url_not_allowed for an invalid return URL and 200 for an allowed login.


## L-d027c970a5c8

- Symptom: Department APIs existed in OpenAPI but returned organization_unavailable because production startup had no organization tables.
- Root cause: The organization module was wired into the application composition root, but the privileged deployment migrator omitted its migration and runtime startup asserted only the IAM schema.
- Prevention: Every persistent module must be added together to deployment up and rollback dispatch, runtime schema assertions, and deployment lifecycle tests before its API can be accepted.
- Evidence: Focused race tests passed for the shared application, deployment, and organization packages; rebuilt startup created `iam_departments` and the real Chromium department workflow passed.


## L-2562be1db2b0

- Symptom: Deployment bootstrap tests failed because the compiled authorizer queried directory role-binding tables that an IAM-only test database did not contain.
- Root cause: Directory role bindings are created by the organization migration after IAM, but bootstrap tests initialized only the legacy IAM migration.
- Prevention: Tests exercising production authorization or bootstrap must apply the same ordered IAM and organization migration stack required at runtime; a module-only schema is valid only for explicit missing-schema tests.
- Evidence: go test -race ./internal/deployment ./internal/modules/iam passed and check-gates.ps1 -Scope backend -Full passed at 90.0 percent coverage.


## L-ec281b4cf69a

- Symptom: The deployment CLI rejected -c for migration subcommands and panicked on --help.
- Root cause: A global Cobra command parsed process arguments through the configurator during package init and registered configuration flags as root-local flags instead of inherited persistent flags.
- Prevention: Construct a fresh Cobra tree per execution, register struct-driven configuration on persistent flags, defer help rendering to Cobra, and prove config flags both before and after a migration subcommand against a real database.
- Evidence: Race tests for the production server command passed with real SQLite migrations for both flag positions; its `--help` command exited successfully and the full backend gate passed.


## L-c59cffe0aa53

- Symptom: Tenant-aware authorization tests returned inactive_tenant_membership after tenant lifecycle enforcement
- Root cause: Test setup migrated iam_tenants but inserted memberships without creating the corresponding active tenant fact
- Prevention: Every real authorization or bootstrap setup must create the tenant fact before tenant memberships, roles, or guarded HTTP requests
- Evidence: check-gates.ps1 -Scope backend -Full passed with race coverage 90.0 percent after tenant-aware setups were corrected


## L-09ef7ee02b42

- Symptom: Huma-generated validation details remained English in Chinese and Malay responses, and some handlers exposed wrapped internal errors in the public message.
- Root cause: The shared response transformer localized known keys but passed unknown framework detail text through, while error construction accepted arbitrary errors as client details.
- Prevention: Configure Huma validation messages as stable i18n keys, preserve and localize field locations, require complete locale parity at startup, and omit non-ErrorDetailer internal errors from public envelopes.
- Evidence: Real Huma HTTP localization tests passed for en-US, zh-CN, and ms-MY; backend Full gate passed at 90.0 percent race coverage with vet and govulncheck clean.


## L-52212c7352b7

- Symptom: In-memory SQLite allowed stale role-scope rows, and an OAuth server-error test changed behavior when foreign keys were enabled.
- Root cause: The shared SQLite DSN helper returned early for :memory: and skipped production foreign_keys and busy_timeout pragmas; the OAuth test dropped a referenced table instead of failing the target write.
- Prevention: Apply production SQLite constraint pragmas to in-memory databases, skip only unsupported WAL mode, and induce persistence failures at the exact write using a real database trigger.
- Evidence: Focused bunx, IAM data-scope, and OAuth tests passed; check-gates.ps1 -Scope all -Full passed at 90.0 percent race coverage.


## L-ed90561cc1ed

- Symptom: Invitation list responses returned role_ids null for invitations without default roles and the administration page crashed while reading the collection
- Root cause: The row mapper copied a nil role slice into a public collection contract
- Prevention: Every public collection field must be normalized to a non-nil empty slice at the producer and asserted as an empty JSON array through the real HTTP transport
- Evidence: Organization invitation race tests passed and the real Chromium invitation workflow completed with zero console errors after role_ids serialized as []


## L-d6e50dbd228b

- Symptom: Go coverage output rounded 89.983 percent to 90.0 percent at the acceptance boundary.
- Root cause: The gate consumes go tool cover's one-decimal total while the raw coverage profile retains exact covered and total statement counts.
- Prevention: When coverage is at the threshold, compute covered statements divided by total statements from the raw profile and add real branch coverage until the unrounded result is at least 90 percent.
- Evidence: Raw profile reached 8751 of 9723 statements, 90.0031 percent, and check-gates.ps1 -Scope all -Full passed.


## L-2ced8a112cfc

- Symptom: IAM Huma HTTP tests failed after using the production localized response envelope and still expected raw error keys.
- Root cause: The shared IAM API test constructor installed locale middleware and the response transformer but did not initialize base resources or register IAM module resources.
- Prevention: Every real HTTP test constructor must initialize the same base and owning-module i18n resources as production, assert localized messages for every supported locale, and reject raw message keys in public envelopes.
- Evidence: go test -race ./internal/modules/iam/api -count=1 passed; check-gates.ps1 -Scope all -Full passed at exact raw Go coverage 9500/10538 (90.149934%).


## L-0c85001233c1

- Symptom: Full backend coverage stayed at 89.943789 percent after SCIM tests exercised identity and organization provisioning adapters indirectly.
- Root cause: Go package coverage did not attribute adapter execution from provisioning package tests to the identity and organization packages that own those production files.
- Prevention: Test each cross-module adapter through its owning package with real database services and transactional failure paths, then verify the exact raw coverage profile.
- Evidence: check-gates.ps1 -Scope all -Full passed; raw profile reached 11690 of 12987 statements (90.013090 percent).


## L-3856605940d0

- Symptom: Static security analysis reported unchecked numeric narrowing at WASM and generated-ID trust boundaries.
- Root cause: Guest ABI values and unsigned generator results were converted to narrower signed or unsigned Go types without explicit range validation.
- Prevention: Validate numeric bounds before every trust-boundary narrowing conversion and cover overflow, underflow, non-finite, and fractional inputs with real tests.
- Evidence: Focused race tests passed for pkg/interpreter and internal/infra/guid and the full repository quality gate passed at exact coverage 11739/13041.


## L-94354b665990

- Symptom: Static security analysis identified an unbounded GeoIP ZIP extraction path that could consume excessive disk space.
- Root cause: The archive entry was copied directly to its final database path without an extracted-size limit or cleanup after failure.
- Prevention: Bound extraction by policy, write private files, close writers explicitly, and remove partial outputs on copy, close, or size failure.
- Evidence: The real ZIP regression test rejected an oversized database and verified no partial file remained; pkg/geoip/providers race tests and the full repository quality gate passed.


## L-7d555a7dcce6

- Symptom: MySQL migration fails on fresh DB with FK errno 3780 and index errno 1071
- Root cause: role_id/subject_id widths diverged across tables (VARCHAR(32) vs 64/255) and MySQL tables lacked a uniform COLLATE, so implicit collation mismatches broke FK creation and utf8mb4 index bytes exceeded 3072
- Prevention: Keep referenced id columns byte-identical in width and collation across all dialect migrations; cap indexed varchar columns at 128 for utf8mb4; declare ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci on every MySQL table
- Evidence: IAM and provisioning Test*MigrationDialectLifecycle pass on real MySQL 8.0.42 (go test -race, count=1) after collation normalization


## L-13fc9a7d95cd

- Symptom: 开启 federation 的服务启动时报 federation schema is not ready
- Root cause: deployment.migrate/Rollback/assertRuntimeAccess 的模块列表漏掉了 federation 模块
- Prevention: 新模块必须同时接入部署迁移列表、运行时 schema 断言和 CLI 迁移入口，三者缺一不可
- Evidence: TestMigrateSQLite、TestRollbackRealSQLiteModules、TestMigrationStageFailures/federation 全部通过


## L-fa992af12e4e

- Symptom: govulncheck reported GO-2026-4753: goxmldsig < v1.6.0 has a loop-variable-capture flaw that lets an attacker bypass SAML signature validation
- Root cause: crewjam/saml indirect dependency pinned goxmldsig v1.4.0 via go.mod
- Prevention: For SAML signing in crewjam/saml, require goxmldsig >= v1.6.0 and go.sum pinning; run govulncheck in the full gate
- Evidence: go.mod upgraded goxmldsig@v1.6.0; go build, federation tests, and check-gates.ps1 -Scope all -Full all pass; govulncheck reports No vulnerabilities found


## L-84dd6858feed

- Symptom: Bun inserts appeared successful and returned an auto-increment value, but the intended model tables remained empty.
- Root cause: Persistent models omitted an explicit BaseModel table tag while queries attempted to override the inferred table with Table calls, which did not reliably bind model writes to the intended table.
- Prevention: Every Bun persistence model must declare its canonical table with bun.BaseModel and CRUD must use that model mapping without Table overrides; prove new repositories with a real database round trip.
- Evidence: The real SQLite conversation event and projection lifecycle test now persists one event and one projection, preserves idempotency, and rebuilds the deleted projection.
