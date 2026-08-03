import { afterEach, describe, expect, it } from "bun:test"
import {
  createIamApi,
  type AuditEvent,
  type DepartmentInput,
  type OAuthClientInput,
} from "./iam-api"

interface RecordedRequest {
  method: string
  path: string
  headers: Headers
  body: string
}

describe("Chaosplus IAM API client", () => {
  let server: ReturnType<typeof Bun.serve> | undefined
  afterEach(() => server?.stop(true))

  it("uses real HTTP, cookies and the selected tenant", async () => {
    const requests: RecordedRequest[] = []
    server = Bun.serve({
      port: 0,
      async fetch(request) {
        requests.push({
          method: request.method,
          path: new URL(request.url).pathname,
          headers: request.headers,
          body: await request.text(),
        })
        return Response.json({ code: 0, message: "ok", data: [] })
      },
    })
    const client = createIamApi(
      `http://127.0.0.1:${server.port}`,
      () => "tenant-a"
    )
    await client.roles()
    await client.changePassword({
      current_password: "old secure password",
      new_password: "new secure password",
    })
    expect(requests[0]?.path).toBe("/iam/roles")
    expect(requests[0]?.headers.get("x-tenant-id")).toBe("tenant-a")
    expect(requests[1]?.path).toBe("/authn/password/change")
    expect(requests[1]?.headers.get("x-tenant-id")).toBeNull()
    expect(JSON.parse(requests[1]?.body ?? "{}")).toEqual({
      current_password: "old secure password",
      new_password: "new secure password",
    })
  })

  it("uses tenant-scoped access governance contracts", async () => {
    const requests: RecordedRequest[] = []
    server = Bun.serve({
      port: 0,
      async fetch(request) {
        requests.push({
          method: request.method,
          path: new URL(request.url).pathname,
          headers: request.headers,
          body: await request.text(),
        })
        return Response.json({ code: 0, message: "ok", data: [] })
      },
    })
    const client = createIamApi(
      `http://127.0.0.1:${server.port}`,
      () => "tenant-governance"
    )

    await client.requestableRoles()
    await client.myAccessRequests()
    await client.accessRequests()
    await client.createAccessRequest({
      role_id: "role/a",
      reason: "temporary operations",
      access_expires_at: "2026-08-04T12:00:00Z",
    })
    await client.approveAccessRequest("request/a", "approved")
    await client.rejectAccessRequest("request/b", "rejected")
    await client.revokeAccessRequest("request/c", "revoked")
    await client.withdrawAccessRequest("request/d", "withdrawn")
    await client.accessReviews()
    await client.accessReview("review/a")
    await client.createAccessReview({
      name: "Quarterly access review",
      due_at: "2026-08-10T12:00:00Z",
    })
    await client.decideAccessReviewItem("review/a", "item/a", "keep", "valid")
    await client.completeAccessReview("review/a")
    await client.cancelAccessReview("review/b")

    expect(requests.map(({ method, path }) => `${method} ${path}`)).toEqual([
      "GET /iam/requestable-roles",
      "GET /iam/my/access-requests",
      "GET /iam/access-requests",
      "POST /iam/access-requests",
      "POST /iam/access-requests/request%2Fa/approve",
      "POST /iam/access-requests/request%2Fb/reject",
      "POST /iam/access-requests/request%2Fc/revoke",
      "POST /iam/access-requests/request%2Fd/withdraw",
      "GET /iam/access-reviews",
      "GET /iam/access-reviews/review%2Fa",
      "POST /iam/access-reviews",
      "POST /iam/access-reviews/review%2Fa/items/item%2Fa/decide",
      "POST /iam/access-reviews/review%2Fa/complete",
      "POST /iam/access-reviews/review%2Fb/cancel",
    ])
    expect(
      requests.every(
        ({ headers }) => headers.get("x-tenant-id") === "tenant-governance"
      )
    ).toBe(true)
    expect(JSON.parse(requests[3]?.body ?? "{}")).toEqual({
      role_id: "role/a",
      reason: "temporary operations",
      access_expires_at: "2026-08-04T12:00:00Z",
    })
    expect(JSON.parse(requests[4]?.body ?? "{}")).toEqual({ note: "approved" })
    expect(JSON.parse(requests[6]?.body ?? "{}")).toEqual({
      reason: "revoked",
    })
    expect(JSON.parse(requests[10]?.body ?? "{}")).toEqual({
      name: "Quarterly access review",
      due_at: "2026-08-10T12:00:00Z",
    })
    expect(JSON.parse(requests[11]?.body ?? "{}")).toEqual({
      decision: "keep",
      note: "valid",
    })
  })

  it("maps response failures and empty success bodies", async () => {
    let count = 0
    server = Bun.serve({
      port: 0,
      fetch() {
        count++
        if (count === 1)
          return Response.json(
            { code: 403, message: "forbidden", data: null },
            { status: 403 }
          )
        return new Response(null, { status: 204 })
      },
    })
    const client = createIamApi(`http://127.0.0.1:${server.port}`)
    await expect(client.roles()).rejects.toMatchObject({
      status: 403,
      message: "forbidden",
    })
    expect(await client.logout()).toBeUndefined()
  })

  it("preserves the localized service error and stable error code", async () => {
    server = Bun.serve({
      port: 0,
      fetch() {
        return Response.json(
          {
            code: "last_tenant_administrator",
            message:
              "Pentadbir aktif terakhir penyewa tidak boleh dibuang atau dinyahaktifkan.",
            data: null,
          },
          {
            status: 409,
            headers: { "content-type": "application/problem+json" },
          }
        )
      },
    })
    const client = createIamApi(`http://127.0.0.1:${server.port}`)
    await expect(
      client.removeRoleMember("administrator", "principal")
    ).rejects.toMatchObject({
      status: 409,
      code: "last_tenant_administrator",
      message:
        "Pentadbir aktif terakhir penyewa tidak boleh dibuang atau dinyahaktifkan.",
    })
  })

  it("normalizes an empty effective menu response", async () => {
    server = Bun.serve({
      port: 0,
      fetch() {
        return Response.json({ code: 0, message: "ok", data: null })
      },
    })
    const client = createIamApi(`http://127.0.0.1:${server.port}`)
    expect(await client.effectiveMenus()).toEqual([])
  })

  it("uses public password recovery endpoints without tenant context", async () => {
    const requests: RecordedRequest[] = []
    server = Bun.serve({
      port: 0,
      async fetch(request) {
        requests.push({
          method: request.method,
          path: new URL(request.url).pathname,
          headers: request.headers,
          body: await request.text(),
        })
        return Response.json({
          code: 0,
          message: "ok",
          data: requests.length === 1 ? { accepted: true } : { changed: true },
        })
      },
    })
    const client = createIamApi(
      `http://127.0.0.1:${server.port}`,
      () => "tenant-must-not-leak"
    )

    expect(await client.startPasswordRecovery("operator@example.com")).toEqual({
      accepted: true,
    })
    expect(
      await client.completePasswordRecovery({
        token: "cpr1_recovery-value",
        new_password: "new secure password",
      })
    ).toEqual({ changed: true })
    expect(requests.map(({ method, path }) => `${method} ${path}`)).toEqual([
      "POST /authn/password/recovery/start",
      "POST /authn/password/recovery/complete",
    ])
    expect(requests.every(({ headers }) => !headers.has("x-tenant-id"))).toBe(
      true
    )
    expect(JSON.parse(requests[0]?.body ?? "{}")).toEqual({
      identifier: "operator@example.com",
    })
    expect(JSON.parse(requests[1]?.body ?? "{}")).toEqual({
      token: "cpr1_recovery-value",
      new_password: "new secure password",
    })
  })

  it("uses email verification endpoints without tenant context", async () => {
    const requests: RecordedRequest[] = []
    server = Bun.serve({
      port: 0,
      async fetch(request) {
        requests.push({
          method: request.method,
          path: new URL(request.url).pathname,
          headers: request.headers,
          body: await request.text(),
        })
        return Response.json({
          code: 0,
          message: "ok",
          data: requests.length === 1 ? { accepted: true } : { verified: true },
        })
      },
    })
    const client = createIamApi(
      `http://127.0.0.1:${server.port}`,
      () => "tenant-must-not-leak"
    )

    expect(await client.startEmailVerification()).toEqual({ accepted: true })
    expect(
      await client.completeEmailVerification("cpe1_verification-value")
    ).toEqual({ verified: true })
    expect(requests.map(({ method, path }) => `${method} ${path}`)).toEqual([
      "POST /authn/email/verification/start",
      "POST /authn/email/verification/complete",
    ])
    expect(requests.every(({ headers }) => !headers.has("x-tenant-id"))).toBe(
      true
    )
    expect(requests[0]?.body).toBe("")
    expect(JSON.parse(requests[1]?.body ?? "{}")).toEqual({
      token: "cpe1_verification-value",
    })
  })

  it("uses public registration capabilities and registration without tenant context", async () => {
    const requests: RecordedRequest[] = []
    server = Bun.serve({
      port: 0,
      async fetch(request) {
        requests.push({
          method: request.method,
          path: new URL(request.url).pathname,
          headers: request.headers,
          body: await request.text(),
        })
        return Response.json({
          code: 0,
          message: "ok",
          data:
            request.method === "GET"
              ? { registration: true, password_recovery: true, passkey: false }
              : { accepted: true },
        })
      },
    })
    const client = createIamApi(
      `http://127.0.0.1:${server.port}`,
      () => "tenant-must-not-leak"
    )

    expect(await client.capabilities()).toEqual({
      registration: true,
      password_recovery: true,
      passkey: false,
    })
    expect(
      await client.register({
        email: "registered@example.com",
        password: "correct registration password",
        display_name: "Registered User",
      })
    ).toEqual({ accepted: true })
    expect(requests.map(({ method, path }) => `${method} ${path}`)).toEqual([
      "GET /authn/capabilities",
      "POST /authn/register",
    ])
    expect(requests.every(({ headers }) => !headers.has("x-tenant-id"))).toBe(
      true
    )
    expect(JSON.parse(requests[1]?.body ?? "{}")).toEqual({
      email: "registered@example.com",
      password: "correct registration password",
      display_name: "Registered User",
    })
  })

  it("uses the public login challenge and authenticated MFA endpoints", async () => {
    const requests: RecordedRequest[] = []
    server = Bun.serve({
      port: 0,
      async fetch(request) {
        requests.push({
          method: request.method,
          path: new URL(request.url).pathname,
          headers: request.headers,
          body: await request.text(),
        })
        const path = new URL(request.url).pathname
        if (path === "/authn/login")
          return Response.json({
            code: 0,
            message: "ok",
            data: {
              status: "mfa_required",
              return_url: "/",
              challenge_id: "challenge-id",
              methods: ["totp", "recovery_code"],
            },
          })
        if (path === "/authn/mfa")
          return Response.json({
            code: 0,
            message: "ok",
            data: { totp_enabled: true, recovery_codes_remaining: 8 },
          })
        return Response.json({
          code: 0,
          message: "ok",
          data:
            path.includes("recovery-codes") || path.includes("confirm")
              ? { recovery_codes: ["AAAA-BBBB-CCCC-DDDD"] }
              : path.includes("enroll")
                ? {
                    secret: "JBSWY3DPEHPK3PXP",
                    provisioning_uri: "otpauth://totp/Chaosplus:user",
                    expires_at: "2026-08-01T00:10:00Z",
                  }
                : path === "/authn/login/mfa"
                  ? { status: "authenticated", return_url: "/" }
                  : { disabled: true },
        })
      },
    })
    const client = createIamApi(
      `http://127.0.0.1:${server.port}`,
      () => "tenant-must-not-leak"
    )

    expect(
      await client.login({
        login_name: "operator",
        password: "password",
        return_url: "/",
      })
    ).toMatchObject({ status: "mfa_required", challenge_id: "challenge-id" })
    expect(
      await client.verifyLoginMfa({
        challenge_id: "challenge-id",
        code: "123456",
      })
    ).toMatchObject({ status: "authenticated" })
    expect(await client.mfaStatus()).toEqual({
      totp_enabled: true,
      recovery_codes_remaining: 8,
    })
    await client.beginTotpEnrollment("password")
    await client.confirmTotpEnrollment("123456")
    await client.regenerateRecoveryCodes({
      current_password: "password",
      code: "123456",
    })
    await client.disableTotp({
      current_password: "password",
      code: "123456",
    })

    expect(requests.map(({ method, path }) => `${method} ${path}`)).toEqual([
      "POST /authn/login",
      "POST /authn/login/mfa",
      "GET /authn/mfa",
      "POST /authn/mfa/totp/enroll",
      "POST /authn/mfa/totp/confirm",
      "POST /authn/mfa/recovery-codes/regenerate",
      "DELETE /authn/mfa/totp",
    ])
    for (const request of requests) {
      expect(request.headers.get("x-tenant-id")).toBeNull()
    }
    expect(JSON.parse(requests.at(-1)?.body ?? "{}")).toEqual({
      current_password: "password",
      code: "123456",
    })
  })

  it("uses the passkey ceremony and credential management contracts", async () => {
    const requests: RecordedRequest[] = []
    server = Bun.serve({
      port: 0,
      async fetch(request) {
        const path = new URL(request.url).pathname
        requests.push({
          method: request.method,
          path,
          headers: request.headers,
          body: await request.text(),
        })
        const passkey = {
          id: "a".repeat(64),
          name: "Work laptop",
          sign_count: 1,
          created_at: "2026-08-01T00:00:00Z",
        }
        const data = path.endsWith("/options")
          ? {
              challenge_id: "challenge-id",
              expires_at: "2026-08-01T00:05:00Z",
              options: { publicKey: { challenge: "AQID", rpId: "localhost" } },
            }
          : path.endsWith("/verify") && path.includes("/login/")
            ? { status: "authenticated", return_url: "/" }
            : request.method === "GET"
              ? [passkey]
              : request.method === "DELETE"
                ? { deleted: true }
                : passkey
        return Response.json({ code: 0, message: "ok", data })
      },
    })
    const client = createIamApi(
      `http://127.0.0.1:${server.port}`,
      () => "tenant-must-not-leak"
    )
    const credential = { id: "credential", type: "public-key" }

    expect(await client.passkeys()).toHaveLength(1)
    const registration = await client.beginPasskeyRegistration("password")
    await client.finishPasskeyRegistration({
      challenge_id: registration.challenge_id,
      name: "Work laptop",
      credential,
    })
    const login = await client.beginPasskeyLogin("/")
    await client.finishPasskeyLogin({
      challenge_id: login.challenge_id,
      credential,
    })
    await client.renamePasskey("a".repeat(64), "Security key")
    await client.deletePasskey("a".repeat(64), "password")

    expect(requests.map(({ method, path }) => `${method} ${path}`)).toEqual([
      "GET /authn/passkeys",
      "POST /authn/passkeys/registration/options",
      "POST /authn/passkeys/registration/verify",
      "POST /authn/passkey/login/options",
      "POST /authn/passkey/login/verify",
      `PATCH /authn/passkeys/${"a".repeat(64)}`,
      `DELETE /authn/passkeys/${"a".repeat(64)}`,
    ])
    for (const request of requests)
      expect(request.headers.get("x-tenant-id")).toBeNull()
    expect(JSON.parse(requests[1]?.body ?? "{}")).toEqual({
      current_password: "password",
    })
    expect(JSON.parse(requests.at(-1)?.body ?? "{}")).toEqual({
      current_password: "password",
    })
  })

  it("uses the tenant-scoped OAuth client management contract", async () => {
    const requests: RecordedRequest[] = []
    server = Bun.serve({
      port: 0,
      async fetch(request) {
        const path = new URL(request.url).pathname
        requests.push({
          method: request.method,
          path,
          headers: request.headers,
          body: await request.text(),
        })
        const client = {
          id: "client/a",
          tenant_id: "tenant-oauth",
          name: "Operations console",
          redirect_uris: ["https://console.example.test/callback"],
          grant_types: ["authorization_code", "refresh_token"],
          scopes: ["openid", "profile"],
          public_client: false,
          status: "active",
        }
        const data =
          request.method === "GET"
            ? [client]
            : path.endsWith("/rotate-secret")
              ? { client_secret: "rotated-secret" }
              : request.method === "DELETE"
                ? { deleted: true }
                : request.method === "POST"
                  ? { client, client_secret: "created-secret" }
                  : { ...client, status: "disabled" }
        return Response.json({ code: 0, message: "ok", data })
      },
    })
    const client = createIamApi(
      `http://127.0.0.1:${server.port}`,
      () => "tenant-oauth"
    )
    const input: OAuthClientInput = {
      name: "Operations console",
      redirect_uris: ["https://console.example.test/callback"],
      grant_types: ["authorization_code", "refresh_token"],
      scopes: ["openid", "profile"],
      public_client: false,
    }

    expect(await client.oauthClients()).toHaveLength(1)
    expect(await client.createOAuthClient(input)).toMatchObject({
      client_secret: "created-secret",
    })
    expect(
      await client.updateOAuthClient("client/a", {
        ...input,
        grant_types: [...input.grant_types],
        status: "disabled",
      })
    ).toMatchObject({ status: "disabled" })
    expect(await client.rotateOAuthClientSecret("client/a")).toEqual({
      client_secret: "rotated-secret",
    })
    expect(await client.deleteOAuthClient("client/a")).toEqual({
      deleted: true,
    })

    expect(requests.map(({ method, path }) => `${method} ${path}`)).toEqual([
      "GET /iam/oauth-clients",
      "POST /iam/oauth-clients",
      "PUT /iam/oauth-clients/client%2Fa",
      "POST /iam/oauth-clients/client%2Fa/rotate-secret",
      "DELETE /iam/oauth-clients/client%2Fa",
    ])
    for (const request of requests) {
      expect(request.headers.get("x-tenant-id")).toBe("tenant-oauth")
    }
    expect(JSON.parse(requests[1]?.body ?? "{}")).toEqual(input)
    expect(JSON.parse(requests[2]?.body ?? "{}")).toEqual({
      ...input,
      status: "disabled",
    })
  })

  it("uses the tenant-scoped SCIM directory and credential contract", async () => {
    const requests: RecordedRequest[] = []
    const directory = {
      id: "directory/a",
      tenant_id: "tenant-scim",
      name: "Workforce",
      status: "active" as const,
      version: 1,
      created_at: "2026-08-03T00:00:00Z",
      updated_at: "2026-08-03T00:00:00Z",
    }
    const credential = {
      id: "scim_credential/a",
      directory_id: directory.id,
      name: "production",
      created_at: "2026-08-03T00:00:00Z",
    }
    server = Bun.serve({
      port: 0,
      async fetch(request) {
        const path = new URL(request.url).pathname
        requests.push({
          method: request.method,
          path,
          headers: request.headers,
          body: await request.text(),
        })
        let data: unknown = directory
        if (request.method === "GET" && path.endsWith("/credentials"))
          data = [credential]
        else if (request.method === "GET") data = [directory]
        else if (request.method === "POST" && path.endsWith("/credentials"))
          data = { credential, bearer_token: "scim_id.shown-once" }
        else if (request.method === "DELETE") data = { revoked: true }
        return Response.json({ code: 0, message: "ok", data })
      },
    })
    const client = createIamApi(
      `http://127.0.0.1:${server.port}`,
      () => "tenant-scim"
    )

    expect(await client.scimDirectories()).toEqual([directory])
    await client.createSCIMDirectory({ name: "Workforce" })
    await client.updateSCIMDirectory(directory.id, {
      name: "Workforce",
      status: "disabled",
      version: 1,
    })
    expect(await client.scimCredentials(directory.id)).toEqual([credential])
    expect(
      await client.createSCIMCredential(directory.id, {
        name: "production",
        expires_at: "2027-08-03T00:00:00Z",
      })
    ).toMatchObject({ bearer_token: "scim_id.shown-once" })
    await client.revokeSCIMCredential(directory.id, credential.id)

    expect(requests.map(({ method, path }) => `${method} ${path}`)).toEqual([
      "GET /iam/scim/directories",
      "POST /iam/scim/directories",
      "PUT /iam/scim/directories/directory%2Fa",
      "GET /iam/scim/directories/directory%2Fa/credentials",
      "POST /iam/scim/directories/directory%2Fa/credentials",
      "DELETE /iam/scim/directories/directory%2Fa/credentials/scim_credential%2Fa",
    ])
    for (const request of requests)
      expect(request.headers.get("x-tenant-id")).toBe("tenant-scim")
  })

  it("uses the tenant-scoped service account and credential contract", async () => {
    const requests: RecordedRequest[] = []
    const account = {
      id: "service/account",
      login_name: "report-worker",
      display_name: "Report Worker",
      description: "Builds reports",
      status: "active" as const,
      version: 2,
      created_at: "2026-08-02T00:00:00Z",
      updated_at: "2026-08-02T00:00:00Z",
    }
    const credential = {
      id: "sac_credential/a",
      service_account_id: account.id,
      name: "automation",
      scopes: ["reports.read"],
      created_at: "2026-08-02T00:00:00Z",
    }
    server = Bun.serve({
      port: 0,
      async fetch(request) {
        const url = new URL(request.url)
        requests.push({
          method: request.method,
          path: `${url.pathname}${url.search}`,
          headers: request.headers,
          body: await request.text(),
        })
        let data: unknown = account
        if (request.method === "GET" && url.pathname.endsWith("/credentials"))
          data = [credential]
        else if (request.method === "GET") data = { items: [account], total: 1 }
        else if (
          request.method === "POST" &&
          url.pathname.endsWith("/credentials")
        )
          data = { credential, client_secret: "shown-once" }
        else if (
          request.method === "DELETE" &&
          url.pathname.includes("/credentials/")
        )
          data = { revoked: true }
        else if (request.method === "DELETE") data = { deleted: true }
        return Response.json({ code: 0, message: "ok", data })
      },
    })
    const client = createIamApi(
      `http://127.0.0.1:${server.port}`,
      () => "tenant-service"
    )

    expect((await client.serviceAccounts("report worker")).total).toBe(1)
    await client.createServiceAccount({
      login_name: "report-worker",
      display_name: "Report Worker",
      description: "Builds reports",
    })
    await client.updateServiceAccount(account.id, {
      display_name: "Report Worker",
      description: "Builds reports",
      status: "active",
      version: 1,
    })
    expect(await client.serviceAccountCredentials(account.id)).toEqual([
      credential,
    ])
    expect(
      await client.createServiceAccountCredential(account.id, {
        name: "automation",
        scopes: ["reports.read"],
      })
    ).toMatchObject({ client_secret: "shown-once" })
    await client.revokeServiceAccountCredential(account.id, credential.id)
    await client.deleteServiceAccount(account.id, account.version)

    expect(requests.map(({ method, path }) => `${method} ${path}`)).toEqual([
      "GET /iam/service-accounts?limit=200&search=report%20worker",
      "POST /iam/service-accounts",
      "PUT /iam/service-accounts/service%2Faccount",
      "GET /iam/service-accounts/service%2Faccount/credentials",
      "POST /iam/service-accounts/service%2Faccount/credentials",
      "DELETE /iam/service-accounts/service%2Faccount/credentials/sac_credential%2Fa",
      "DELETE /iam/service-accounts/service%2Faccount?version=2",
    ])
    for (const request of requests)
      expect(request.headers.get("x-tenant-id")).toBe("tenant-service")
  })

  it("uses the tenant department hierarchy contract", async () => {
    const requests: RecordedRequest[] = []
    const department = {
      id: "department/a",
      tenant_id: "tenant-organization",
      name: "Engineering",
      status: "active" as const,
      sort_order: 10,
      depth: 0,
      version: 1,
      created_at: "2026-08-02T00:00:00Z",
      updated_at: "2026-08-02T00:00:00Z",
    }
    server = Bun.serve({
      port: 0,
      async fetch(request) {
        const url = new URL(request.url)
        requests.push({
          method: request.method,
          path: `${url.pathname}${url.search}`,
          headers: request.headers,
          body: await request.text(),
        })
        const data =
          request.method === "DELETE"
            ? { deleted: true }
            : request.method === "GET" && url.pathname === "/iam/departments"
              ? [department]
              : request.method === "PATCH"
                ? { ...department, version: 2, name: "Platform" }
                : department
        return Response.json({ code: 0, message: "ok", data })
      },
    })
    const client = createIamApi(
      `http://127.0.0.1:${server.port}`,
      () => "tenant-organization"
    )
    const input: DepartmentInput = {
      parent_id: "",
      name: "Engineering",
      status: "active",
      sort_order: 10,
    }

    expect(await client.departments()).toEqual([department])
    expect(await client.department("department/a")).toEqual(department)
    expect(await client.createDepartment(input)).toEqual(department)
    expect(
      await client.updateDepartment("department/a", {
        ...input,
        name: "Platform",
        version: 1,
      })
    ).toMatchObject({ name: "Platform", version: 2 })
    expect(await client.deleteDepartment("department/a", 2)).toEqual({
      deleted: true,
    })

    expect(requests.map(({ method, path }) => `${method} ${path}`)).toEqual([
      "GET /iam/departments",
      "GET /iam/departments/department%2Fa",
      "POST /iam/departments",
      "PATCH /iam/departments/department%2Fa",
      "DELETE /iam/departments/department%2Fa?version=2",
    ])
    expect(
      requests.every(
        ({ headers }) => headers.get("x-tenant-id") === "tenant-organization"
      )
    ).toBe(true)
    expect(JSON.parse(requests[2]?.body ?? "{}")).toEqual(input)
    expect(JSON.parse(requests[3]?.body ?? "{}")).toEqual({
      ...input,
      name: "Platform",
      version: 1,
    })
  })

  it("uses encoded tenant administration mutation contracts", async () => {
    const requests: RecordedRequest[] = []
    server = Bun.serve({
      port: 0,
      async fetch(request) {
        requests.push({
          method: request.method,
          path: new URL(request.url).pathname,
          headers: request.headers,
          body: await request.text(),
        })
        return Response.json({ code: 0, message: "ok", data: [] })
      },
    })
    const client = createIamApi(
      `http://127.0.0.1:${server.port}`,
      () => "tenant-admin"
    )

    await client.updatePrincipal("principal/a", {
      display_name: "Alice",
      email: "alice@example.test",
    })
    await client.updateMember("principal/a", {
      status: "disabled",
      department_id: "department/a",
    })
    await client.updateRole("role/a", { name: "Operators" })
    await client.roleMembers("role/a")
    await client.addRoleMember("role/a", "principal/a")
    await client.removeRoleMember("role/a", "principal/a")
    await client.roleDirectoryBindings("role/a")
    await client.roleDataScope("role/a")
    await client.setRoleDataScope("role/a", {
      scope: "selected_departments",
      department_ids: ["department/a"],
    })
    await client.addRoleDirectoryBinding("role/a", "group", "group/a")
    await client.removeRoleDirectoryBinding("role/a", "position", "position/a")
    await client.rolePermissionGrants("role/a")
    await client.grantPermission("role/a", "user:view")
    await client.setRolePermissionCondition("role/a", "user:view", {
      version: 1,
      gte: [{ context: "auth.acr" }, { value: 2 }],
    })
    await client.clearRolePermissionCondition("role/a", "user:view")
    await client.revokePermission("role/a", "user:view")
    await client.updateMenu("menu/a", {
      label: "Operators",
      status: "disabled",
    })

    expect(requests.map(({ method, path }) => `${method} ${path}`)).toEqual([
      "PATCH /iam/principals/principal%2Fa",
      "PATCH /iam/members/principal%2Fa",
      "PATCH /iam/roles/role%2Fa",
      "GET /iam/roles/role%2Fa/members",
      "PUT /iam/roles/role%2Fa/members/principal%2Fa",
      "DELETE /iam/roles/role%2Fa/members/principal%2Fa",
      "GET /iam/roles/role%2Fa/directory-bindings",
      "GET /iam/roles/role%2Fa/data-scope",
      "PUT /iam/roles/role%2Fa/data-scope",
      "PUT /iam/roles/role%2Fa/directory-bindings/group/group%2Fa",
      "DELETE /iam/roles/role%2Fa/directory-bindings/position/position%2Fa",
      "GET /iam/roles/role%2Fa/permission-grants",
      "PUT /iam/roles/role%2Fa/permissions/user%3Aview",
      "PUT /iam/roles/role%2Fa/permissions/user%3Aview/condition",
      "DELETE /iam/roles/role%2Fa/permissions/user%3Aview/condition",
      "DELETE /iam/roles/role%2Fa/permissions/user%3Aview",
      "PATCH /iam/menus/menu%2Fa",
    ])
    expect(
      requests.every(
        ({ headers }) => headers.get("x-tenant-id") === "tenant-admin"
      )
    ).toBe(true)
    expect(JSON.parse(requests[0]?.body ?? "{}")).toEqual({
      display_name: "Alice",
      email: "alice@example.test",
    })
    expect(JSON.parse(requests[13]?.body ?? "{}")).toEqual({
      condition: {
        version: 1,
        gte: [{ context: "auth.acr" }, { value: 2 }],
      },
    })
    expect(JSON.parse(requests.at(-1)?.body ?? "{}")).toEqual({
      label: "Operators",
      status: "disabled",
    })
  })

  it("uses tenant-scoped invitation management and public acceptance", async () => {
    const requests: RecordedRequest[] = []
    server = Bun.serve({
      port: 0,
      async fetch(request) {
        requests.push({
          method: request.method,
          path: new URL(request.url).pathname,
          headers: request.headers,
          body: await request.text(),
        })
        return Response.json({ code: 0, message: "ok", data: [] })
      },
    })
    const client = createIamApi(
      `http://127.0.0.1:${server.port}`,
      () => "tenant-invitation"
    )

    await client.invitations()
    await client.createInvitation({
      email: "user@example.test",
      department_id: "department/a",
      role_ids: ["role/a"],
      expires_in_hours: 48,
    })
    await client.resendInvitation("invitation/a", 24)
    await client.revokeInvitation("invitation/a")
    await client.acceptInvitation({
      token: "cpi1_invitation/a.secret",
      login_name: "invited",
      password: "correct horse battery staple",
      display_name: "Invited User",
    })

    expect(requests.map(({ method, path }) => `${method} ${path}`)).toEqual([
      "GET /iam/invitations",
      "POST /iam/invitations",
      "POST /iam/invitations/invitation%2Fa/resend",
      "DELETE /iam/invitations/invitation%2Fa",
      "POST /iam/invitations/accept",
    ])
    expect(
      requests
        .slice(0, 4)
        .every(
          ({ headers }) => headers.get("x-tenant-id") === "tenant-invitation"
        )
    ).toBe(true)
    expect(requests[4]?.headers.get("x-tenant-id")).toBeNull()
    expect(JSON.parse(requests[2]?.body ?? "{}")).toEqual({
      expires_in_hours: 24,
    })
  })

  it("uses the platform tenant lifecycle contract without tenant context", async () => {
    const requests: RecordedRequest[] = []
    server = Bun.serve({
      port: 0,
      async fetch(request) {
        const url = new URL(request.url)
        requests.push({
          method: request.method,
          path: `${url.pathname}${url.search}`,
          headers: request.headers,
          body: await request.text(),
        })
        return Response.json({ code: 0, message: "ok", data: [] })
      },
    })
    const client = createIamApi(
      `http://127.0.0.1:${server.port}`,
      () => "tenant-must-not-leak"
    )

    await client.tenants(true)
    await client.createTenant({ slug: "acme", name: "Acme" })
    await client.updateTenant("tenant/a", {
      name: "Acme Global",
      status: "suspended",
      version: 2,
    })
    await client.deleteTenant("tenant/a", 3)

    expect(requests.map(({ method, path }) => `${method} ${path}`)).toEqual([
      "GET /iam/tenants?include_deleted=true",
      "POST /iam/tenants",
      "PATCH /iam/tenants/tenant%2Fa",
      "DELETE /iam/tenants/tenant%2Fa?version=3",
    ])
    expect(
      requests.every(({ headers }) => headers.get("x-tenant-id") === null)
    ).toBe(true)
    expect(JSON.parse(requests[1]?.body ?? "{}")).toEqual({
      slug: "acme",
      name: "Acme",
    })
    expect(JSON.parse(requests[2]?.body ?? "{}")).toEqual({
      name: "Acme Global",
      status: "suspended",
      version: 2,
    })
  })

  it("uses the complete position and position-member contract over real HTTP", async () => {
    const requests: RecordedRequest[] = []
    const position = {
      id: "position/a",
      tenant_id: "tenant-positions",
      code: "platform.engineer",
      name: "Platform Engineer",
      status: "active" as const,
      sort_order: 10,
      version: 7,
      created_at: "2026-08-02T12:00:00Z",
      updated_at: "2026-08-02T12:00:00Z",
    }
    const member = {
      position_id: position.id,
      principal_id: "principal/a",
      display_name: "Alice",
      starts_at: "2026-08-03T00:00:00Z",
      ends_at: "2026-09-03T00:00:00Z",
      created_at: "2026-08-02T12:00:00Z",
      updated_at: "2026-08-02T12:00:00Z",
    }
    server = Bun.serve({
      port: 0,
      async fetch(request) {
        const url = new URL(request.url)
        requests.push({
          method: request.method,
          path: `${url.pathname}${url.search}`,
          headers: request.headers,
          body: await request.text(),
        })
        let data: unknown = position
        if (request.method === "DELETE") data = { deleted: true }
        else if (url.pathname.endsWith("/members")) data = [member]
        else if (url.pathname.includes("/members/")) data = member
        else if (request.method === "GET" && url.pathname === "/iam/positions")
          data = [position]
        return Response.json({ code: 0, message: "ok", data })
      },
    })
    const client = createIamApi(
      `http://127.0.0.1:${server.port}`,
      () => "tenant-positions"
    )

    expect(await client.positions()).toEqual([position])
    expect(await client.position(position.id)).toEqual(position)
    await client.createPosition({
      code: position.code,
      name: position.name,
      status: position.status,
      sort_order: position.sort_order,
    })
    await client.updatePosition(position.id, {
      name: "Principal Platform Engineer",
      version: position.version,
    })
    expect(await client.deletePosition(position.id, position.version)).toEqual({
      deleted: true,
    })
    expect(await client.positionMembers(position.id)).toEqual([member])
    expect(
      await client.putPositionMember(position.id, member.principal_id, {
        starts_at: member.starts_at,
        ends_at: member.ends_at,
      })
    ).toEqual(member)
    expect(
      await client.deletePositionMember(position.id, member.principal_id)
    ).toEqual({ deleted: true })

    expect(requests.map(({ method, path }) => `${method} ${path}`)).toEqual([
      "GET /iam/positions",
      "GET /iam/positions/position%2Fa",
      "POST /iam/positions",
      "PATCH /iam/positions/position%2Fa",
      "DELETE /iam/positions/position%2Fa?version=7",
      "GET /iam/positions/position%2Fa/members",
      "PUT /iam/positions/position%2Fa/members/principal%2Fa",
      "DELETE /iam/positions/position%2Fa/members/principal%2Fa",
    ])
    expect(
      requests.every(
        ({ headers }) => headers.get("x-tenant-id") === "tenant-positions"
      )
    ).toBe(true)
    expect(JSON.parse(requests[2]?.body ?? "{}")).toEqual({
      code: position.code,
      name: position.name,
      status: position.status,
      sort_order: position.sort_order,
    })
    expect(JSON.parse(requests[3]?.body ?? "{}")).toEqual({
      name: "Principal Platform Engineer",
      version: position.version,
    })
    expect(JSON.parse(requests[6]?.body ?? "{}")).toEqual({
      starts_at: member.starts_at,
      ends_at: member.ends_at,
    })
  })

  it("uses the complete group and group-member contract over real HTTP", async () => {
    const requests: RecordedRequest[] = []
    const group = {
      id: "group/a",
      tenant_id: "tenant-groups",
      name: "Platform Operators",
      type: "dynamic" as const,
      membership_rule: {
        version: 1 as const,
        match: "all" as const,
        conditions: [
          {
            field: "member.email_domain" as const,
            operator: "in" as const,
            values: ["example.com"],
          },
        ],
      },
      description: "Production platform operators",
      status: "active" as const,
      sort_order: 10,
      version: 7,
      created_at: "2026-08-02T12:00:00Z",
      updated_at: "2026-08-02T12:00:00Z",
    }
    const member = {
      group_id: group.id,
      principal_id: "principal/a",
      display_name: "Alice",
      starts_at: "2026-08-03T00:00:00Z",
      ends_at: "2026-09-03T00:00:00Z",
      created_at: "2026-08-02T12:00:00Z",
      updated_at: "2026-08-02T12:00:00Z",
    }
    server = Bun.serve({
      port: 0,
      async fetch(request) {
        const url = new URL(request.url)
        requests.push({
          method: request.method,
          path: `${url.pathname}${url.search}`,
          headers: request.headers,
          body: await request.text(),
        })
        let data: unknown = group
        if (request.method === "DELETE") data = { deleted: true }
        else if (url.pathname.endsWith("/members")) data = [member]
        else if (url.pathname.includes("/members/")) data = member
        else if (request.method === "GET" && url.pathname === "/iam/groups")
          data = [group]
        return Response.json({ code: 0, message: "ok", data })
      },
    })
    const client = createIamApi(
      `http://127.0.0.1:${server.port}`,
      () => "tenant-groups"
    )

    expect(await client.groups()).toEqual([group])
    expect(await client.group(group.id)).toEqual(group)
    await client.createGroup({
      name: group.name,
      type: group.type,
      membership_rule: group.membership_rule,
      description: group.description,
      status: group.status,
      sort_order: group.sort_order,
    })
    await client.updateGroup(group.id, {
      name: "Security Operators",
      membership_rule: group.membership_rule,
      version: group.version,
    })
    expect(await client.deleteGroup(group.id, group.version)).toEqual({
      deleted: true,
    })
    expect(await client.groupMembers(group.id)).toEqual([member])
    expect(
      await client.putGroupMember(group.id, member.principal_id, {
        starts_at: member.starts_at,
        ends_at: member.ends_at,
      })
    ).toEqual(member)
    expect(
      await client.deleteGroupMember(group.id, member.principal_id)
    ).toEqual({ deleted: true })

    expect(requests.map(({ method, path }) => `${method} ${path}`)).toEqual([
      "GET /iam/groups",
      "GET /iam/groups/group%2Fa",
      "POST /iam/groups",
      "PATCH /iam/groups/group%2Fa",
      "DELETE /iam/groups/group%2Fa?version=7",
      "GET /iam/groups/group%2Fa/members",
      "PUT /iam/groups/group%2Fa/members/principal%2Fa",
      "DELETE /iam/groups/group%2Fa/members/principal%2Fa",
    ])
    expect(
      requests.every(
        ({ headers }) => headers.get("x-tenant-id") === "tenant-groups"
      )
    ).toBe(true)
    expect(JSON.parse(requests[2]?.body ?? "{}")).toEqual({
      name: group.name,
      type: group.type,
      membership_rule: group.membership_rule,
      description: group.description,
      status: group.status,
      sort_order: group.sort_order,
    })
    expect(JSON.parse(requests[3]?.body ?? "{}")).toEqual({
      name: "Security Operators",
      membership_rule: group.membership_rule,
      version: group.version,
    })
    expect(JSON.parse(requests[6]?.body ?? "{}")).toEqual({
      starts_at: member.starts_at,
      ends_at: member.ends_at,
    })
  })

  it("uses the complete entity and scoped role-binding contract over real HTTP", async () => {
    const requests: RecordedRequest[] = []
    const entity = {
      id: "entity/a",
      tenant_id: "tenant-entities",
      parent_id: "entity/root",
      type: "store",
      name: "Flagship",
      status: "active" as const,
      metadata: { region: "west" },
      created_at: "2026-08-02T12:00:00Z",
      updated_at: "2026-08-02T12:00:00Z",
    }
    const binding = {
      entity_id: entity.id,
      role_id: "role/a",
      principal_id: "principal/a",
      effect: "deny" as const,
      expires_at: "2026-09-03T00:00:00Z",
      created_at: "2026-08-02T12:00:00Z",
    }
    const relationshipInput = {
      subject_type: "principal" as const,
      subject_id: binding.principal_id,
      relation: "viewer" as const,
      resource_type: entity.type,
      resource_id: entity.id,
      starts_at: "2026-08-03T00:00:00Z",
      ends_at: "2026-09-03T00:00:00Z",
      condition: {
        version: 1 as const,
        all: [
          {
            gte: [{ context: "auth.acr" as const }, { value: 1 }] as [
              { context: "auth.acr" },
              { value: number },
            ],
          },
        ],
      },
    }
    const relationship = {
      ...relationshipInput,
      created_at: "2026-08-02T12:00:00Z",
    }
    const resourceRelationshipInput = {
      ...relationshipInput,
      entity_id: entity.id,
      resource_id: "business/store/a",
    }
    const resourceRelationship = {
      ...resourceRelationshipInput,
      created_at: relationship.created_at,
    }
    const constraint = {
      allow_all: true,
      owner_ids: [],
      resource_ids: [],
      department_ids: [],
      ancestors: [{ type: "entity", id: entity.parent_id }],
      denied_ids: [entity.id],
      revision: 7,
    }
    const explanation = {
      allowed: false,
      reason: "explicit_deny" as const,
      revision: 7,
      matches: [
        {
          permission_code: "store_view",
          role_id: binding.role_id,
          source_type: "entity_binding",
          source_id: entity.id,
          scope_type: "entity",
          scope_id: entity.id,
          effect: "deny" as const,
          inherited: false,
        },
      ],
    }
    server = Bun.serve({
      port: 0,
      async fetch(request) {
        const url = new URL(request.url)
        requests.push({
          method: request.method,
          path: `${url.pathname}${url.search}`,
          headers: request.headers,
          body: await request.text(),
        })
        let data: unknown = entity
        if (url.pathname === "/iam/authorization/constraints") data = constraint
        else if (url.pathname === "/iam/authorization/explain")
          data = explanation
        else if (url.pathname === "/iam/authorization/check")
          data = {
            allowed: true,
            reason: "relationship_grant",
            revision: 8,
          }
        else if (
          request.method === "GET" &&
          url.pathname === "/iam/relationships"
        )
          data = url.searchParams.has("entity_id")
            ? [resourceRelationship]
            : [relationship]
        else if (
          request.method === "POST" &&
          url.pathname === "/iam/relationships"
        )
          data = relationship
        else if (request.method === "DELETE")
          data = { changed: true, sync_status: "applied" }
        else if (url.pathname.endsWith("/role-bindings")) data = [binding]
        else if (url.pathname.includes("/role-bindings/")) data = binding
        else if (request.method === "GET" && url.pathname === "/iam/entities")
          data = [entity]
        return Response.json({ code: 0, message: "ok", data })
      },
    })
    const client = createIamApi(
      `http://127.0.0.1:${server.port}`,
      () => "tenant-entities"
    )

    expect(await client.entities()).toEqual([entity])
    expect(await client.entity(entity.id)).toEqual(entity)
    await client.createEntity({
      parent_id: entity.parent_id,
      type: entity.type,
      name: entity.name,
      status: entity.status,
      metadata: entity.metadata,
    })
    await client.updateEntity(entity.id, { name: "Downtown" })
    expect(await client.entityRoleBindings(entity.id)).toEqual([binding])
    expect(
      await client.putEntityRoleBinding(
        entity.id,
        binding.role_id,
        binding.principal_id,
        { effect: binding.effect, expires_at: binding.expires_at }
      )
    ).toEqual(binding)
    expect(
      await client.deleteEntityRoleBinding(
        entity.id,
        binding.role_id,
        binding.principal_id
      )
    ).toEqual({ changed: true, sync_status: "applied" })
    expect(await client.relationships(entity.id)).toEqual([relationship])
    expect(await client.putRelationship(relationshipInput)).toEqual(
      relationship
    )
    expect(JSON.parse(requests.at(-1)?.body ?? "{}")).toEqual(relationshipInput)
    expect(await client.deleteRelationship(relationshipInput)).toEqual({
      changed: true,
      sync_status: "applied",
    })
    expect(await client.resourceRelationships(entity.id)).toEqual([
      resourceRelationship,
    ])
    expect(await client.deleteRelationship(resourceRelationshipInput)).toEqual({
      changed: true,
      sync_status: "applied",
    })
    expect(
      await client.checkEntityAuthorization(
        entity.id,
        "store_view",
        binding.principal_id
      )
    ).toEqual({ allowed: true, reason: "relationship_grant", revision: 8 })
    expect(
      await client.checkResourceAuthorization(
        entity.id,
        "store",
        "business/store/a",
        "store_view",
        binding.principal_id
      )
    ).toEqual({ allowed: true, reason: "relationship_grant", revision: 8 })
    expect(
      await client.authorizationConstraint("store_view", binding.principal_id)
    ).toEqual(constraint)
    expect(
      await client.explainEntityAuthorization(
        entity.id,
        "store_view",
        binding.principal_id
      )
    ).toEqual(explanation)
    expect(
      await client.explainResourceAuthorization(
        entity.id,
        "store",
        "business/store/a",
        "store_view",
        binding.principal_id
      )
    ).toEqual(explanation)
    expect(await client.deleteEntity(entity.id)).toEqual({
      changed: true,
      sync_status: "applied",
    })

    expect(requests.map(({ method, path }) => `${method} ${path}`)).toEqual([
      "GET /iam/entities",
      "GET /iam/entities/entity%2Fa",
      "POST /iam/entities",
      "PATCH /iam/entities/entity%2Fa",
      "GET /iam/entities/entity%2Fa/role-bindings",
      "PUT /iam/entities/entity%2Fa/role-bindings/role%2Fa/principal%2Fa",
      "DELETE /iam/entities/entity%2Fa/role-bindings/role%2Fa/principal%2Fa",
      "GET /iam/relationships?resource_id=entity%2Fa",
      "POST /iam/relationships",
      "DELETE /iam/relationships?subject_type=principal&subject_id=principal%2Fa&relation=viewer&resource_type=store&resource_id=entity%2Fa",
      "GET /iam/relationships?entity_id=entity%2Fa",
      "DELETE /iam/relationships?entity_id=entity%2Fa&subject_type=principal&subject_id=principal%2Fa&relation=viewer&resource_type=store&resource_id=business%2Fstore%2Fa",
      "POST /iam/authorization/check",
      "POST /iam/authorization/check",
      "POST /iam/authorization/constraints",
      "POST /iam/authorization/explain",
      "POST /iam/authorization/explain",
      "DELETE /iam/entities/entity%2Fa",
    ])
    expect(
      requests.every(
        ({ headers }) => headers.get("x-tenant-id") === "tenant-entities"
      )
    ).toBe(true)
    expect(JSON.parse(requests[2]?.body ?? "{}")).toEqual({
      parent_id: entity.parent_id,
      type: entity.type,
      name: entity.name,
      status: entity.status,
      metadata: entity.metadata,
    })
    expect(JSON.parse(requests[5]?.body ?? "{}")).toEqual({
      effect: binding.effect,
      expires_at: binding.expires_at,
    })
    expect(JSON.parse(requests[8]?.body ?? "{}")).toEqual(relationshipInput)
    expect(JSON.parse(requests[12]?.body ?? "{}")).toEqual({
      entity_id: entity.id,
      permission_code: "store_view",
      subject: binding.principal_id,
    })
    expect(JSON.parse(requests[13]?.body ?? "{}")).toEqual({
      entity_id: entity.id,
      resource_type: "store",
      resource_id: "business/store/a",
      permission_code: "store_view",
      subject: binding.principal_id,
    })
    expect(JSON.parse(requests[14]?.body ?? "{}")).toEqual({
      permission_code: "store_view",
      subject: binding.principal_id,
    })
    expect(JSON.parse(requests[15]?.body ?? "{}")).toEqual({
      entity_id: entity.id,
      permission_code: "store_view",
      subject: binding.principal_id,
    })
    expect(JSON.parse(requests[16]?.body ?? "{}")).toEqual({
      entity_id: entity.id,
      resource_type: "store",
      resource_id: "business/store/a",
      permission_code: "store_view",
      subject: binding.principal_id,
    })
  })

  it("reads tenant audit pagination, detail and chain integrity", async () => {
    const requests: RecordedRequest[] = []
    const event: AuditEvent = {
      id: "event-1",
      tenant_id: "tenant-audit",
      event_type: "oauth_client_created",
      target_type: "oauth_client",
      target_id: "client-1",
      outcome: "success",
      detail: { public_client: false },
      sequence: 1,
      event_hash: "a".repeat(64),
      created_at: "2026-08-01T15:00:00Z",
    }
    server = Bun.serve({
      port: 0,
      async fetch(request) {
        const url = new URL(request.url)
        requests.push({
          method: request.method,
          path: `${url.pathname}${url.search}`,
          headers: request.headers,
          body: await request.text(),
        })
        if (url.pathname === "/iam/audit-integrity")
          return Response.json({
            code: 0,
            message: "ok",
            data: {
              tenant_id: "tenant-audit",
              valid: true,
              verified_events: 1,
              head_sequence: 1,
              head_hash: "a".repeat(64),
            },
          })
        if (url.pathname === "/iam/audit-events/export")
          return new Response(
            [
              JSON.stringify({
                type: "manifest",
                schema: "chaosplus.audit-export.v1",
                tenant_id: "tenant-audit",
                head_sequence: 2,
              }),
              JSON.stringify({ type: "event", event }),
              JSON.stringify({
                type: "complete",
                exported_events: 1,
                content_sha256: "b".repeat(64),
              }),
              "",
            ].join("\n"),
            {
              headers: {
                "Content-Type": "application/x-ndjson",
                "Content-Disposition":
                  'attachment; filename="chaosplus-audit-20260801T150000Z.ndjson"',
              },
            }
          )
        if (url.pathname === "/iam/audit-events/event-1")
          return Response.json({ code: 0, message: "ok", data: event })
        return Response.json({
          code: 0,
          message: "ok",
          data: [event],
          meta: { page: { offset: 0, limit: 50, count: 1, total: 7 } },
        })
      },
    })
    const client = createIamApi(
      `http://127.0.0.1:${server.port}`,
      () => "tenant-audit"
    )

    expect(await client.auditEvents("event_type=oauth_client_created")).toEqual(
      {
        items: [event],
        total: 7,
      }
    )
    expect(await client.auditEvent("event-1")).toEqual(event)
    expect(await client.auditIntegrity()).toMatchObject({
      valid: true,
      head_sequence: 1,
    })
    const download = await client.auditExport("event_type=oauth_client_created")
    expect(download.filename).toBe("chaosplus-audit-20260801T150000Z.ndjson")
    expect(await download.blob.text()).toContain(
      '"schema":"chaosplus.audit-export.v1"'
    )
    expect(requests.map(({ path }) => path)).toEqual([
      "/iam/audit-events?event_type=oauth_client_created",
      "/iam/audit-events/event-1",
      "/iam/audit-integrity",
      "/iam/audit-events/export?event_type=oauth_client_created",
    ])
    for (const request of requests)
      expect(request.headers.get("x-tenant-id")).toBe("tenant-audit")
  })

  it("rejects invalid or incomplete audit export responses", async () => {
    let requestCount = 0
    server = Bun.serve({
      port: 0,
      fetch() {
        requestCount++
        if (requestCount === 1)
          return new Response("not ndjson", {
            headers: { "Content-Type": "text/plain" },
          })
        return new Response(
          '{"type":"manifest","schema":"chaosplus.audit-export.v1"}\n',
          { headers: { "Content-Type": "application/x-ndjson" } }
        )
      },
    })
    const client = createIamApi(`http://127.0.0.1:${server.port}`)
    await expect(client.auditExport()).rejects.toMatchObject({
      status: 502,
      message: "The audit export returned an unexpected file type.",
    })
    await expect(client.auditExport()).rejects.toMatchObject({
      status: 502,
      message: "The audit export is incomplete.",
    })
  })
})
