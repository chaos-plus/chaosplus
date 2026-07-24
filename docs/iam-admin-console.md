# IAM admin console

Status: implemented on `feat/iam-admin-console`.

## Security boundary

- Zitadel owns identities, password verification, MFA, registration, and OIDC tokens.
- Chaosplus stores tenant membership metadata, roles, menu metadata, and the
  authorization outbox. Direct login passes the password to Zitadel over the
  server-side Session API and never stores or logs it.
- SpiceDB is the final permission decision source.
- The browser uses Authorization Code + PKCE through a Go BFF. Access and
  refresh tokens are AES-GCM encrypted in Redis; JavaScript only receives an
  opaque `HttpOnly`, `SameSite=Lax` session cookie.
- `POST /authn/login` completes the PKCE and OIDC callback steps server-side,
  so a password-only user stays on the Chaosplus origin. Accounts governed by
  forced MFA are rejected with `409` and continue through the full Zitadel UI.
- Cookie-authenticated mutations require an exact allowed `Origin`.
- `POST /authn/logout` destroys the Redis session, best-effort revokes the
  refresh token at Zitadel, and returns a `logout_url` (RP-initiated logout via
  `end_session_endpoint` with `id_token_hint`) the browser must visit so the
  Zitadel SSO session ends too.
- Every guarded tenant route first requires an active `iam_tenant_members`
  row, then checks SpiceDB. `X-Tenant-Id` is only a tenant selector.

Production must use HTTPS, set `authn.web.cookie_secure=true`, use an external
32-byte encryption secret, restrict Redis network access, and configure exact
return URL and Origin allowlists.

## User lifecycle

`POST /iam/members` binds an existing immutable Zitadel `sub` to a tenant. It
does not create a Zitadel account. Self-registration begins at
`GET /authn/oidc/start?mode=register`; Zitadel handles the registration UI and
credential lifecycle.

Tenant member status is `active` or `disabled`. A disabled member is rejected
before SpiceDB, even when old role relationships still exist. Adding a subject
to a role also requires that subject to be an active member of the same tenant.

## APIs

Browser authentication:

- `POST /authn/login`
- `GET /authn/oidc/start`
- `GET /authn/oidc/callback`
- `GET /authn/session`
- `POST /authn/logout`

Tenant members:

- `GET|POST /iam/members`
- `GET|PATCH /iam/members/{subject}`
- `GET /iam/members/{subject}/roles`

Roles and grants:

- `GET|POST /iam/roles`
- `GET|PATCH|DELETE /iam/roles/{role_id}`
- `GET|PUT|DELETE /iam/roles/{role_id}/permissions/{permission_code}`
- `GET|PUT|DELETE /iam/roles/{role_id}/members/{subject}`

Menus:

- `GET|POST /iam/menus`
- `GET|PATCH|DELETE /iam/menus/{menu_id}`
- `GET /iam/me/menus`

Persisted menu permission codes must exist in the code-first authorization
catalog. Parent menus are tenant-local and update validation rejects cycles.
Effective menus use SpiceDB `CheckBulkPermissions` in chunks of 100. Any bulk
request or item failure rejects the whole menu response; allowed descendants
retain their ancestor containers.

## Frontend

The React/TypeScript/Vite application is under `web/admin`. It has login,
registration, tenant member, role/permission, and menu management screens. It
does not store tokens or make authorization decisions. Build and test with:

```text
cd web/admin
npm install
npm run build
npm test
npm run test:e2e
```
