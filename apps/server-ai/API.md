# server-ai API inventory

This is the inventory of routes registered by the current Go server. The
formal product UI is `apps/admin-ai`; server-ai does not serve an embedded HTML
application and `GET /` returns `404`.

All mutating routes require the server Bearer token when authentication is
enabled. Read-only routes follow the PRD's current token exemption. Tenant and
actor scope are supplied by the trusted identity boundary as `X-Entity` and
`X-Actor`; production rejects those headers from untrusted sources.

## Operations

| Method | Path | Purpose |
|---|---|---|
| GET | `/healthz` | Process liveness |
| GET | `/readyz` | NATS and state-store readiness |
| GET | `/api/stats/dashboard` | Run, approval, machine and cost summary |
| POST | `/api/email/notification` | Auth-exempt IAM email webhook; `CONTROL_EMAIL_WEBHOOK_SECRET` is required in production |

## Machine

| Method | Path | Purpose |
|---|---|---|
| POST | `/api/machines/tokens` | Begin machine onboarding and mint a bootstrap token |
| GET | `/api/machines/ws` | Machine WebSocket authenticated by machine token |
| GET | `/api/machines` | List entity-visible machines |
| GET | `/api/machines/{id}` | Machine detail, runtimes and hosted agents |
| POST | `/api/machines/{id}/confirm` | Confirm onboarding |
| POST | `/api/machines/{id}/onboarding-status` | Poll bootstrap state |
| DELETE | `/api/machines/{id}` | Cancel onboarding or remove machine |
| GET | `/api/machines/{id}/token` | Read in-memory token for a local setup command |
| POST | `/api/machines/{id}/refresh-token` | Rotate machine token |

## Workflow, run and artifact

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/workflows` | List workflow definitions |
| POST | `/api/workflows` | Create/update a workflow definition |
| DELETE | `/api/workflows/{id}` | Delete all versions in the current entity |
| POST | `/api/runs` | Validate and launch a workflow run |
| GET | `/api/runs` | List current and persisted runs |
| GET | `/api/runs/{id}` | Read run status and DAG snapshot |
| POST | `/api/runs/{id}/pause` | Pause an active run |
| POST | `/api/runs/{id}/resume` | Resume a paused run |
| POST | `/api/runs/{id}/cancel` | Cancel a non-terminal run |
| GET | `/api/runs/{id}/events` | Run event WebSocket |
| POST | `/api/runs/{id}/approvals/{node}` | Approve or reject a human gate |
| GET | `/api/artifacts` | List entity/project artifacts by status |
| POST | `/api/artifacts/{id}/force-valid` | Human override with audit event |
| POST | `/api/artifacts/reconcile` | Trigger an artifact scan |

## Conversation and digital human

| Method | Path | Purpose |
|---|---|---|
| GET, POST | `/api/agents` | List or create digital-human agents |
| PUT, DELETE | `/api/agents/{id}` | Update or delete an agent |
| POST | `/api/agents/{id}/status` | Start/stop lifecycle status |
| POST | `/api/agents/{id}/retire` | Normal or forced retirement |
| GET, POST | `/api/channels` | List or create channels |
| DELETE | `/api/channels/{id}` | Dissolve a channel transactionally |
| GET, POST | `/api/channels/{id}/members` | List or add channel members |
| DELETE | `/api/channels/{id}/members/{memberId}/{kind}` | Remove a channel member |
| GET, POST | `/api/channels/{id}/messages` | List or post messages |
| GET | `/api/channels/{id}/events` | Channel message WebSocket |
| GET | `/api/channels/{id}/execution` | Current in-memory execution progress |
| POST | `/api/invite-human` | Send a human invitation email |

## Workspace module

Workspace is a v2+ PRD area. These routes exist and are isolated for safety,
but they do not count toward completion of the v1 execution-kernel journey.

| Method | Path | Purpose |
|---|---|---|
| GET, POST | `/api/work-items` | List or create work items |
| GET, PUT, DELETE | `/api/work-items/{id}` | Read, update or delete one work item |
| POST | `/api/work-items/{id}/execute` | Launch the work-item workflow |
| GET, POST | `/api/work-items/{id}/attachments` | List or upload work-item attachments |
| POST | `/api/channels/{id}/work-items` | Create a channel-linked work item |
| GET, POST | `/api/channels/{id}/attachments` | List or upload channel attachments |
| GET | `/api/attachments/{id}` | Serve an entity-scoped attachment |
| GET, POST | `/api/okrs` | List or create OKRs |
| PUT, DELETE | `/api/okrs/{id}` | Update or delete an OKR |

## Known contract gaps

- The service uses hand-written `net/http` REST rather than the PRD's formal
  gRPC/gateway contract and generated schema.
- Mutating requests do not consistently expose `client_request_id`; therefore
  the PRD-wide RPC idempotency contract is incomplete.
- There is no versioned `/api/v1` prefix or generated OpenAPI document.
- Conversation execution progress is process memory, not a rebuildable query.
- Production browser session and HTTP/WS identity propagation are not complete.
