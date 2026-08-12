# server-ai architecture

This document records the architecture that exists today and the target
boundary used for production reviews. It deliberately does not describe the
whole service as DDD-compliant: only the workspace context currently follows
the module and port direction consistently.

## Composition root

`cmd/server-ai/main.go` is the composition root. It owns configuration,
process-level clients, module construction, HTTP server lifecycle and graceful
shutdown. Feature packages must not read process configuration or construct
infrastructure clients behind the composition root, except for temporary
legacy code called out below.

The HTTP server accepts `RESTRegistrar` modules. Cross-cutting middleware
(authentication, tenant/actor context, security headers and recovery) remains
in `internal/server`; feature modules register their own routes.

```text
cmd/server-ai (composition root)
  -> internal/server (HTTP host and legacy application orchestration)
  -> internal/modules/* (bounded contexts)
       REST adapter -> application service -> domain-owned ports
                                           -> infrastructure adapters
```

## Current bounded contexts

| Context | Current location | State |
|---|---|---|
| Workspace | `internal/modules/workspace` | Module boundary exists. Domain types and validation, application service, repository/executor/notifier ports and REST adapter are separated. |
| Workflow / run | `internal/workflow`, `internal/server/runs*.go` | Partial boundary. Engine types are isolated, but run application orchestration and HTTP handlers remain mixed in `internal/server`. |
| Artifact | `internal/store/store_artifact.go`, `internal/server/artifacts.go` | Legacy. Persistence, reconciliation orchestration and HTTP are split by technical layer, not by bounded context. |
| Conversation / digital human | `internal/server/chat.go`, `internal/store/store_chat.go` | Legacy. A large service owns routes, application behavior and integration concerns. |
| Machine | `internal/machine`, `internal/server/server.go` | Partial boundary. Hub/domain behavior is separated; REST application behavior remains in the central handler. |
| Notification / email | `internal/server/email.go` | Legacy adapter and application behavior are coupled. |

## Workspace dependency direction

`internal/modules/workspace` owns its vocabulary and ports:

```text
workspace/rest.go
       |
       v
workspace/Service
   |       |       |
   v       v       v
Repository Executor Notifier       (workspace-owned ports)
   ^       ^       ^
   |       |       |
SQLite   RunManager ChatService     (adapters wired by the composition root)
```

The SQLite store implements the repository port. `WorkspaceExecutor` and the
chat notifier live outside the workspace domain because they translate between
bounded contexts. Tenant and owner context is preserved across background run
execution.

## What `internal/store` is

`internal/store` is the current SQLite infrastructure adapter and migration
owner. It is not a business module and it is not a DDD domain layer. It
contains persistence models and repository implementations for several
contexts because the original service was organized by technical layer.

New domain behavior must not be added to `internal/store`. A bounded context
defines repository interfaces using its own domain types; store code may adapt
those interfaces to Bun/SQLite. Cross-context transactions must be exposed as
explicit application operations rather than sharing Bun models with handlers.

## Production invariants

- A run has one writer lease and a monotonic fencing token.
- Run events and rebuildable projections commit in one transaction.
- Reconciliation status events use checksum compare-and-swap so an old scan
  cannot overwrite a newer producer result.
- HTTP requests receive entity and actor scope before reaching modules.
- Resource lookups are entity-scoped; callers must not fetch globally and
  authorize afterward.
- Long-running work uses the process context while preserving the request's
  entity and owner identity.
- `cmd/nats` is development-only. Production uses a pinned official NATS image.

## Required migration order

To reach parity with the `apps/server` module style, migrate contexts in this
order without changing public API behavior:

1. Conversation/digital-human: domain model, channel/agent repositories,
   application service, REST/WS adapters and workflow notification port.
2. Workflow/run: run command/query service, scheduler port, event store port,
   REST/WS adapter and runner gateway adapter.
3. Artifact: repository and artifact-store ports, validator/reconciliation
   application services, REST adapter.
4. Machine and notification: move remaining central handlers into modules.
5. Reduce `internal/server` to the HTTP host, middleware and module lifecycle.

Until these migrations are complete, the architecture is only partially
aligned with `apps/server` and must be tracked as production architecture debt.
