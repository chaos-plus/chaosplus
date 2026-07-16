# IAM authorization writes

Status: accepted for implementation on `feat/iam-authorization-writes`.

## Scope

This phase adds tenant-scoped role metadata and the first supported path for
writing SpiceDB relationships:

- role create, read, update, and delete;
- grant and revoke a catalog permission from a role;
- add and remove a Zitadel subject from a role;
- transactionally enqueue every relationship change for SpiceDB delivery.

Tenant, merchant, store, department, menu management, CheckBulk, and Redis
permission caching remain outside this phase.

## Ownership

Zitadel remains the source of truth for users and credentials. The Chaosplus
database owns role display metadata and the desired role bindings. SpiceDB is
the authorization decision source.

The first tables are:

- `iam_roles`;
- `iam_role_permissions`;
- `iam_role_members`;
- `authz_outbox`.

Role IDs are globally unique application IDs. Every role row and binding also
stores `tenant_id`; repository queries and mutations always include it.

## Relationship mapping

Permission grants write:

```text
tenant:<tenant>#<permission_code>_role@role:<role>#member
```

Role members write:

```text
role:<role>#member@user:<zitadel_subject>
```

The service validates permission codes against the code-first authz catalog
before opening a transaction.

## Transactional outbox

`authz_outbox` is a desired-state dispatcher, not an append-only audit log.
The SHA-256 hash of `tenant_id` and the canonical SpiceDB relationship string
is unique, so one row represents the latest desired operation (`TOUCH` or
`DELETE`) for that relationship. The complete tuple is retained in non-null
columns but is not used as a wide MySQL index.

Each business transaction updates the binding row and upserts its outbox row.
The upsert increments `version`. When a worker already owns the row, the upsert
keeps `processing`, `locked_by`, and `locked_at`; otherwise it resets the row to
`pending`. A new business change revives a `dead` row.

Workers claim rows without dialect-specific locking syntax:

1. Select due `pending` candidate IDs.
2. For each ID, conditionally update it to `processing` with a worker ID.
3. Treat `RowsAffected == 1` as a successful claim.
4. Read the claimed operation and version in the same short transaction.
5. Write the idempotent operation to SpiceDB outside the transaction.
6. Mark delivered only when ID, version, and worker ID still match.
7. If the version changed while delivering, unlock it as `pending` so the
   latest desired state is delivered next.

Stale processing locks are returned to pending. Failures use bounded
exponential backoff and become `dead` after the configured attempt limit.
Successful writes store their ZedToken.

The initial defaults are a batch size of 32, a 250 ms poll interval, a 30
second lock timeout, a 10 second delivery deadline, eight attempts, and
exponential backoff from 250 ms to 30 seconds. The deadline must remain shorter
than the lock timeout.

A reclaimed worker can still report a late remote success. If a newer opposite
operation was already delivered, that late result may have overwritten the
desired SpiceDB state. Completion therefore requeues the latest terminal state
when it detects a higher version with a different operation. It never steals a
row that a newer worker is still processing.

If SpiceDB succeeds and the database acknowledgement fails, retrying is safe
because exact TOUCH and DELETE operations are idempotent.

## Role deletion

Role deletion enumerates local permission and member bindings first. In one
database transaction it enqueues DELETE desired states for every relationship,
deletes the bindings, and deletes the role. SpiceDB revocation is eventually
consistent; callers can observe delivery state but database rollback is never
attempted after a remote write.

This first implementation targets at most 10,000 members per role. A separate
deleting state and chunked revocation workflow for larger roles is deferred.

## Bootstrap

Initial tenant administration will be a separate CLI command that talks to the
database and SpiceDB with operator credentials. No public HTTP bootstrap route
will be introduced. Automated acceptance tests may create and clean up their
isolated tenant-admin relationship directly through the SpiceDB test client.
