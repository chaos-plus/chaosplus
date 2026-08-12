# server-ai / admin-ai production readiness review

- Review date: 2026-08-12
- Scope: `apps/server-ai`, `apps/admin-ai`, and the execution-critical `apps/runner`
- Product baseline: `PRD.md` v2.0 Final
- Architecture baseline: `apps/server` module lifecycle and bounded-context direction
- Release decision: **NO-GO**

## Executive decision

The system is materially safer than the previous baseline. This review removed
the embedded debug UI, isolated workspace routes into a bounded context, fixed
workspace and conversation IDOR paths, added per-run leases and fencing,
atomically commits events with projections, added heartbeat recovery, prevented
stale artifact reconciliation writes, and established a production boundary
for official-image NATS deployment.

It is still not a production release. PRD v1 is not fully implemented and the
architecture is only partially aligned with `apps/server`. The remaining
release blockers are runtime contract enforcement, convergence rules,
production identity, operational evidence and desktop distribution. Workspace
and OKR are v2+ PRD features; making them safer does not compensate for missing
v1 execution-kernel requirements.

## PRD v1 coverage

| Capability | Status | Evidence and gap |
|---|---|---|
| Machine onboarding | Implemented | Bootstrap token state, confirmation, refresh, disconnect/reconnect and tenant tests exist. Clean-machine J3 evidence is still required. |
| Workflow schema and static DAG | Partial | Schema validation and core node types work. `subworkflow` is declared but rejected before execution. |
| ExecutorAgentSpec enforcement | Safe rejection | Unsupported `forbiddenActions`, MCP allowlist, required skills, hooks and input validator fail closed. Rejection is safer than bypass but is not implementation. |
| Runner execution | Partial | Claude/Codex/script/http adapters and runner tests exist. The full software-development workflow with two real executors has not been accepted. |
| Artifact authority | Partial | Produced artifacts, checksums, dependency staleness, human override and CAS reconciliation exist. Full lifecycle states and artifact-store abstraction are incomplete. |
| Validators | Partial | Command validators and human approval exist. General adapter routing and `ai_assisted` validators are missing. |
| Reconciliation | Partial | Periodic checksum scan, orphan/invalid projection and stale propagation exist. Missing default five-minute policy, event debounce/batching, mtime/size preflight, depth limit and worktree governance. |
| Bounded autonomy | Partial | Bounded retries, backoff, exhausted-retry pause, structured rejection feedback and heartbeat retry/pause exist. `notifyThreshold`, `<0.08` output similarity, A-B oscillation and repeated missing-artifact detection are missing. |
| Event sourcing | Partial | Run events and core projections commit atomically and WRT tests rebuild projections. Several platform contexts are not yet unified append-only event sources. |
| Lease/fencing | Implemented | Per-run lease, monotonic fencing token, takeover and stale writer rejection have tests. |
| Crash/heartbeat recovery | Implemented for covered run path | Startup takeover and lost runner heartbeat retries/pause are covered; multi-node/network partition drills remain absent. |
| Conversation/approval | Partial | Channels, messages, routing, approval feedback and agent replies exist. Full event-sourced channel projection and production identity are incomplete. |
| Digital-human lifecycle | Partial | CRUD/status/retire routes exist. Runner-hosted lifecycle, normal retirement handover artifact and memory audit are not fully accepted. |
| Production authentication | Blocked | Bearer and trusted-proxy checks exist, but normal Get/List/Subscribe requests remain anonymously readable by PRD rule. Without an injected entity, repository filters may become global. Browser session and HTTP/WS identity renewal are not complete. |
| Desktop profile | Not proven | No signed/notarized Win/macOS/Linux artifacts, installer/upgrade evidence or clean first-run J1 evidence was found. |
| NFR and operations | Not proven | No 10x100 benchmark, eight-hour soak, backup/restore drill, disk-full test, SLO dashboard, alerts or production runbook. |

## J1-J10 journey

| Step | Status | Release evidence |
|---|---|---|
| J1 install and first launch | Not proven | Three-platform packages and automatic local runner startup are absent. |
| J2 create project/workspace and `# ALL` | Partial | Workspace/run paths and channels exist; clean first-run creation and early path/permission validation are not accepted. |
| J3 connect a second machine | Implemented in code | State-machine tests exist; retain a clean-network manual record before release. |
| J4 create workflow-only digital human | Partial | CRUD exists; hosted process lifecycle and handover contract remain incomplete. |
| J5 configure executor secret by `$env` | Not proven | `.chaosplus.env` permission, redaction and user workflow are not accepted. |
| J6 mention agent to launch template | Partial | Routing code exists; the exact `# ALL` to `software-dev-agile` journey lacks release evidence. |
| J7 approve PRD/architecture in chat | Partial | Approval persistence and feedback work; production identity and full card flow are not accepted. |
| J8 unattended validated execution | Blocked | Similarity/oscillation convergence and soak evidence are missing. |
| J9 structured intervention | Partial | Retry exhaustion pauses and feedback is structured; notify escalation is incomplete. |
| J10 human acceptance and replay | Partial | Human events and WRT exist; complete clean-machine J1-J10 acceptance is missing. |

## Architecture review

`cmd/server-ai` is now the composition root and the HTTP host accepts feature
registrars. `internal/modules/workspace` is the only current bounded context
with consistent domain types, application service, owned ports and REST
adapter. Its SQLite repository, run executor and chat notifier are adapters.

`internal/store` is not a DDD module. It is a shared SQLite adapter and
migration owner containing persistence implementations for multiple contexts.
Business rules must not continue to accumulate there.

The following remain mixed technical layers and must be migrated:

1. `conversation/digital-human`: routes, application orchestration and
   integration behavior remain in `internal/server/chat.go`.
2. `workflow/run`: the engine is isolated, but command/query orchestration and
   HTTP/WS behavior remain in `internal/server`.
3. `artifact`: reconciliation, status commands and storage are divided by
   technical layer, not a bounded context.
4. `machine` and `notification`: domain helpers exist but central server routes
   still own application behavior.

The target dependency direction and migration order are recorded in
`apps/server-ai/ARCHITECTURE.md`. Actual endpoints are inventoried in
`apps/server-ai/API.md`.

## Security and reliability findings

### P0 release blockers

1. **Production identity is fail-open for reads.** Anonymous Get/List/Subscribe
   is a PRD contract, but it conflicts with cloud/self-hosted tenant isolation.
   Production must require an authenticated session or a verified proxy entity
   and reject empty tenant identity.
2. **Execution contracts are not enforced.** Required tools/skills, forbidden
   actions, hooks and input validators cannot run; current fail-closed behavior
   prevents silent bypass but blocks compliant workflows.
3. **Convergence is incomplete.** Output similarity, oscillation and repeated
   missing-artifact stuck detection are execution-kernel invariants, not polish.
4. **Release evidence is absent.** Desktop packages, clean J1-J10 acceptance,
   performance/soak, backup restore and failure drills are mandatory.

### P1 high priority

1. Complete the validator bus, AI-assisted evidence and validator audit model.
2. Align reconciliation with the PRD policy and expose metrics for checked,
   changed, orphaned, skipped-CAS and scan duration.
3. Move conversation, run and artifact into modules matching `apps/server`.
4. Add API-wide idempotency (`client_request_id`), versioned generated
   contracts, rate limiting and authorization tests per resource/action.
5. Add Prometheus/OpenTelemetry, structured audit fields, alerts and runbooks.
6. Remove process-memory conversation execution projections or make them
   rebuildable from the unified event log.

## NATS deployment boundary

`apps/server-ai/cmd/nats` is explicitly a local-development fallback and
refuses `CONTROL_ENV=production`. Development uses the official Docker image
when Docker/Compose is available and falls back to the command only when it is
not. Production configuration requires:

- official `nats:2.14.4-alpine` image (or explicitly reviewed pinned upgrade);
- `CONTROL_NATS_DEPLOYMENT=official-image`;
- `tls://` URL without embedded credentials;
- token and trusted CA, with optional client cert/key pair;
- persistent JetStream storage and a tested backup/restore procedure.

Docker is not installed in this review environment. Compose files and shell
syntax were inspected, but the official image was not started here. Per the
request, `cmd/nats` tests were not run.

## UI/UX/UE review

The workspace and OKR surfaces now distinguish loading, load failure and empty
state; provide retry; use visible form labels; expose validation errors; use
44px primary interaction targets; add delete confirmation/progress; and make
work-item titles keyboard reachable. OKR supports create, edit and delete, and
progress is calculated as each key result's completion ratio rather than the
mean of raw values. Structural status emoji were removed from workspace
messages in favor of structured status fields.

Remaining UI release risks:

- Shared button defaults are 28-32px for common variants, below the 44px target
  for touch-critical use. A global change needs screenshot regression coverage.
- Session/chat has compact 32-36px icon and action controls; some truncation
  surfaces do not provide the full value.
- Production login/session renewal and WebSocket reconnect identity are not a
  coherent user experience.
- Browser viewport, dark mode, keyboard-only, screen reader and contrast
  acceptance could not be executed because the in-app browser control entry
  point is unavailable in this session. Static review/build is not a substitute.

## Verification evidence

Passed during this review:

```text
go vet ./cmd/server-ai ./internal/...
go run honnef.co/go/tools/cmd/staticcheck@latest ./cmd/server-ai ./internal/...
go run golang.org/x/vuln/cmd/govulncheck@latest ./cmd/server-ai ./internal/...
bun run lint && bun run typecheck && bun run test && bun run build  # admin-ai
bun run typecheck && bun test                                      # runner
sh -n deploy/nats/start-dev.sh
```

Admin tests: 121 passed (85 shared UI + 36 platform), 0 failed. Runner tests:
37 passed, 0 failed. `govulncheck` found no reachable vulnerabilities; one
required module has a reported vulnerability but no affected symbol is called.

The first Go race/coverage run exposed a real SQLite `SQLITE_BUSY` failure
between workspace progress projection and approval event commit. The store now
uses SQLite immediate transactions, and persist-before-memory ordering prevents
waiting/terminal states from becoming visible before their events commit. The
targeted race regressions and the final full race/coverage command passed.

Final Go coverage from the race run:

| Package | Coverage |
|---|---:|
| `cmd/server-ai` | 19.4% |
| `internal/gateway` | 86.3% |
| `internal/machine` | 80.6% |
| `internal/modules/workspace` | 20.3% |
| `internal/server` | 70.6% |
| `internal/store` | 62.3% |
| `internal/websec` | 100.0% |
| `internal/workflow` | 72.5% |

`bun audit --production` did not produce a result because the configured audit
endpoint returned HTTP 404. Run SCA against a working registry before release.

## GO criteria

Change this decision only after all of the following are recorded:

- Executor contract enforcement, validator layers and convergence rules pass
  unit, integration and fault-injection tests.
- Production auth/session/tenant behavior is fail-closed for HTTP and WS.
- Conversation/run/artifact module ownership is accepted or has a time-bound
  architecture exception approved by the maintainers.
- J1-J10 passes on clean Win/macOS/Linux installations.
- 10 concurrent runs x 100 nodes, eight-hour soak, crash/network/disk-full and
  backup/restore drills meet the PRD targets.
- Metrics, traces, alerts, capacity baseline and operational runbooks exist.
- Browser desktop/mobile/dark/keyboard/screen-reader regression evidence and a
  successful dependency SCA report are archived.
